package strategies

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"polymarketbot/internal/core"
	"polymarketbot/internal/execution"
	"polymarketbot/internal/services"
)

type SniperLadder struct {
	api      *services.PolymarketService
	ws       *services.WebSocketService
	executor execution.Executor

	mu        sync.Mutex
	isRunning bool
	logger    *services.SpreadsheetLogger

	// --- CONFIG (mirrors TS) ---
	initialTrapPrice float64
	initialTrapSize  float64
	legThreshold     float64
	ladderLevels     []struct {
		price float64
		size  float64
	}
	initialProfitTargetPercent float64
	pivotTimeBeforeEnd         time.Duration
	maxSpread                 float64
	oneCycleOnly              bool

	partialSellTime time.Duration
	fullSellTime    time.Duration
	priceHistLen    int

	// --- STATE ---
	phase string // NEUTRAL|TRIGGERED|ACCUMULATION|PARTIAL_HEDGE|PIVOT|EXIT

	firstFillTime     time.Time
	currentMarketSlug string
	marketEndTimeMs   int64

	processedMatchIds map[string]struct{}

	activeOrders map[string]struct {
		side  string // YES|NO
		typ   string // TRAP|LADDER|HEDGE
		price float64
	}

	orderFillHistory map[string]float64 // orderId -> totalMatched already processed

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

	lastLogTime      time.Time
	processingTick   bool
	lastSyncTime     time.Time
	lastTickTime     time.Time
	zeroDataStreak   int
	isHedging        bool
	lastTradeTime    time.Time

	priceHistoryYes []float64
	priceHistoryNo  []float64
	partialSellDone bool
	fullSellDone    bool

	healthTicker *time.Ticker
	syncTicker   *time.Ticker
	marketTimer  *time.Timer

	tokenClaimer *services.TokenClaimer

	doneCh chan struct{}
}

func NewSniperLadder(api *services.PolymarketService, ws *services.WebSocketService, ex execution.Executor, claimer *services.TokenClaimer) *SniperLadder {
	return &SniperLadder{
		api:      api,
		ws:       ws,
		executor: ex,
		logger:   services.NewSpreadsheetLogger("SniperLadder"),

		initialTrapPrice: 0.45,
		initialTrapSize:  5,
		legThreshold:     4.95,
		ladderLevels: []struct {
			price float64
			size  float64
		}{
			{price: 0.37, size: 5},
			{price: 0.30, size: 5},
		},

		initialProfitTargetPercent: 0.025,
		pivotTimeBeforeEnd:         3 * time.Minute,
		maxSpread:                 0.4,
		oneCycleOnly:              true,

		partialSellTime: 90 * time.Second,
		fullSellTime:    60 * time.Second,
		priceHistLen:    100,

		phase:            "NEUTRAL",
		processedMatchIds: map[string]struct{}{},
		activeOrders:      map[string]struct{ side, typ string; price float64 }{},
		orderFillHistory:  map[string]float64{},
		lastTickTime:      time.Now(),
		tokenClaimer:      claimer,
		doneCh:            make(chan struct{}),
	}
}

func (b *SniperLadder) Done() <-chan struct{} { return b.doneCh }

// HandleUserWS exposes private WS handler to app wiring.
func (b *SniperLadder) HandleUserWS(ctx context.Context, v any) { b.handleUserFill(ctx, v) }

