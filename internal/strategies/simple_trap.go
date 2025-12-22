package strategies

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"polymarketbot/internal/core"
	"polymarketbot/internal/execution"
	"polymarketbot/internal/services"
)

type SimpleTrap struct {
	api      *services.PolymarketService
	ws       *services.WebSocketService
	executor execution.Executor

	mu        sync.Mutex
	isRunning bool
	logger    *services.SpreadsheetLogger

	// --- CONFIG (mirrors TS) ---
	initialTrapPrice   float64
	initialTrapSize    float64
	hedgePrice         float64
	hedgeTimeout       time.Duration
	marketSellPriceGap float64
	oneCycleOnly       bool

	// --- STATE ---
	phase             string // NEUTRAL|HEDGE_PENDING|EXIT
	currentMarketSlug string
	marketEndTimeMs   int64

	activeOrders map[string]struct {
		side  string // YES|NO
		typ   string // TRAP|HEDGE
		price float64
	}
	orderFillHistory map[string]float64

	filledCountYes float64
	filledCountNo  float64
	avgCostYes     float64
	avgCostNo      float64
	yesFills       []services.Fill
	noFills        []services.Fill

	tokenIdYes string
	tokenIdNo  string
	priceYes   float64
	priceNo    float64

	lastLogTime    time.Time
	processingTick bool
	lastSyncTime   time.Time

	lastTickTime   time.Time
	zeroDataStreak int

	processedMatchIds map[string]struct{}

	hedgeTimeoutTimer *time.Timer
	trapFillTime      time.Time
	hedgeOrderID      string
	filledSide        string // YES|NO

	healthTicker *time.Ticker
	syncTicker   *time.Ticker
	marketTimer  *time.Timer

	doneCh chan struct{}
}

func NewSimpleTrap(api *services.PolymarketService, ws *services.WebSocketService, ex execution.Executor) *SimpleTrap {
	return &SimpleTrap{
		api:      api,
		ws:       ws,
		executor: ex,
		logger:   services.NewSpreadsheetLogger("SimpleTrap"),

		initialTrapPrice:   0.46,
		initialTrapSize:    5,
		hedgePrice:         0.52,
		hedgeTimeout:       3 * time.Minute,
		marketSellPriceGap: 0.05,
		oneCycleOnly:       true,

		phase: "NEUTRAL",
		activeOrders: map[string]struct {
			side, typ string
			price     float64
		}{},
		orderFillHistory:  map[string]float64{},
		processedMatchIds: map[string]struct{}{},
		lastTickTime:      time.Now(),
		doneCh:            make(chan struct{}),
	}
}

func (b *SimpleTrap) Done() <-chan struct{} { return b.doneCh }

// HandleUserWS exposes private WS handler to app wiring.
func (b *SimpleTrap) HandleUserWS(ctx context.Context, v any) { b.handleUserFill(ctx, v) }