func (b *SniperLadder) Start(ctx context.Context) error {
	b.mu.Lock()
	b.isRunning = true
	b.mu.Unlock()

	fmt.Println("🚀 SNIPER LADDER BOT STARTED (v5 - SIT & WAIT)")
	fmt.Println("   🛡️ Logic: Event-Driven. Recalculates ONLY on Fills.")

	_ = b.hydrateState(ctx)

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

func (b *SniperLadder) Stop(ctx context.Context) error {
	_ = ctx
	fmt.Println("🛑 Stopping bot gracefully...")
	b.mu.Lock()
	b.isRunning = false
	if b.tokenClaimer != nil {
		b.tokenClaimer.StopPolling()
	}
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
	b.mu.Unlock()

	_ = b.cancelAllOrders(context.Background())
	services.DashboardManager.Log("✅ All orders cancelled during shutdown.", services.LogInfo)
	b.ws.Close()
	services.DashboardManager.Log("✅ WebSocket connections closed.", services.LogInfo)
	select {
	case <-b.doneCh:
	default:
		close(b.doneCh)
	}
	return nil
}

func (b *SniperLadder) safeRunLifecycle(ctx context.Context) {
	if err := b.runLifecycle(ctx); err != nil {
		services.DashboardManager.Log("CRITICAL CRASH: "+err.Error(), services.LogError)
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

func (b *SniperLadder) runLifecycle(ctx context.Context) error {
	b.mu.Lock()
	if !b.isRunning {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	currentTarget := core.GetTargetMarketSlug()
	nextTarget := core.GetNextMarketSlug()

	services.DashboardManager.Log(fmt.Sprintf("🔍 System Start. Checking Market: %s...", currentTarget.Slug[:min(10, len(currentTarget.Slug))]), services.LogInfo)
	target := nextTarget

	// SMART TARGETING & WALLET CHECK
	if tokens, err := b.api.GetMarketTokenIDs(currentTarget.Slug); err == nil && len(tokens) >= 2 {
		yes, no, _ := b.executor.GetPositions(ctx, tokens[0], tokens[1])
		if yes > 0.1 || no > 0.1 {
			services.DashboardManager.Log("🚨 ACTIVE TRADE FOUND! Forcing Current Market.", services.LogWarn)
			target = currentTarget
		}
	}

	b.mu.Lock()
	b.marketEndTimeMs = target.EndTimeMs
	b.currentMarketSlug = target.Slug
	b.mu.Unlock()

	// WAIT LOGIC: wait until 1 min before target start
	msUntilStart := core.GetMsUntil(target.StartTimeMs)
	msUntilPrep := msUntilStart - 60_000
	if msUntilPrep > 0 {
		waitSecs := int(math.Round(float64(msUntilPrep) / 1000))
		services.SetBotStatus("SniperLadder", fmt.Sprintf("⏳ Waiting %ds", waitSecs))
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
	services.DashboardManager.LogEvent("SNIPER", "✅ Tokens Acquired")

	// HYDRATE MEMORY
	b.mu.Lock()
	if b.filledCountYes == 0 && b.filledCountNo == 0 {
		b.mu.Unlock()
		_ = b.hydrateState(ctx)
	} else {
		services.DashboardManager.Log(fmt.Sprintf("🧠 Memory Active: [%.1fY / %.1fN]. Skipping scan.", b.filledCountYes, b.filledCountNo), services.LogInfo)
		b.mu.Unlock()
	}

	// PLACE TRAPS (only if none and phase is NEUTRAL)
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

	// CONNECT SOCKET (market prices)
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

	// SCHEDULE END (based on marketEndTimeMs which can be overridden by hydrate)
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

func (b *SniperLadder) endCycleAndRestart(ctx context.Context) error {
	services.SetBotStatus("SniperLadder", "🔄 Switching cycle...")
	services.DashboardManager.LogEvent("SNIPER", "🏁 Market ended")
	_ = b.logIncompletePosition(ctx)
	b.ws.Close()

	if b.tokenClaimer != nil {
		fmt.Println("[SNIPER] 🎁 Starting automatic token claiming (polling every 5s)...")
		b.tokenClaimer.StartPolling()
	}

	b.resetState()

	if b.oneCycleOnly {
		fmt.Println("[SNIPER] ✅ Single Cycle Complete.")
		if b.tokenClaimer != nil {
			b.tokenClaimer.StopPolling()
		}
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

func (b *SniperLadder) resetState() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.phase = "NEUTRAL"
	b.firstFillTime = time.Time{}
	b.activeOrders = map[string]struct{ side, typ string; price float64 }{}
	b.orderFillHistory = map[string]float64{}
	b.filledCountYes = 0
	b.filledCountNo = 0
	b.avgCostYes = 0
	b.avgCostNo = 0
	b.priceYes = 0
	b.priceNo = 0
	b.marketEndTimeMs = 0
	b.currentMarketSlug = ""
	b.yesFills = nil
	b.noFills = nil
	b.lastLogTime = time.Time{}
	b.zeroDataStreak = 0
	b.processedMatchIds = map[string]struct{}{}
	b.isHedging = false
	b.partialSellDone = false
	b.fullSellDone = false
	b.priceHistoryYes = nil
	b.priceHistoryNo = nil
	b.lastTradeTime = time.Time{}
	if b.marketTimer != nil {
		b.marketTimer.Stop()
		b.marketTimer = nil
	}
}

func (b *SniperLadder) handleMarketTick(ctx context.Context, data any) error {
	b.mu.Lock()
	if b.processingTick {
		b.mu.Unlock()
		return nil
	}
	b.processingTick = true
	b.lastTickTime = time.Now()
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.processingTick = false
		b.mu.Unlock()
	}()

	// Update prices only
	m, _ := data.(map[string]any)
	if m != nil {
		evt := fmt.Sprint(m["event_type"])
		asset := fmt.Sprint(m["asset_id"])
		if evt == "price_change" || evt == "book" {
			p := parseFloat(fmt.Sprint(m["price"]))
			if p > 0 {
				b.mu.Lock()
				if asset == b.tokenIdYes {
					b.priceYes = p
					b.priceHistoryYes = append(b.priceHistoryYes, p)
					if len(b.priceHistoryYes) > b.priceHistLen {
						b.priceHistoryYes = b.priceHistoryYes[len(b.priceHistoryYes)-b.priceHistLen:]
					}
				}
				if asset == b.tokenIdNo {
					b.priceNo = p
					b.priceHistoryNo = append(b.priceHistoryNo, p)
					if len(b.priceHistoryNo) > b.priceHistLen {
						b.priceHistoryNo = b.priceHistoryNo[len(b.priceHistoryNo)-b.priceHistLen:]
					}
				}
				b.mu.Unlock()
			}
		}
	}
	b.updatePrices(data)

	// anomaly detection
	b.mu.Lock()
	isPriceZero := b.priceYes == 0 && b.priceNo == 0
	isAmnesia := b.phase != "NEUTRAL" && b.phase != "EXIT" && b.filledCountYes == 0 && b.filledCountNo == 0
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

	// PANIC MODE near market end
	b.mu.Lock()
	phase := b.phase
	endMs := b.marketEndTimeMs
	b.mu.Unlock()
	if phase != "NEUTRAL" && phase != "EXIT" && endMs > 0 {
		msUntilEnd := endMs - time.Now().UnixMilli()
		if msUntilEnd < int64(b.partialSellTime/time.Millisecond) && msUntilEnd >= int64(b.fullSellTime/time.Millisecond) && !b.partialSellDone {
			_ = b.checkPartialSell(ctx)
		}
		if msUntilEnd < int64(b.fullSellTime/time.Millisecond) && !b.fullSellDone {
			_ = b.checkFullSell(ctx)
		}
		if msUntilEnd < int64(b.pivotTimeBeforeEnd/time.Millisecond) {
			_ = b.executeLiquidityEscape(ctx)
		}
	}

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

func (b *SniperLadder) handleUserFill(ctx context.Context, data any) {
	m, ok := data.(map[string]any)
	if !ok || fmt.Sprint(m["event_type"]) != "trade" {
		return
	}
	unique := fmt.Sprint(firstAny(m, "match_id", "id"))
	if unique == "" {
		unique = fmt.Sprintf("%v-%v", m["timestamp"], m["order_id"])
	}

	b.mu.Lock()
	if _, ok := b.processedMatchIds[unique]; ok {
		b.mu.Unlock()
		return
	}
	b.processedMatchIds[unique] = struct{}{}
	if len(b.processedMatchIds) > 1000 {
		b.processedMatchIds = map[string]struct{}{}
	}
	b.lastTradeTime = time.Now()
	b.mu.Unlock()

	side := fmt.Sprint(m["side"]) // BUY/SELL
	size := parseFloat(fmt.Sprint(m["size"]))
	price := parseFloat(fmt.Sprint(m["price"]))
	asset := fmt.Sprint(m["asset_id"])
	orderID := fmt.Sprint(firstAny(m, "order_id", "orderId"))
	if size == 0 || price == 0 {
		return
	}

	// Critical: WS fills must advance per-order "already accounted" matched amount,
	// otherwise polling-based fill detection will double-count.
	if orderID != "" {
		b.mu.Lock()
		b.orderFillHistory[orderID] += size
		b.mu.Unlock()
	}
	services.DashboardManager.Log(fmt.Sprintf("⚡ FILL CONFIRMED: %s %.4g @ %.4f", side, size, price), services.LogTrade)

	b.mu.Lock()
	if asset == b.tokenIdYes {
		if side == "BUY" {
			b.filledCountYes += size
			b.avgCostYes += price * size
			b.yesFills = append(b.yesFills, services.Fill{Price: price, Size: size})
		} else {
			b.filledCountYes -= size
			if b.filledCountYes > 0 {
				b.avgCostYes = (b.avgCostYes/(b.filledCountYes+size))*b.filledCountYes
			} else {
				b.avgCostYes = 0
			}
		}
	} else if asset == b.tokenIdNo {
		if side == "BUY" {
			b.filledCountNo += size
			b.avgCostNo += price * size
			b.noFills = append(b.noFills, services.Fill{Price: price, Size: size})
		} else {
			b.filledCountNo -= size
			if b.filledCountNo > 0 {
				b.avgCostNo = (b.avgCostNo/(b.filledCountNo+size))*b.filledCountNo
			} else {
				b.avgCostNo = 0
			}
		}
	}
	b.mu.Unlock()

	b.checkExitOrHedge(ctx)
}

func (b *SniperLadder) checkExitOrHedge(ctx context.Context) {
	b.mu.Lock()
	yes := b.filledCountYes
	no := b.filledCountNo
	phase := b.phase
	b.mu.Unlock()

	// victory
	if yes > 0.1 && math.Abs(yes-no) < 0.1 {
		services.DashboardManager.Log("⚖️ PERFECT BALANCE REACHED. EXITING...", services.LogTrade)
		_ = b.transitionToExit(ctx)
		return
	}

	if phase == "NEUTRAL" {
		currentSideCount := math.Max(yes, no)
		if currentSideCount >= b.legThreshold {
			services.DashboardManager.Log("🦵 LEG COMPLETED. LADDER ACTIVATING!", services.LogTrade)
			b.mu.Lock()
			b.phase = "TRIGGERED"
			b.mu.Unlock()

			side := "YES"
			if no > yes {
				side = "NO"
			}
			_ = b.placeLadderOrders(ctx, side)
			_ = b.recalculateAndHedge(ctx)
		}
		return
	}

	_ = b.recalculateAndHedge(ctx)
}

func (b *SniperLadder) checkConnectionHealth(ctx context.Context) error {
	b.mu.Lock()
	if !b.isRunning {
		b.mu.Unlock()
		return nil
	}
	silence := time.Since(b.lastTickTime)
	b.mu.Unlock()
	if silence > 45*time.Second {
		services.DashboardManager.Log(fmt.Sprintf("💀 WATCHDOG: Connection Dead (%ds). Resetting Socket...", int(silence.Seconds())), services.LogError)
		return b.reconnectWebSocket(ctx)
	}
	return nil
}

func (b *SniperLadder) reconnectWebSocket(ctx context.Context) error {
	b.mu.Lock()
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	b.mu.Unlock()
	if yesID == "" || noID == "" {
		services.DashboardManager.Log("⚠️ Cannot reconnect: Tokens not set yet.", services.LogWarn)
		return nil
	}
	b.ws.SubscribeMarket([]string{yesID, noID}, func(data any) { _ = b.handleMarketTick(ctx, data) })
	b.mu.Lock()
	b.lastTickTime = time.Now()
	b.mu.Unlock()
	services.DashboardManager.Log("🔌 WebSocket Reset Successfully!", services.LogInfo)
	return nil
}

func (b *SniperLadder) transitionToExit(ctx context.Context) error {
	_ = b.cancelAllOrders(ctx)

	b.mu.Lock()
	totalCost := b.avgCostYes + b.avgCostNo
	yes := b.filledCountYes
	no := b.filledCountNo
	slug := b.currentMarketSlug
	yesFills := append([]services.Fill(nil), b.yesFills...)
	noFills := append([]services.Fill(nil), b.noFills...)
	b.phase = "EXIT"
	b.mu.Unlock()

	hedgedPairs := math.Min(yes, no)
	payout := hedgedPairs * 1.0
	profit := payout - totalCost
	profitPercent := 0.0
	if totalCost > 0 {
		profitPercent = profit / totalCost
	}
	services.DashboardManager.LogEvent("SNIPER", fmt.Sprintf("🏆 WIN! +$%.3f (%.1fY/%.1fN)", profit, yes, no))

	rec := services.TradeRecord{
		Time:            time.Now().UTC().Format(time.RFC3339),
		MarketSlug:      slug,
		CapitalInvested: totalCost,
		ProfitPercent:   profitPercent,
		CapitalEnd:      totalCost + profit,
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

	// force end cycle quickly (mirrors TS)
	go func() {
		time.Sleep(1 * time.Second)
		_ = b.endCycleAndRestart(context.Background())
	}()
	return nil
}

func (b *SniperLadder) logIncompletePosition(ctx context.Context) error {
	_ = ctx
	b.mu.Lock()
	hasPos := b.filledCountYes > 0 || b.filledCountNo > 0
	isBalanced := b.filledCountYes == b.filledCountNo && b.filledCountYes > 0
	if !hasPos || isBalanced || b.phase == "EXIT" {
		b.mu.Unlock()
		return nil
	}
	totalCost := b.avgCostYes + b.avgCostNo
	yes := b.filledCountYes
	no := b.filledCountNo
	slug := b.currentMarketSlug
	yesFills := append([]services.Fill(nil), b.yesFills...)
	noFills := append([]services.Fill(nil), b.noFills...)
	b.mu.Unlock()

	hedgedPairs := math.Min(yes, no)
	payout := hedgedPairs * 1.0
	profit := payout - totalCost
	profitPercent := 0.0
	if totalCost > 0 {
		profitPercent = profit / totalCost
	}
	services.DashboardManager.LogEvent("SNIPER", fmt.Sprintf("❌ LOSS! PnL: $%.3f", profit))
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
		Outcome:         services.OutcomeLoss,
	}
	b.logger.LogTrade(rec)
	return nil
}

func (b *SniperLadder) hydrateState(ctx context.Context) error {
	services.DashboardManager.Log("🧠 Scanning Wallet (Hydration)...", services.LogInfo)
	current := core.GetTargetMarketSlug()
	next := core.GetNextMarketSlug()
	found := false

	// CHECK CURRENT
	if tokens, err := b.api.GetMarketTokenIDs(current.Slug); err == nil && len(tokens) >= 2 {
		yes, no, _ := b.executor.GetPositions(ctx, tokens[0], tokens[1])
		if looseFloat(yes) > 0.01 || looseFloat(no) > 0.01 {
			services.DashboardManager.Log("🚨 FOUND SHARES IN CURRENT MARKET!", services.LogWarn)
			b.mu.Lock()
			b.currentMarketSlug = current.Slug
			b.marketEndTimeMs = current.EndTimeMs
			b.tokenIdYes, b.tokenIdNo = tokens[0], tokens[1]
			b.filledCountYes, b.filledCountNo = yes, no
			found = true
			b.mu.Unlock()
		}
	}

	// CHECK NEXT
	if !found {
		if tokens, err := b.api.GetMarketTokenIDs(next.Slug); err == nil && len(tokens) >= 2 {
			yes, no, _ := b.executor.GetPositions(ctx, tokens[0], tokens[1])
			if looseFloat(yes) > 0.01 || looseFloat(no) > 0.01 {
				services.DashboardManager.Log("✅ Found shares in NEXT market.", services.LogInfo)
				b.mu.Lock()
				b.currentMarketSlug = next.Slug
				b.marketEndTimeMs = next.EndTimeMs
				b.tokenIdYes, b.tokenIdNo = tokens[0], tokens[1]
				b.filledCountYes, b.filledCountNo = yes, no
				found = true
				b.mu.Unlock()
			}
		}
	}

	// fallback cost estimation + order recovery
	b.mu.Lock()
	if b.filledCountYes > 0 && b.avgCostYes == 0 {
		b.avgCostYes = b.filledCountYes * 0.5
	}
	if b.filledCountNo > 0 && b.avgCostNo == 0 {
		b.avgCostNo = b.filledCountNo * 0.5
	}
	slug := b.currentMarketSlug
	b.mu.Unlock()

	if slug != "" {
		// adopt ghost orders if executor supports it (paper executor does; live returns empty)
		open, _ := b.executor.GetOpenOrders(ctx, slug)
		b.mu.Lock()
		b.activeOrders = map[string]struct{ side, typ string; price float64 }{}
		for _, o := range open {
			typ := "TRAP"
			if parseFloat(o.Price) < 0.4 {
				typ = "LADDER"
			}
			b.activeOrders[o.ID] = struct{ side, typ string; price float64 }{side: o.Side, typ: typ, price: parseFloat(o.Price)}
		}
		if b.filledCountYes > 0 || b.filledCountNo > 0 {
			services.DashboardManager.Log(fmt.Sprintf("📝 MEMORY UPDATED: %.1fY / %.1fN (from Scan)", b.filledCountYes, b.filledCountNo), services.LogWarn)
			if b.phase == "NEUTRAL" {
				b.phase = "ACCUMULATION"
			}
		} else {
			services.DashboardManager.Log("🤷 Scan Complete. No active shares.", services.LogInfo)
		}
		b.mu.Unlock()
	}
	return nil
}

func (b *SniperLadder) checkFills(ctx context.Context) error {
	b.mu.Lock()
	orderIDs := make([]string, 0, len(b.activeOrders))
	for id := range b.activeOrders {
		orderIDs = append(orderIDs, id)
	}
	b.mu.Unlock()

	for _, id := range orderIDs {
		b.mu.Lock()
		info, ok := b.activeOrders[id]
		prev := b.orderFillHistory[id]
		b.mu.Unlock()
		if !ok {
			continue
		}

		st, _ := b.executor.GetOrderStatus(ctx, id)
		totalMatched := st.Matched
		newFill := totalMatched - prev
		if newFill > 0.0001 {
			fillPrice := st.AvgFillPrice
			if fillPrice == 0 {
				fillPrice = info.price
			}
			b.mu.Lock()
			b.orderFillHistory[id] = totalMatched
			b.mu.Unlock()
			_ = b.handleOrderFilled(ctx, id, info.side, info.typ, fillPrice, newFill)
		}
	}
	return nil
}

func (b *SniperLadder) handleOrderFilled(ctx context.Context, orderID string, side string, typ string, fillPrice float64, filledSize float64) error {
	services.DashboardManager.LogEvent("SNIPER", fmt.Sprintf("💥 %s %s @ $%.3f (x%.2f)", side, typ, fillPrice, filledSize))

	b.mu.Lock()
	if side == "YES" {
		b.filledCountYes += filledSize
		b.avgCostYes += fillPrice * filledSize
		b.yesFills = append(b.yesFills, services.Fill{Price: fillPrice, Size: filledSize})
		b.lastTradeTime = time.Now()
	} else {
		b.filledCountNo += filledSize
		b.avgCostNo += fillPrice * filledSize
		b.noFills = append(b.noFills, services.Fill{Price: fillPrice, Size: filledSize})
		b.lastTradeTime = time.Now()
	}
	phase := b.phase
	b.mu.Unlock()

	if phase == "NEUTRAL" {
		// immediate defense: kill opposite trap
		loser := "NO"
		if side == "NO" {
			loser = "YES"
		}
		_ = b.cancelOrdersByType(ctx, loser, "TRAP")

		currentSideCount := 0.0
		b.mu.Lock()
		if side == "YES" {
			currentSideCount = b.filledCountYes
		} else {
			currentSideCount = b.filledCountNo
		}
		b.mu.Unlock()
		if looseFloat(currentSideCount) < b.legThreshold {
			services.DashboardManager.Log(fmt.Sprintf("🛡️ Trap Hit. Holding Dust (%.2f). Waiting...", currentSideCount), services.LogInfo)
			return nil
		}

		services.DashboardManager.Log("🦵 LEG COMPLETED. LADDER ACTIVATING!", services.LogTrade)
		b.mu.Lock()
		b.firstFillTime = time.Now()
		b.phase = "TRIGGERED"
		b.mu.Unlock()
		_ = b.transitionToTriggered(ctx, side)
		return nil
	}

	// victory check
	b.mu.Lock()
	yes := b.filledCountYes
	no := b.filledCountNo
	b.mu.Unlock()
	if yes > 0.1 && math.Abs(yes-no) < 0.1 {
		services.DashboardManager.Log("⚖️ PERFECT BALANCE. CANCELLING ALL...", services.LogTrade)
		return b.transitionToExit(ctx)
	}

	return b.recalculateAndHedge(ctx)
}

func (b *SniperLadder) transitionToTriggered(ctx context.Context, winnerSide string) error {
	services.DashboardManager.Log(fmt.Sprintf("⚡ PHASE 2: TRIGGERED by %s", winnerSide), services.LogTrade)
	_ = b.placeLadderOrders(ctx, winnerSide)
	return b.recalculateAndHedge(ctx)
}

func (b *SniperLadder) placeLadderOrders(ctx context.Context, side string) error {
	b.mu.Lock()
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	priceYes, priceNo := b.priceYes, b.priceNo
	b.mu.Unlock()
	if yesID == "" || noID == "" {
		services.DashboardManager.Log("❌ Cannot place ladders: Tokens not set yet.", services.LogError)
		return nil
	}
	spread := priceYes + priceNo - 1.0
	if spread > b.maxSpread {
		services.DashboardManager.Log(fmt.Sprintf("⚠️ SKIPPING LADDERS: Spread %.3f > %.3f", spread, b.maxSpread), services.LogWarn)
		return nil
	}

	services.DashboardManager.Log(fmt.Sprintf("🪜 Placing LADDER for %s...", side), services.LogInfo)
	tokenID := yesID
	if side == "NO" {
		tokenID = noID
	}

	for _, lvl := range b.ladderLevels {
		services.DashboardManager.Log(fmt.Sprintf("📤 SENDING: LADDER %s %.2f @ $%.2f (GTC)", side, lvl.size, lvl.price), services.LogInfo)
		id, err := b.executor.PlaceOrder(ctx, execution.TradeParams{
			TokenID: tokenID,
			Side:    execution.SideBuy,
			Price:   lvl.price,
			Size:    lvl.size,
			Type:    execution.OrderTypeGTC,
		})
		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "balance") || strings.Contains(msg, "allowance") {
				services.DashboardManager.Log("💸 WALLET EMPTY during Ladder. Syncing...", services.LogWarn)
				_ = b.syncPositions(ctx)
				continue
			}
			services.DashboardManager.Log("❌ LADDER FAILED: "+msg, services.LogError)
			_ = b.handleExecutionError(ctx, err)
			continue
		}
		b.mu.Lock()
		b.activeOrders[id] = struct{ side, typ string; price float64 }{side: side, typ: "LADDER", price: lvl.price}
		b.mu.Unlock()
	}
	services.SetBotStatus("SniperLadder", fmt.Sprintf("🪜 Ladder Active (%s)", side))
	return nil
}

func (b *SniperLadder) recalculateAndHedge(ctx context.Context) error {
	b.mu.Lock()
	if b.isHedging {
		b.mu.Unlock()
		return nil
	}
	b.isHedging = true
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.isHedging = false
		b.mu.Unlock()
	}()

	b.mu.Lock()
	yes := b.filledCountYes
	no := b.filledCountNo
	endMs := b.marketEndTimeMs
	phase := b.phase
	totalSunkCost := b.avgCostYes + b.avgCostNo
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	b.mu.Unlock()

	var defenderSide string
	aggressorCount := 0.0
	defenderCount := 0.0
	if yes > no {
		defenderSide = "NO"
		aggressorCount, defenderCount = yes, no
	} else if no > yes {
		defenderSide = "YES"
		aggressorCount, defenderCount = no, yes
	} else {
		return nil
	}

	if endMs > 0 {
		msUntilEnd := endMs - time.Now().UnixMilli()
		if msUntilEnd < int64(b.pivotTimeBeforeEnd/time.Millisecond) {
			return nil
		}
	}

	sharesNeeded := aggressorCount - defenderCount
	actualGap := math.Abs(yes - no)
	if math.Abs(sharesNeeded-actualGap) > 0.5 {
		services.DashboardManager.Log(fmt.Sprintf("⚠️ MISMATCH: Calculated gap %.1f but actual gap is %.1f. Syncing positions...", sharesNeeded, actualGap), services.LogWarn)
		_ = b.syncPositions(ctx)
		b.mu.Lock()
		yes, no = b.filledCountYes, b.filledCountNo
		b.mu.Unlock()
		if yes > no {
			aggressorCount, defenderCount = yes, no
		} else if no > yes {
			aggressorCount, defenderCount = no, yes
		}
		sharesNeeded = aggressorCount - defenderCount
		services.DashboardManager.Log(fmt.Sprintf("✅ After sync: %.1fY / %.1fN → need %.1f shares", yes, no, sharesNeeded), services.LogInfo)
	}

	if sharesNeeded > 10 && (b.lastSyncTime.IsZero() || time.Since(b.lastSyncTime) > 5*time.Second) {
		_ = b.syncPositions(ctx)
		return nil
	}

	isInitialHedge := phase == "TRIGGERED"
	if !isInitialHedge {
		b.mu.Lock()
		b.phase = "ACCUMULATION"
		b.mu.Unlock()
	}

	if round2(sharesNeeded) <= 0.1 {
		return nil
	}
	if sharesNeeded < 2 {
		services.DashboardManager.Log(fmt.Sprintf("🛡️ Gap too small (%.1f). Ignoring to prevent ping-pong.", sharesNeeded), services.LogInfo)
		return nil
	}
	if sharesNeeded < 5 {
		fmt.Printf("[HEDGE] Gap is %.2f. Rounding to 5 to force execution.\n", sharesNeeded)
		sharesNeeded = 5
	}

	totalRevenueTarget := aggressorCount * 1.0
	anchorProfitMargin := 0.02
	totalCostAllowed := totalRevenueTarget / (1 + anchorProfitMargin)
	remainingBudget := totalCostAllowed - totalSunkCost

	anchorPrice := 0.0
	if isInitialHedge {
		anchorPrice = 0.52
		services.DashboardManager.Log(fmt.Sprintf("🎯 INITIAL HEDGE: Using fixed price $0.52 (Trap was $%.2f)", b.initialTrapPrice), services.LogInfo)
	} else {
		raw := remainingBudget / sharesNeeded
		anchorPrice = math.Max(0.01, math.Min(0.99, math.Floor(raw*100)/100))
		be := 1.0 - b.initialTrapPrice
		if anchorPrice > be {
			anchorPrice = math.Min(anchorPrice, be)
		}
	}

	tokenID := yesID
	if defenderSide == "NO" {
		tokenID = noID
	}

	// cancel old hedges on defender
	_ = b.cancelOrdersByType(ctx, defenderSide, "HEDGE")

	ordersToPlace := b.calculateOrdersNeeded(sharesNeeded)
	totalSharesToPlace := float64(ordersToPlace) * 5
	if totalSharesToPlace < sharesNeeded-0.5 {
		services.DashboardManager.Log(fmt.Sprintf("⚠️ WARNING: Placing %.1f shares but need %.1f. This may be insufficient!", totalSharesToPlace, sharesNeeded), services.LogWarn)
	}

	mostFar := anchorPrice
	mostNear := anchorPrice
	if ordersToPlace > 1 {
		if isInitialHedge {
			spread := 0.025
			mostFar = math.Max(0.01, anchorPrice*(1-spread))
			mostNear = math.Min(0.99, anchorPrice*(1+spread))
		} else {
			nearMargin := 0.025
			totalCostAllowedNear := totalRevenueTarget / (1 + nearMargin)
			remainingBudgetNear := totalCostAllowedNear - totalSunkCost
			mostNear = math.Max(0.01, math.Min(0.99, remainingBudgetNear/sharesNeeded))

			mostFar = anchorPrice
			if ordersToPlace >= 3 {
				farMargin := 0.015
				totalCostAllowedFar := totalRevenueTarget / (1 + farMargin)
				remainingBudgetFar := totalCostAllowedFar - totalSunkCost
				mostFar = math.Max(0.01, math.Min(0.99, remainingBudgetFar/sharesNeeded))
			}
			be := 1.0 - b.initialTrapPrice
			mostFar = math.Min(mostFar, be)
			mostNear = math.Min(mostNear, be)
			anchorPrice = math.Min(anchorPrice, be)
		}
	}

	ladder := b.calculateHedgeLadder(sharesNeeded, anchorPrice, mostFar, mostNear, ordersToPlace)
	services.DashboardManager.Log(fmt.Sprintf("🛡️ HEDGE LADDER: Need %.1f %s | Placing %d order(s)", sharesNeeded, defenderSide, len(ladder)), services.LogInfo)
	for _, lvl := range ladder {
		_ = b.placeHedgeOrder(ctx, defenderSide, tokenID, lvl.price, lvl.size, !isInitialHedge, isInitialHedge)
		time.Sleep(150 * time.Millisecond)
	}
	return nil
}

func (b *SniperLadder) calculateOrdersNeeded(sharesNeeded float64) int {
	if sharesNeeded < 10 {
		return 1
	}
	if math.Mod(sharesNeeded, 5) == 0 {
		return int(sharesNeeded / 5)
	}
	return 2
}

func (b *SniperLadder) calculateHedgeLadder(sharesNeeded float64, anchorPrice float64, mostFar float64, mostNear float64, ordersToPlace int) []struct {
	price float64
	size  float64
} {
	var ladder []struct {
		price float64
		size  float64
	}
	if ordersToPlace == 1 {
		ladder = append(ladder, struct{ price, size float64 }{price: anchorPrice, size: sharesNeeded})
		return ladder
	}
	if ordersToPlace == 2 {
		remainder := math.Mod(sharesNeeded, 5)
		base := 5.0
		if remainder > 0 {
			ladder = append(ladder, struct{ price, size float64 }{price: anchorPrice, size: remainder + base})
			ladder = append(ladder, struct{ price, size float64 }{price: mostNear, size: base})
		} else {
			ladder = append(ladder, struct{ price, size float64 }{price: anchorPrice, size: base})
			ladder = append(ladder, struct{ price, size float64 }{price: mostNear, size: base})
		}
		return ladder
	}
	sizePer := math.Floor(sharesNeeded / float64(ordersToPlace))
	rem := math.Mod(sharesNeeded, float64(ordersToPlace))
	prices := []float64{mostFar, anchorPrice, mostNear}
	for i := 0; i < ordersToPlace; i++ {
		sz := sizePer
		if i == 0 && rem > 0 {
			sz += rem
		}
		ladder = append(ladder, struct{ price, size float64 }{price: prices[i%3], size: sz})
	}
	return ladder
}

func (b *SniperLadder) placeHedgeOrder(ctx context.Context, side string, tokenID string, price float64, size float64, useAggressiveBuffer bool, isInitialHedge bool) error {
	finalPrice := price
	if useAggressiveBuffer {
		finalPrice = math.Min(0.99, price+0.01)
	}
	if !isInitialHedge {
		be := 1.0 - b.initialTrapPrice
		if finalPrice > be+0.01 {
			services.DashboardManager.Log(fmt.Sprintf("⚠️ CAPPING PRICE: $%.2f -> $%.2f to preserve capital.", finalPrice, be), services.LogWarn)
			finalPrice = be
		}
	}
	services.DashboardManager.Log(fmt.Sprintf("📤 SENDING: %s HEDGE %.2f @ $%.2f (GTC)", side, size, finalPrice), services.LogInfo)
	id, err := b.executor.PlaceOrder(ctx, execution.TradeParams{
		TokenID: tokenID,
		Side:    execution.SideBuy,
		Price:   finalPrice,
		Size:    size,
		Type:    execution.OrderTypeGTC,
	})
	if err != nil {
		services.DashboardManager.Log("❌ HEDGE FAILED: "+err.Error(), services.LogError)
		_ = b.handleExecutionError(ctx, err)
		return err
	}
	b.mu.Lock()
	b.activeOrders[id] = struct{ side, typ string; price float64 }{side: side, typ: "HEDGE", price: finalPrice}
	b.mu.Unlock()
	return nil
}

func (b *SniperLadder) handleExecutionError(ctx context.Context, err error) error {
	msg := err.Error()
	if strings.Contains(msg, "balance") || strings.Contains(msg, "insufficient") || strings.Contains(msg, "allowance") {
		services.DashboardManager.Log(fmt.Sprintf("🚨 CRITICAL: Blockchain rejected order (%s). Memory likely wrong. FORCING SYNC.", msg), services.LogError)
		yes, no, _ := b.executor.GetPositions(ctx, b.tokenIdYes, b.tokenIdNo)
		b.mu.Lock()
		b.filledCountYes = yes
		b.filledCountNo = no
		services.DashboardManager.Log(fmt.Sprintf("🔄 HARD RESET: Synced to Reality (Y:%.1f/N:%.1f)", yes, no), services.LogWarn)
		b.mu.Unlock()
	}
	return nil
}

func (b *SniperLadder) cancelOrdersByType(ctx context.Context, side string, typ string) error {
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

func (b *SniperLadder) cancelAllOrders(ctx context.Context) error {
	b.mu.Lock()
	var ids []string
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

func (b *SniperLadder) placeTrapOrders(ctx context.Context) error {
	b.mu.Lock()
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	b.mu.Unlock()
	if yesID == "" || noID == "" {
		services.DashboardManager.Log("❌ Cannot place traps: Tokens not set yet.", services.LogError)
		return nil
	}
	services.DashboardManager.Log(fmt.Sprintf("🪤 Placing TRAP orders @ $%.2f...", b.initialTrapPrice), services.LogInfo)
	size := b.initialTrapSize
	idYes, err := b.executor.PlaceOrder(ctx, execution.TradeParams{TokenID: yesID, Side: execution.SideBuy, Price: b.initialTrapPrice, Size: size, Type: execution.OrderTypeGTC})
	if err != nil {
		services.DashboardManager.Log("❌ TRAP FAILED: "+err.Error(), services.LogError)
		return err
	}
	idNo, err := b.executor.PlaceOrder(ctx, execution.TradeParams{TokenID: noID, Side: execution.SideBuy, Price: b.initialTrapPrice, Size: size, Type: execution.OrderTypeGTC})
	if err != nil {
		services.DashboardManager.Log("❌ TRAP FAILED: "+err.Error(), services.LogError)
		return err
	}
	b.mu.Lock()
	b.activeOrders[idYes] = struct{ side, typ string; price float64 }{side: "YES", typ: "TRAP", price: b.initialTrapPrice}
	b.activeOrders[idNo] = struct{ side, typ string; price float64 }{side: "NO", typ: "TRAP", price: b.initialTrapPrice}
	b.mu.Unlock()
	services.SetBotStatus("SniperLadder", fmt.Sprintf("🪤 Trap Set ($%.2f)", b.initialTrapPrice))
	return nil
}

func (b *SniperLadder) updatePrices(data any) {
	m, ok := data.(map[string]any)
	if !ok {
		return
	}
	updates, _ := m["price_changes"].([]any)
	if updates == nil {
		updates, _ = m["updates"].([]any)
	}
	if asks, ok := m["asks"].([]any); ok && len(asks) > 0 {
		asset := fmt.Sprint(m["asset_id"])
		first, _ := asks[0].(map[string]any)
		if first != nil {
			p := parseFloat(fmt.Sprint(first["price"]))
			if p > 0 {
				b.mu.Lock()
				if asset == b.tokenIdYes {
					b.priceYes = p
					b.priceHistoryYes = append(b.priceHistoryYes, p)
					if len(b.priceHistoryYes) > b.priceHistLen {
						b.priceHistoryYes = b.priceHistoryYes[len(b.priceHistoryYes)-b.priceHistLen:]
					}
				}
				if asset == b.tokenIdNo {
					b.priceNo = p
					b.priceHistoryNo = append(b.priceHistoryNo, p)
					if len(b.priceHistoryNo) > b.priceHistLen {
						b.priceHistoryNo = b.priceHistoryNo[len(b.priceHistoryNo)-b.priceHistLen:]
					}
				}
				b.mu.Unlock()
			}
		}
	}
	for _, u := range updates {
		um, _ := u.(map[string]any)
		if um == nil {
			continue
		}
		if um["price"] == nil {
			continue
		}
		asset := fmt.Sprint(firstAny(um, "asset_id"))
		if asset == "" {
			asset = fmt.Sprint(m["asset_id"])
		}
		p := parseFloat(fmt.Sprint(um["price"]))
		if fmt.Sprint(um["side"]) == "SELL" && p > 0 {
			b.mu.Lock()
			if asset == b.tokenIdYes {
				b.priceYes = p
				b.priceHistoryYes = append(b.priceHistoryYes, p)
				if len(b.priceHistoryYes) > b.priceHistLen {
					b.priceHistoryYes = b.priceHistoryYes[len(b.priceHistoryYes)-b.priceHistLen:]
				}
			}
			if asset == b.tokenIdNo {
				b.priceNo = p
				b.priceHistoryNo = append(b.priceHistoryNo, p)
				if len(b.priceHistoryNo) > b.priceHistLen {
					b.priceHistoryNo = b.priceHistoryNo[len(b.priceHistoryNo)-b.priceHistLen:]
				}
			}
			b.mu.Unlock()
		}
	}
}

func (b *SniperLadder) checkPartialSell(ctx context.Context) error {
	b.mu.Lock()
	if b.phase == "EXIT" || b.partialSellDone {
		b.mu.Unlock()
		return nil
	}
	yes, no := looseFloat(b.filledCountYes), looseFloat(b.filledCountNo)
	hasYes := yes > 0.1
	hasNo := no > 0.1
	imbalance := math.Abs(yes - no)
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	b.mu.Unlock()

	if hasYes && hasNo && imbalance > 0.5 {
		services.DashboardManager.Log(fmt.Sprintf("🚨 PANIC MODE 1: PARTIAL SELL | Unbalanced: %.1fY / %.1fN (imbalance: %.2f)", yes, no, imbalance), services.LogWarn)
		_ = b.cancelAllOrders(ctx)

		sideToSell := "NO"
		tokenID := noID
		if yes > no {
			sideToSell = "YES"
			tokenID = yesID
		}
		services.DashboardManager.Log(fmt.Sprintf("📤 SELLING %.2f %s to balance positions", imbalance, sideToSell), services.LogWarn)
		_, err := b.executor.PlaceOrder(ctx, execution.TradeParams{
			TokenID: tokenID,
			Side:    execution.SideSell,
			Price:   0.01,
			Size:    imbalance,
			Type:    execution.OrderTypeGTC,
		})
		if err == nil {
			b.mu.Lock()
			if sideToSell == "YES" {
				b.filledCountYes -= imbalance
			} else {
				b.filledCountNo -= imbalance
			}
			b.partialSellDone = true
			b.mu.Unlock()
		} else {
			services.DashboardManager.Log("❌ PARTIAL SELL FAILED: "+err.Error(), services.LogError)
		}
	}
	return nil
}

func (b *SniperLadder) checkFullSell(ctx context.Context) error {
	b.mu.Lock()
	if b.phase == "EXIT" || b.fullSellDone {
		b.mu.Unlock()
		return nil
	}
	yes := looseFloat(b.filledCountYes)
	no := looseFloat(b.filledCountNo)
	hasYes := yes > 0.1
	hasNo := no > 0.1
	isNaked := (hasYes && !hasNo) || (!hasYes && hasNo)
	hYes := append([]float64(nil), b.priceHistoryYes...)
	hNo := append([]float64(nil), b.priceHistoryNo...)
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	b.mu.Unlock()
	if !isNaked {
		return nil
	}

	validYes := filterPositive(hYes)
	validNo := filterPositive(hNo)
	if len(validYes) < 60 || len(validNo) < 60 {
		services.DashboardManager.Log(fmt.Sprintf("⚠️ PANIC MODE: Insufficient price history (%dY / %dN). Waiting...", len(validYes), len(validNo)), services.LogWarn)
		return nil
	}
	avgYes := avg(validYes)
	avgNo := avg(validNo)

	services.DashboardManager.Log(fmt.Sprintf("🚨 PANIC MODE 2: FULL SELL | Naked Position: %.1fY / %.1fN", yes, no), services.LogWarn)
	services.DashboardManager.Log(fmt.Sprintf("   Avg Prices: YES $%.3f / NO $%.3f", avgYes, avgNo), services.LogWarn)

	holdingSide := ""
	holdingAmount := 0.0
	isWinning := false
	if hasYes && !hasNo {
		holdingSide = "YES"
		holdingAmount = yes
		isWinning = avgYes > avgNo
	} else if hasNo && !hasYes {
		holdingSide = "NO"
		holdingAmount = no
		isWinning = avgNo > avgYes
	}
	if holdingSide == "" || holdingAmount <= 0 {
		return nil
	}
	if isWinning {
		services.DashboardManager.Log(fmt.Sprintf("✅ HOLDING: %s is WINNING. Keeping position.", holdingSide), services.LogInfo)
		b.mu.Lock()
		b.fullSellDone = true
		b.mu.Unlock()
		return nil
	}

	services.DashboardManager.Log(fmt.Sprintf("📤 SELLING ALL: %s is LOSING. Dumping %.2f shares.", holdingSide, holdingAmount), services.LogWarn)
	_ = b.cancelAllOrders(ctx)
	tokenID := yesID
	if holdingSide == "NO" {
		tokenID = noID
	}
	_, err := b.executor.PlaceOrder(ctx, execution.TradeParams{TokenID: tokenID, Side: execution.SideSell, Price: 0.01, Size: holdingAmount, Type: execution.OrderTypeGTC})
	if err != nil {
		services.DashboardManager.Log("❌ FULL SELL FAILED: "+err.Error(), services.LogError)
		return nil
	}
	b.mu.Lock()
	if holdingSide == "YES" {
		b.filledCountYes = 0
	} else {
		b.filledCountNo = 0
	}
	b.fullSellDone = true
	b.mu.Unlock()
	return nil
}

func (b *SniperLadder) executeLiquidityEscape(ctx context.Context) error {
	b.mu.Lock()
	if b.phase == "EXIT" {
		b.mu.Unlock()
		return nil
	}
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	endMs := b.marketEndTimeMs
	yes := b.filledCountYes
	no := b.filledCountNo
	priceYes, priceNo := b.priceYes, b.priceNo
	b.mu.Unlock()
	if yesID == "" || noID == "" {
		services.DashboardManager.Log("⚠️ Cannot execute escape: Tokens not set yet.", services.LogWarn)
		return nil
	}
	if endMs-time.Now().UnixMilli() > 60_000 {
		return nil
	}
	netExposure := math.Abs(yes - no)
	if looseFloat(netExposure) > 5 {
		sideToSell := "NO"
		tokenID := noID
		currentPrice := priceNo
		if yes > no {
			sideToSell = "YES"
			tokenID = yesID
			currentPrice = priceYes
		}
		if currentPrice > 0.05 {
			services.DashboardManager.Log(fmt.Sprintf("📤 SENDING: DUMP %.1f %s (Risk Reduce)", netExposure, sideToSell), services.LogWarn)
			_, err := b.executor.PlaceOrder(ctx, execution.TradeParams{TokenID: tokenID, Side: execution.SideSell, Price: 0.01, Size: netExposure, Type: execution.OrderTypeGTC})
			if err != nil {
				services.DashboardManager.Log("❌ DUMP FAILED: "+err.Error(), services.LogError)
			} else {
				b.mu.Lock()
				if sideToSell == "YES" {
					b.filledCountYes -= netExposure
				} else {
					b.filledCountNo -= netExposure
				}
				b.mu.Unlock()
			}
		}
	}
	return nil
}

func (b *SniperLadder) updateDashboard() {
	b.mu.Lock()
	if b.priceYes == 0 && b.priceNo == 0 {
		b.mu.Unlock()
		return
	}
	totalCost := b.avgCostYes + b.avgCostNo
	yesAvg := 0.0
	noAvg := 0.0
	if b.filledCountYes > 0 {
		yesAvg = b.avgCostYes / b.filledCountYes
	}
	if b.filledCountNo > 0 {
		noAvg = b.avgCostNo / b.filledCountNo
	}
	var pnl *float64
	isComplete := false
	if b.phase == "EXIT" {
		v := math.Min(b.filledCountYes, b.filledCountNo)*1.0 - totalCost
		pnl = &v
		isComplete = true
	} else if b.filledCountYes > 0 && b.filledCountNo > 0 {
		v := math.Min(b.filledCountYes, b.filledCountNo)*1.0 - totalCost
		pnl = &v
	}
	yesVal := b.filledCountYes * b.priceYes
	noVal := b.filledCountNo * b.priceNo
	unreal := yesVal + noVal - totalCost
	unrealPtr := &unreal

	var hedgePrice *float64
	var hedgeSide *string
	for _, v := range b.activeOrders {
		if v.typ == "HEDGE" {
			hp := v.price
			hs := v.side
			hedgePrice = &hp
			hedgeSide = &hs
		}
	}
	state := services.BotState{
		Name:          "SniperLadder",
		Phase:         b.phase,
		PriceYes:      b.priceYes,
		PriceNo:       b.priceNo,
		YesShares:     b.filledCountYes,
		YesAvg:        yesAvg,
		NoShares:      b.filledCountNo,
		NoAvg:         noAvg,
		ActiveOrders:  len(b.activeOrders),
		PnL:           pnl,
		UnrealizedPnL: unrealPtr,
		HedgePrice:    hedgePrice,
		HedgeSide:     hedgeSide,
		IsComplete:    isComplete,
	}
	b.mu.Unlock()
	services.DashboardManager.Update(state)
}

func (b *SniperLadder) syncPositions(ctx context.Context) error {
	b.mu.Lock()
	if b.tokenIdYes == "" || b.tokenIdNo == "" {
		b.mu.Unlock()
		return nil
	}
	if !b.lastTradeTime.IsZero() && time.Since(b.lastTradeTime) < 60*time.Second {
		b.mu.Unlock()
		return nil
	}
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	memYes, memNo := b.filledCountYes, b.filledCountNo
	b.mu.Unlock()

	yes, no, err := b.executor.GetPositions(ctx, yesID, noID)
	if err != nil {
		return nil
	}
	// zero-protection
	if yes == 0 && memYes > 0 {
		services.DashboardManager.Log(fmt.Sprintf("🛡️ IGNORING API LAG: API says 0 YES, Memory says %.1f. Keeping Memory.", memYes), services.LogWarn)
		return nil
	}
	if no == 0 && memNo > 0 {
		services.DashboardManager.Log(fmt.Sprintf("🛡️ IGNORING API LAG: API says 0 NO, Memory says %.1f. Keeping Memory.", memNo), services.LogWarn)
		return nil
	}
	driftYes := yes - memYes
	driftNo := no - memNo
	if math.Abs(driftYes) > 0.1 || math.Abs(driftNo) > 0.1 {
		services.DashboardManager.Log(fmt.Sprintf("⚖️ SYNC FIX: YES %.1f->%.1f | NO %.1f->%.1f", memYes, yes, memNo, no), services.LogWarn)
		b.mu.Lock()
		b.filledCountYes = yes
		b.filledCountNo = no
		if b.filledCountYes > 0 && b.avgCostYes == 0 {
			b.avgCostYes = b.filledCountYes * 0.5
		}
		if b.filledCountNo > 0 && b.avgCostNo == 0 {
			b.avgCostNo = b.filledCountNo * 0.5
		}
		b.lastSyncTime = time.Now()
		b.mu.Unlock()
	}
	return nil
}

// --- helpers ---

func looseFloat(n float64) float64 { return math.Round(n*100) / 100 }

func round2(n float64) float64 { return math.Round(n*100) / 100 }

func filterPositive(xs []float64) []float64 {
	out := make([]float64, 0, len(xs))
	for _, v := range xs {
		if v > 0 {
			out = append(out, v)
		}
	}
	return out
}

func avg(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range xs {
		sum += v
	}
	return sum / float64(len(xs))
}