func (b *SimpleTrap) Start(ctx context.Context) error {
	b.mu.Lock()
	b.isRunning = true
	b.mu.Unlock()

	fmt.Println("🚀 SIMPLE TRAP BOT STARTED")
	fmt.Println("   🎯 Strategy: Trap @ $0.46 → Hedge @ $0.52 → Exit")

	_ = b.hydrateState(ctx) // mirrors TS: first call likely no-op until slug set

	// user stream is optional; strategy still works via polling fills
	// (in upstream it logs critical if missing, but continues)
	b.healthTicker = time.NewTicker(15 * time.Second)
	b.syncTicker = time.NewTicker(30 * time.Second)

	go func() {
		for {
			select {
			case <-b.healthTicker.C:
				_ = b.checkConnectionHealth(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		for {
			select {
			case <-b.syncTicker.C:
				_ = b.syncPositions(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()

	go b.safeRunLifecycle(ctx)
	return nil
}

func (b *SimpleTrap) Stop(ctx context.Context) error {
	_ = ctx
	fmt.Println("🛑 Stopping bot gracefully...")
	b.mu.Lock()
	b.isRunning = false
	if b.healthTicker != nil {
		b.healthTicker.Stop()
		b.healthTicker = nil
	}
	if b.syncTicker != nil {
		b.syncTicker.Stop()
		b.syncTicker = nil
	}
	if b.marketTimer != nil {
		b.marketTimer.Stop()
		b.marketTimer = nil
	}
	if b.hedgeTimeoutTimer != nil {
		b.hedgeTimeoutTimer.Stop()
		b.hedgeTimeoutTimer = nil
	}
	b.mu.Unlock()

	_ = b.cancelAllOrders(ctx)
	b.ws.Close()
	select {
	case <-b.doneCh:
	default:
		close(b.doneCh)
	}
	return nil
}

func (b *SimpleTrap) safeRunLifecycle(ctx context.Context) {
	if err := b.runLifecycle(ctx); err != nil {
		services.DashboardManager.Log("❌ Lifecycle error: "+err.Error(), services.LogError)
		select {
		case <-time.After(5 * time.Second):
			b.mu.Lock()
			running := b.isRunning
			b.mu.Unlock()
			if running {
				b.safeRunLifecycle(ctx)
			}
		case <-ctx.Done():
		}
	}
}

func (b *SimpleTrap) runLifecycle(ctx context.Context) error {
	b.mu.Lock()
	running := b.isRunning
	b.mu.Unlock()
	if !running {
		return nil
	}

	current := core.GetTargetMarketSlug()
	next := core.GetNextMarketSlug()

	services.DashboardManager.Log(fmt.Sprintf("🔍 System Start. Checking Market: %s...", current.Slug[:min(10, len(current.Slug))]), services.LogInfo)
	target := next

	// SMART TARGETING
	if tokens, err := b.api.GetMarketTokenIDs(current.Slug); err == nil && len(tokens) >= 2 {
		yes, no, _ := b.executor.GetPositions(ctx, tokens[0], tokens[1])
		if yes > 0.1 || no > 0.1 {
			services.DashboardManager.Log("🚨 ACTIVE TRADE FOUND! Trading Current Market.", services.LogWarn)
			target = current
		}
	}

	b.mu.Lock()
	b.marketEndTimeMs = target.EndTimeMs
	b.currentMarketSlug = target.Slug
	b.mu.Unlock()

	// WAIT LOGIC: target NEXT -> place 1 min before start
	msUntilStart := core.GetMsUntil(target.StartTimeMs)
	msUntilPrep := msUntilStart - 60_000
	if msUntilPrep > 0 && target.Slug == next.Slug {
		waitSecs := int(math.Round(float64(msUntilPrep) / 1000))
		waitMins := int(math.Round(float64(waitSecs) / 60))
		services.SetBotStatus("SimpleTrap", fmt.Sprintf("⏳ Waiting %dm %ds for next market", waitMins, waitSecs%60))
		services.DashboardManager.Log(fmt.Sprintf("⏳ Current market still active. Waiting %dm %ds until 1 min before next market...", waitMins, waitSecs%60), services.LogInfo)
		select {
		case <-time.After(time.Duration(msUntilPrep) * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// GET TOKENS
	tokenIDs, err := b.api.GetMarketTokenIDs(target.Slug)
	if err != nil || len(tokenIDs) < 2 {
		services.DashboardManager.Log(fmt.Sprintf("❌ Failed to get tokens for %s. Retrying in 10s...", target.Slug), services.LogError)
		select {
		case <-time.After(10 * time.Second):
			return b.runLifecycle(ctx)
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	b.mu.Lock()
	b.tokenIdYes = tokenIDs[0]
	b.tokenIdNo = tokenIDs[1]
	b.mu.Unlock()
	services.DashboardManager.LogEvent("SIMPLETRAP", "✅ Tokens Acquired")

	// HYDRATE
	b.mu.Lock()
	needHydrate := b.filledCountYes == 0 && b.filledCountNo == 0
	b.mu.Unlock()
	if needHydrate {
		_ = b.hydrateState(ctx)
	}

	// PLACE TRAPS
	b.mu.Lock()
	hasTraps := false
	for _, o := range b.activeOrders {
		if o.typ == "TRAP" {
			hasTraps = true
			break
		}
	}
	phase := b.phase
	b.mu.Unlock()
	if !hasTraps && phase == "NEUTRAL" {
		_ = b.placeTrapOrders(ctx)
	} else {
		services.DashboardManager.Log("✅ State Restored. Skipping Initial Traps.", services.LogInfo)
	}

	// CONNECT WS (market)
	b.mu.Lock()
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	b.mu.Unlock()
	if yesID == "" || noID == "" {
		services.DashboardManager.Log("❌ Cannot subscribe: Tokens not set. Retrying lifecycle...", services.LogError)
		select {
		case <-time.After(5 * time.Second):
			return b.runLifecycle(ctx)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	b.ws.SubscribeMarket([]string{yesID, noID}, func(data any) { _ = b.handleMarketTick(ctx, data) })

	// SCHEDULE END
	b.mu.Lock()
	msUntilEnd := core.GetMsUntil(b.marketEndTimeMs)
	if b.marketTimer != nil {
		b.marketTimer.Stop()
	}
	b.marketTimer = time.AfterFunc(time.Duration(msUntilEnd+2000)*time.Millisecond, func() {
		_ = b.endCycleAndRestart(context.Background())
	})
	b.mu.Unlock()

	services.DashboardManager.Log(fmt.Sprintf("⏰ Timer set for %.1f minutes", float64(msUntilEnd)/60000), services.LogInfo)
	return nil
}

func (b *SimpleTrap) endCycleAndRestart(ctx context.Context) error {
	services.SetBotStatus("SimpleTrap", "🔄 Switching cycle...")
	services.DashboardManager.LogEvent("SIMPLETRAP", "🏁 Market ended")
	_ = b.logIncompletePosition(ctx)
	b.ws.Close()

	b.resetState()

	if b.oneCycleOnly {
		fmt.Println("[SIMPLETRAP] ✅ Single Cycle Complete.")
		b.mu.Lock()
		b.isRunning = false
		b.mu.Unlock()
		select {
		case <-b.doneCh:
		default:
			close(b.doneCh)
		}
		return nil
	}
	go b.safeRunLifecycle(ctx)
	return nil
}

func (b *SimpleTrap) resetState() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.phase = "NEUTRAL"
	b.filledCountYes = 0
	b.filledCountNo = 0
	b.avgCostYes = 0
	b.avgCostNo = 0
	b.yesFills = nil
	b.noFills = nil
	b.activeOrders = map[string]struct {
		side, typ string
		price     float64
	}{}
	b.orderFillHistory = map[string]float64{}
	b.processedMatchIds = map[string]struct{}{}
	b.hedgeOrderID = ""
	b.filledSide = ""
	b.trapFillTime = time.Time{}
	if b.hedgeTimeoutTimer != nil {
		b.hedgeTimeoutTimer.Stop()
		b.hedgeTimeoutTimer = nil
	}
}

func (b *SimpleTrap) hydrateState(ctx context.Context) error {
	b.mu.Lock()
	if b.currentMarketSlug == "" {
		b.mu.Unlock()
		return nil // mirrors TS
	}
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	b.mu.Unlock()

	yes, no, err := b.executor.GetPositions(ctx, yesID, noID)
	if err == nil && (yes > 0 || no > 0) {
		b.mu.Lock()
		b.filledCountYes = yes
		b.filledCountNo = no
		b.mu.Unlock()
	}

	b.mu.Lock()
	if b.filledCountYes > 0 && b.avgCostYes == 0 {
		b.avgCostYes = b.filledCountYes * 0.5
	}
	if b.filledCountNo > 0 && b.avgCostNo == 0 {
		b.avgCostNo = b.filledCountNo * 0.5
	}
	if b.filledCountYes > 0 || b.filledCountNo > 0 {
		services.DashboardManager.Log(fmt.Sprintf("📝 MEMORY UPDATED: %.1fY / %.1fN (from Scan)", b.filledCountYes, b.filledCountNo), services.LogWarn)
		if b.phase == "NEUTRAL" {
			b.phase = "HEDGE_PENDING"
		}
	}
	b.mu.Unlock()
	return nil
}

func (b *SimpleTrap) handleMarketTick(ctx context.Context, data any) error {
	b.mu.Lock()
	if b.processingTick {
		b.mu.Unlock()
		return nil
	}
	b.processingTick = true
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.processingTick = false
		b.mu.Unlock()
	}()

	b.mu.Lock()
	b.lastTickTime = time.Now()
	b.mu.Unlock()

	b.updatePrices(data)

	b.mu.Lock()
	isPriceZero := b.priceYes == 0 && b.priceNo == 0
	isAmnesia := b.priceYes == 0 && b.priceNo == 0 && b.filledCountYes == 0 && b.filledCountNo == 0
	if isPriceZero || isAmnesia {
		b.zeroDataStreak++
		if b.zeroDataStreak > 15 {
			b.zeroDataStreak = 0
			b.mu.Unlock()
			services.DashboardManager.Log("⚠️ ANOMALY: Data Streak Dead. Resetting Socket...", services.LogWarn)
			_ = b.reconnectWebSocket(ctx)
			return nil
		}
	} else {
		b.zeroDataStreak = 0
	}
	b.mu.Unlock()

	_ = b.checkFills(ctx)

	// dashboard update
	b.mu.Lock()
	should := b.lastLogTime.IsZero() || time.Since(b.lastLogTime) > time.Second
	if should {
		b.lastLogTime = time.Now()
	}
	b.mu.Unlock()
	if should {
		b.updateDashboard()
	}

	return nil
}

func (b *SimpleTrap) updatePrices(data any) {
	m, ok := data.(map[string]any)
	if !ok {
		return
	}
	asks, _ := m["asks"].([]any)
	assetID, _ := m["asset_id"].(string)
	if len(asks) == 0 || assetID == "" {
		return
	}
	first, _ := asks[0].(map[string]any)
	if first == nil {
		return
	}
	priceStr := fmt.Sprint(first["price"])
	p := parseFloat(priceStr)
	if p <= 0 {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if assetID == b.tokenIdYes {
		b.priceYes = p
	}
	if assetID == b.tokenIdNo {
		b.priceNo = p
	}
}

func (b *SimpleTrap) checkFills(ctx context.Context) error {
	b.mu.Lock()
	orders := make([]struct {
		id   string
		info struct {
			side, typ string
			price     float64
		}
	}, 0, len(b.activeOrders))
	for id, info := range b.activeOrders {
		orders = append(orders, struct {
			id   string
			info struct {
				side, typ string
				price     float64
			}
		}{id: id, info: info})
	}
	b.mu.Unlock()

	for _, o := range orders {
		status, _ := b.executor.GetOrderStatus(ctx, o.id)
		totalMatched := status.Matched

		b.mu.Lock()
		prev := b.orderFillHistory[o.id]
		if totalMatched > prev {
			newFill := totalMatched - prev
			fillPrice := status.AvgFillPrice
			if fillPrice == 0 {
				fillPrice = o.info.price
			}
			b.orderFillHistory[o.id] = totalMatched
			b.mu.Unlock()

			_ = b.handleOrderFilled(ctx, o.id, o.info.side, o.info.typ, fillPrice, newFill)
			continue
		}
		b.mu.Unlock()
	}
	return nil
}

func (b *SimpleTrap) handleUserFill(ctx context.Context, data any) {
	m, ok := data.(map[string]any)
	if !ok {
		return
	}
	if fmt.Sprint(m["event_type"]) != "trade" {
		return
	}
	matchID := fmt.Sprint(firstAny(m, "match_id", "id"))
	if matchID == "" {
		return
	}

	b.mu.Lock()
	if _, ok := b.processedMatchIds[matchID]; ok {
		b.mu.Unlock()
		return
	}
	b.processedMatchIds[matchID] = struct{}{}
	b.mu.Unlock()

	assetID := fmt.Sprint(firstAny(m, "asset_id", "token_id"))
	price := parseFloat(fmt.Sprint(m["price"]))
	size := parseFloat(fmt.Sprint(firstAny(m, "size", "amount")))
	if assetID == "" || price <= 0 || size <= 0 {
		return
	}

	if assetID == b.tokenIdYes || assetID == b.tokenIdNo {
		side := "YES"
		if assetID == b.tokenIdNo {
			side = "NO"
		}
		_ = b.handleOrderFilled(ctx, matchID, side, "TRAP", price, size) // mirrors TS simplification
	}
}

func (b *SimpleTrap) handleOrderFilled(ctx context.Context, orderID string, side string, typ string, fillPrice float64, filledSize float64) error {
	services.DashboardManager.LogEvent("SIMPLETRAP", fmt.Sprintf("💥 %s %s @ $%.3f (x%.2f)", side, typ, fillPrice, filledSize))

	b.mu.Lock()
	if side == "YES" {
		b.filledCountYes += filledSize
		b.avgCostYes += fillPrice * filledSize
		b.yesFills = append(b.yesFills, services.Fill{Price: fillPrice, Size: filledSize})
	} else {
		b.filledCountNo += filledSize
		b.avgCostNo += fillPrice * filledSize
		b.noFills = append(b.noFills, services.Fill{Price: fillPrice, Size: filledSize})
	}
	phase := b.phase
	b.mu.Unlock()

	// Trap filled
	if phase == "NEUTRAL" && typ == "TRAP" {
		loserSide := "NO"
		if side == "NO" {
			loserSide = "YES"
		}
		services.DashboardManager.Log(fmt.Sprintf("🚨 TRAP FILLED! Cancelling opposite %s trap immediately...", loserSide), services.LogTrade)
		_ = b.cancelOrdersByType(ctx, loserSide, "TRAP")

		b.mu.Lock()
		b.filledSide = side
		b.trapFillTime = time.Now()
		b.phase = "HEDGE_PENDING"
		if b.hedgeTimeoutTimer != nil {
			b.hedgeTimeoutTimer.Stop()
		}
		b.hedgeTimeoutTimer = time.AfterFunc(b.hedgeTimeout, func() {
			_ = b.handleHedgeTimeout(context.Background())
		})
		b.mu.Unlock()

		services.DashboardManager.Log(fmt.Sprintf("📤 Placing hedge order immediately: %s @ $%.2f", loserSide, b.hedgePrice), services.LogTrade)
		_ = b.placeHedgeOrder(ctx, side)
		return nil
	}

	if phase == "HEDGE_PENDING" && typ == "HEDGE" {
		return b.transitionToExit(ctx)
	}
	return nil
}

func (b *SimpleTrap) placeHedgeOrder(ctx context.Context, filledSide string) error {
	defender := "NO"
	tokenID := b.tokenIdNo
	if filledSide == "NO" {
		defender = "YES"
		tokenID = b.tokenIdYes
	}
	id, err := b.executor.PlaceOrder(ctx, execution.TradeParams{
		TokenID: tokenID,
		Side:    execution.SideBuy,
		Price:   b.hedgePrice,
		Size:    b.initialTrapSize,
		Type:    execution.OrderTypeGTC,
	})
	if err != nil {
		services.DashboardManager.Log("❌ HEDGE FAILED: "+err.Error(), services.LogError)
		return err
	}

	b.mu.Lock()
	b.hedgeOrderID = id
	b.activeOrders[id] = struct {
		side, typ string
		price     float64
	}{side: defender, typ: "HEDGE", price: b.hedgePrice}
	b.mu.Unlock()
	services.SetBotStatus("SimpleTrap", fmt.Sprintf("🛡️ Hedge Pending (%s @ $%.2f)", defender, b.hedgePrice))
	return nil
}

func (b *SimpleTrap) handleHedgeTimeout(ctx context.Context) error {
	b.mu.Lock()
	if b.phase != "HEDGE_PENDING" {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	services.DashboardManager.Log("⏰ HEDGE TIMEOUT: 3 minutes elapsed. Exiting position...", services.LogWarn)
	_ = b.cancelAllOrders(ctx)

	b.mu.Lock()
	side := b.filledSide
	entryYes := 0.0
	entryNo := 0.0
	if b.filledCountYes > 0 {
		entryYes = b.avgCostYes / b.filledCountYes
	}
	if b.filledCountNo > 0 {
		entryNo = b.avgCostNo / b.filledCountNo
	}
	priceYes := b.priceYes
	priceNo := b.priceNo
	b.mu.Unlock()

	if side == "YES" {
		if priceYes > 0 && priceYes >= entryYes+b.marketSellPriceGap {
			services.DashboardManager.Log(fmt.Sprintf("💰 Market sell: Current price $%.2f is $%.2f above entry. Selling at market...", priceYes, priceYes-entryYes), services.LogInfo)
			_ = b.sellAtMarket(ctx, "YES")
		} else {
			be := 1.0 - entryYes
			services.DashboardManager.Log(fmt.Sprintf("📊 Placing breakeven order: %.2f YES @ $%.2f", b.filledCountYes, be), services.LogInfo)
			_ = b.placeBreakevenOrder(ctx, "YES", be)
		}
	} else if side == "NO" {
		if priceNo > 0 && priceNo >= entryNo+b.marketSellPriceGap {
			services.DashboardManager.Log(fmt.Sprintf("💰 Market sell: Current price $%.2f is $%.2f above entry. Selling at market...", priceNo, priceNo-entryNo), services.LogInfo)
			_ = b.sellAtMarket(ctx, "NO")
		} else {
			be := 1.0 - entryNo
			services.DashboardManager.Log(fmt.Sprintf("📊 Placing breakeven order: %.2f NO @ $%.2f", b.filledCountNo, be), services.LogInfo)
			_ = b.placeBreakevenOrder(ctx, "NO", be)
		}
	}

	b.mu.Lock()
	b.phase = "EXIT"
	b.mu.Unlock()
	return nil
}

func (b *SimpleTrap) sellAtMarket(ctx context.Context, side string) error {
	tokenID := b.tokenIdYes
	shares := b.filledCountYes
	if side == "NO" {
		tokenID = b.tokenIdNo
		shares = b.filledCountNo
	}
	_, err := b.executor.PlaceOrder(ctx, execution.TradeParams{
		TokenID: tokenID,
		Side:    execution.SideSell,
		Price:   0.01,
		Size:    shares,
		Type:    execution.OrderTypeFOK,
	})
	if err != nil {
		services.DashboardManager.Log("❌ Market sell failed: "+err.Error(), services.LogError)
	}
	return err
}

func (b *SimpleTrap) placeBreakevenOrder(ctx context.Context, side string, price float64) error {
	tokenID := b.tokenIdYes
	shares := b.filledCountYes
	if side == "NO" {
		tokenID = b.tokenIdNo
		shares = b.filledCountNo
	}
	id, err := b.executor.PlaceOrder(ctx, execution.TradeParams{
		TokenID: tokenID,
		Side:    execution.SideSell,
		Price:   price,
		Size:    shares,
		Type:    execution.OrderTypeGTC,
	})
	if err != nil {
		services.DashboardManager.Log("❌ Breakeven order failed: "+err.Error(), services.LogError)
		return err
	}
	b.mu.Lock()
	b.activeOrders[id] = struct {
		side, typ string
		price     float64
	}{side: side, typ: "HEDGE", price: price}
	b.mu.Unlock()
	services.DashboardManager.Log(fmt.Sprintf("✅ Breakeven order placed: %s %.2f @ $%.2f", side, shares, price), services.LogInfo)
	return nil
}

func (b *SimpleTrap) transitionToExit(ctx context.Context) error {
	b.mu.Lock()
	if b.phase == "EXIT" {
		b.mu.Unlock()
		return nil
	}
	b.phase = "EXIT"
	yes := b.filledCountYes
	no := b.filledCountNo
	totalCost := b.avgCostYes + b.avgCostNo
	yesFills := append([]services.Fill(nil), b.yesFills...)
	noFills := append([]services.Fill(nil), b.noFills...)
	slug := b.currentMarketSlug
	b.mu.Unlock()

	services.DashboardManager.Log("⚖️ PERFECT BALANCE REACHED. EXITING...", services.LogTrade)
	_ = b.cancelAllOrders(ctx)

	hedgedPairs := math.Min(yes, no)
	payout := hedgedPairs * 1.0
	profit := payout - totalCost
	profitPercent := 0.0
	if totalCost > 0 {
		profitPercent = profit / totalCost
	}
	services.DashboardManager.LogEvent("SIMPLETRAP", fmt.Sprintf("🏆 WIN! +$%.3f (%.1fY/%.1fN)", profit, yes, no))

	rec := services.TradeRecord{
		Time:            time.Now().UTC().Format(time.RFC3339),
		MarketSlug:      slug,
		CapitalInvested: totalCost,
		ProfitPercent:   profitPercent,
		CapitalEnd:      payout,
		YesPositions:    services.FormatPositions(yesFills),
		YesAvg:          services.CalculateAvg(yesFills),
		YesTotal:        yes,
		NoPositions:     services.FormatPositions(noFills),
		NoAvg:           services.CalculateAvg(noFills),
		NoTotal:         no,
		PnL:             profit,
		Outcome:         services.OutcomeWin,
	}
	if profit < 0 {
		rec.Outcome = services.OutcomeLoss
	} else if profit == 0 {
		rec.Outcome = services.OutcomeBreakeven
	}
	b.logger.LogTrade(rec)
	return nil
}

func (b *SimpleTrap) logIncompletePosition(ctx context.Context) error {
	b.mu.Lock()
	hasPos := b.filledCountYes > 0 || b.filledCountNo > 0
	isBalanced := b.filledCountYes == b.filledCountNo && b.filledCountYes > 0
	phase := b.phase
	totalCost := b.avgCostYes + b.avgCostNo
	yes := b.filledCountYes
	no := b.filledCountNo
	slug := b.currentMarketSlug
	yesFills := append([]services.Fill(nil), b.yesFills...)
	noFills := append([]services.Fill(nil), b.noFills...)
	b.mu.Unlock()

	if hasPos && !isBalanced && phase != "EXIT" {
		hedgedPairs := math.Min(yes, no)
		hedgedPayout := hedgedPairs * 1.0
		profit := hedgedPayout - totalCost
		profitPercent := 0.0
		if totalCost > 0 {
			profitPercent = profit / totalCost
		}
		services.DashboardManager.LogEvent("SIMPLETRAP", fmt.Sprintf("💔 LOSS: Unbalanced position (%.1fY/%.1fN) -$%.3f", yes, no, math.Abs(profit)))
		rec := services.TradeRecord{
			Time:            time.Now().UTC().Format(time.RFC3339),
			MarketSlug:      slug,
			CapitalInvested: totalCost,
			ProfitPercent:   profitPercent,
			CapitalEnd:      hedgedPayout,
			YesPositions:    services.FormatPositions(yesFills),
			YesAvg:          services.CalculateAvg(yesFills),
			YesTotal:        yes,
			NoPositions:     services.FormatPositions(noFills),
			NoAvg:           services.CalculateAvg(noFills),
			NoTotal:         no,
			PnL:             profit,
			Outcome:         services.OutcomeLoss,
		}
		b.logger.LogTrade(rec)
	}
	return nil
}

func (b *SimpleTrap) placeTrapOrders(ctx context.Context) error {
	b.mu.Lock()
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	b.mu.Unlock()
	if yesID == "" || noID == "" {
		services.DashboardManager.Log("❌ Cannot place traps: Tokens not set yet.", services.LogError)
		return nil
	}

	services.DashboardManager.Log(fmt.Sprintf("🪤 Placing TRAP orders @ $%.2f...", b.initialTrapPrice), services.LogInfo)
	idYes, err := b.executor.PlaceOrder(ctx, execution.TradeParams{
		TokenID: yesID,
		Side:    execution.SideBuy,
		Price:   b.initialTrapPrice,
		Size:    b.initialTrapSize,
		Type:    execution.OrderTypeGTC,
	})
	if err != nil {
		services.DashboardManager.Log("❌ TRAP FAILED: "+err.Error(), services.LogError)
		return err
	}
	idNo, err := b.executor.PlaceOrder(ctx, execution.TradeParams{
		TokenID: noID,
		Side:    execution.SideBuy,
		Price:   b.initialTrapPrice,
		Size:    b.initialTrapSize,
		Type:    execution.OrderTypeGTC,
	})
	if err != nil {
		services.DashboardManager.Log("❌ TRAP FAILED: "+err.Error(), services.LogError)
		return err
	}

	b.mu.Lock()
	b.activeOrders[idYes] = struct {
		side, typ string
		price     float64
	}{side: "YES", typ: "TRAP", price: b.initialTrapPrice}
	b.activeOrders[idNo] = struct {
		side, typ string
		price     float64
	}{side: "NO", typ: "TRAP", price: b.initialTrapPrice}
	b.mu.Unlock()

	services.SetBotStatus("SimpleTrap", fmt.Sprintf("🪤 Traps Set ($%.2f)", b.initialTrapPrice))
	return nil
}

func (b *SimpleTrap) cancelAllOrders(ctx context.Context) error {
	b.mu.Lock()
	ids := make([]string, 0, len(b.activeOrders))
	for id := range b.activeOrders {
		ids = append(ids, id)
	}
	b.mu.Unlock()

	for _, id := range ids {
		_ = b.executor.CancelOrder(ctx, id)
		b.mu.Lock()
		delete(b.activeOrders, id)
		b.mu.Unlock()
	}
	return nil
}

func (b *SimpleTrap) cancelOrdersByType(ctx context.Context, side string, typ string) error {
	b.mu.Lock()
	var ids []string
	for id, info := range b.activeOrders {
		if info.side == side && info.typ == typ {
			ids = append(ids, id)
		}
	}
	b.mu.Unlock()
	for _, id := range ids {
		_ = b.executor.CancelOrder(ctx, id)
		b.mu.Lock()
		delete(b.activeOrders, id)
		b.mu.Unlock()
	}
	return nil
}

func (b *SimpleTrap) reconnectWebSocket(ctx context.Context) error {
	_ = ctx
	b.ws.Close()
	time.Sleep(1 * time.Second)
	b.mu.Lock()
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	b.mu.Unlock()
	b.ws.SubscribeMarket([]string{yesID, noID}, func(data any) { _ = b.handleMarketTick(context.Background(), data) })
	return nil
}

func (b *SimpleTrap) checkConnectionHealth(ctx context.Context) error {
	b.mu.Lock()
	if !b.isRunning {
		b.mu.Unlock()
		return nil
	}
	silence := time.Since(b.lastTickTime)
	b.mu.Unlock()
	if silence > 45*time.Second {
		services.DashboardManager.Log(fmt.Sprintf("💀 WATCHDOG: Connection Dead (%ds). Resetting Socket...", int(silence.Seconds())), services.LogWarn)
		return b.reconnectWebSocket(ctx)
	}
	return nil
}

func (b *SimpleTrap) syncPositions(ctx context.Context) error {
	b.mu.Lock()
	if !b.lastSyncTime.IsZero() && time.Since(b.lastSyncTime) < 5*time.Second {
		b.mu.Unlock()
		return nil
	}
	b.lastSyncTime = time.Now()
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	memYes, memNo := b.filledCountYes, b.filledCountNo
	b.mu.Unlock()

	yes, no, err := b.executor.GetPositions(ctx, yesID, noID)
	if err != nil {
		return nil
	}
	b.mu.Lock()
	if memYes == 0 && memNo == 0 && (yes > 0 || no > 0) {
		b.filledCountYes = yes
		b.filledCountNo = no
	}
	b.mu.Unlock()
	return nil
}

func (b *SimpleTrap) updateDashboard() {
	b.mu.Lock()
	status := fmt.Sprintf("[%.1fY / %.1fN]", b.filledCountYes, b.filledCountNo)
	b.mu.Unlock()
	services.SetBotStatus("SimpleTrap", status)
}
