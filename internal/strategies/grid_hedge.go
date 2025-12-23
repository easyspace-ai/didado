package strategies

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"polymarketbot/internal/core"
	"polymarketbot/internal/execution"
	"polymarketbot/internal/services"
)

// GridHedge:
// - 监听某一侧（默认 YES）的价格
// - 当价格从下向上穿越用户定义网格价时，发送“市价”买入（FOK，价格=0.99）
// - 买入后立即挂另一侧（NO）的对冲买单，用于锁定利润（双向持仓）
//
// 说明：
// - Polymarket CLOB 需要指定价格；这里用 FOK + 0.99 近似市价买入。
// - 对冲单是“买另一侧”，最终形成 YES/NO 双向持仓，锁定 1 美元兑付差额的利润。
type GridHedge struct {
	api      *services.PolymarketService
	ws       *services.WebSocketService
	executor execution.Executor

	mu        sync.Mutex
	isRunning bool

	// --- CONFIG ---
	tradeSide             string // "YES" or "NO" (默认 YES)
	gridLevels            []float64
	entrySize             float64
	profitLock            float64 // 每对锁定的最低利润（美元），对冲单会按 1-entryPrice-profitLock 挂买价
	marketBuyMaxPrice     float64 // FOK 买入最大价格（近似市价）
	oneCycleOnly          bool
	acknowledgesPaperFill bool // paper 模式下自动填充 entry 的 FOK 订单

	// --- STATE ---
	currentMarketSlug string
	marketEndTimeMs   int64

	tokenIdYes string
	tokenIdNo  string
	priceYes   float64
	priceNo    float64

	lastPrice float64
	triggered map[float64]struct{} // level -> triggered

	activeOrders map[string]struct {
		side  string // YES|NO
		typ   string // HEDGE
		price float64
		size  float64
	}
	orderFillHistory  map[string]float64
	processedMatchIds map[string]struct{}

	filledCountYes float64
	filledCountNo  float64
	avgCostYes     float64
	avgCostNo      float64

	processingTick bool
	lastTickTime   time.Time
	zeroDataStreak int

	healthTicker *time.Ticker
	syncTicker   *time.Ticker
	marketTimer  *time.Timer

	doneCh chan struct{}
}

func NewGridHedge(api *services.PolymarketService, ws *services.WebSocketService, ex execution.Executor) *GridHedge {
	b := &GridHedge{
		api:      api,
		ws:       ws,
		executor: ex,

		tradeSide:         "YES",
		gridLevels:        []float64{0.62, 0.67, 0.72},
		entrySize:         5,
		profitLock:        0.01,
		marketBuyMaxPrice: 0.99,
		oneCycleOnly:      false,

		triggered: map[float64]struct{}{},
		activeOrders: map[string]struct {
			side, typ   string
			price, size float64
		}{},
		orderFillHistory:  map[string]float64{},
		processedMatchIds: map[string]struct{}{},
		lastTickTime:      time.Now(),
		doneCh:            make(chan struct{}),
	}

	// 允许用环境变量简单覆写（不改全局 Config，保持策略自包含）
	// GRID_LEVELS="0.62,0.67,0.72"
	if s := strings.TrimSpace(os.Getenv("GRID_LEVELS")); s != "" {
		if lvls := parseGridLevels(s); len(lvls) > 0 {
			b.gridLevels = lvls
		}
	}
	// GRID_ENTRY_SIZE=5
	if s := strings.TrimSpace(os.Getenv("GRID_ENTRY_SIZE")); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil && v > 0 {
			b.entrySize = v
		}
	}
	// GRID_PROFIT_LOCK=0.01
	if s := strings.TrimSpace(os.Getenv("GRID_PROFIT_LOCK")); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil && v >= 0 {
			b.profitLock = v
		}
	}
	// GRID_TRADE_SIDE=YES|NO
	if s := strings.TrimSpace(os.Getenv("GRID_TRADE_SIDE")); s != "" {
		u := strings.ToUpper(s)
		if u == "YES" || u == "NO" {
			b.tradeSide = u
		}
	}
	// GRID_ONE_CYCLE_ONLY=true|false
	if strings.TrimSpace(os.Getenv("GRID_ONE_CYCLE_ONLY")) == "true" {
		b.oneCycleOnly = true
	}
	// GRID_PAPER_AUTO_FILL=true|false (默认 true)
	if strings.TrimSpace(os.Getenv("GRID_PAPER_AUTO_FILL")) != "false" {
		b.acknowledgesPaperFill = true
	}

	sort.Float64s(b.gridLevels)
	return b
}

func (b *GridHedge) Done() <-chan struct{} { return b.doneCh }

// HandleUserWS exposes private WS handler to app wiring.
func (b *GridHedge) HandleUserWS(ctx context.Context, v any) { b.handleUserFill(ctx, v) }

func (b *GridHedge) Start(ctx context.Context) error {
	b.mu.Lock()
	b.isRunning = true
	b.mu.Unlock()

	services.DashboardManager.Log("🚀 GRID HEDGE 策略启动", services.LogInfo)
	services.DashboardManager.Log(fmt.Sprintf("   触发侧=%s, 网格=%v, 每次买入=%.4g, 锁利=%.3f", b.tradeSide, b.gridLevels, b.entrySize, b.profitLock), services.LogInfo)

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

func (b *GridHedge) Stop(ctx context.Context) error {
	_ = ctx
	services.DashboardManager.Log("🛑 GridHedge 停止中...", services.LogWarn)

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
	b.mu.Unlock()

	_ = b.cancelAllOrders(context.Background())
	b.ws.Close()
	select {
	case <-b.doneCh:
	default:
		close(b.doneCh)
	}
	return nil
}

func (b *GridHedge) safeRunLifecycle(ctx context.Context) {
	if err := b.runLifecycle(ctx); err != nil {
		services.DashboardManager.Log("❌ GridHedge lifecycle error: "+err.Error(), services.LogError)
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

func (b *GridHedge) runLifecycle(ctx context.Context) error {
	b.mu.Lock()
	running := b.isRunning
	b.mu.Unlock()
	if !running {
		return nil
	}

	current := core.GetTargetMarketSlug()
	next := core.GetNextMarketSlug()
	target := next

	// 如果当前市场已有持仓，继续交易当前市场（与现有策略一致）
	if tokens, err := b.api.GetMarketTokenIDs(current.Slug); err == nil && len(tokens) >= 2 {
		yes, no, _ := b.executor.GetPositions(ctx, tokens[0], tokens[1])
		if yes > 0.1 || no > 0.1 {
			target = current
			services.DashboardManager.Log("🚨 检测到当前市场已有持仓，将继续在当前市场运行 GridHedge", services.LogWarn)
		}
	}

	b.mu.Lock()
	b.currentMarketSlug = target.Slug
	b.marketEndTimeMs = target.EndTimeMs
	b.mu.Unlock()

	// 获取 token ids
	tokenIDs, err := b.api.GetMarketTokenIDs(target.Slug)
	if err != nil || len(tokenIDs) < 2 {
		services.DashboardManager.Log(fmt.Sprintf("❌ 获取 tokens 失败: %s，10s 后重试", target.Slug), services.LogError)
		select {
		case <-time.After(10 * time.Second):
			return b.runLifecycle(ctx)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	b.mu.Lock()
	b.tokenIdYes, b.tokenIdNo = tokenIDs[0], tokenIDs[1]
	b.lastPrice = 0
	b.triggered = map[float64]struct{}{}
	b.mu.Unlock()

	// 订阅市场行情
	b.ws.SubscribeMarket([]string{tokenIDs[0], tokenIDs[1]}, func(data any) { _ = b.handleMarketTick(ctx, data) })

	// 市场结束计时
	b.mu.Lock()
	msUntilEnd := core.GetMsUntil(b.marketEndTimeMs)
	if b.marketTimer != nil {
		b.marketTimer.Stop()
	}
	b.marketTimer = time.AfterFunc(time.Duration(msUntilEnd+2000)*time.Millisecond, func() {
		_ = b.endCycleAndRestart(context.Background())
	})
	b.mu.Unlock()

	services.DashboardManager.Log(fmt.Sprintf("⏰ GridHedge 计时器: %.1f 分钟后切换", float64(msUntilEnd)/60000), services.LogInfo)
	return nil
}

func (b *GridHedge) endCycleAndRestart(ctx context.Context) error {
	services.SetBotStatus("GridHedge", "🏁 Market ended, switching...")
	_ = b.cancelAllOrders(ctx)
	b.ws.Close()
	b.resetState()

	if b.oneCycleOnly {
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

func (b *GridHedge) resetState() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastPrice = 0
	b.triggered = map[float64]struct{}{}
	b.activeOrders = map[string]struct {
		side, typ   string
		price, size float64
	}{}
	b.orderFillHistory = map[string]float64{}
	b.processedMatchIds = map[string]struct{}{}
	b.priceYes, b.priceNo = 0, 0
	b.currentMarketSlug = ""
	b.marketEndTimeMs = 0
	b.tokenIdYes, b.tokenIdNo = "", ""
	b.processingTick = false
	b.zeroDataStreak = 0
}

func (b *GridHedge) handleMarketTick(ctx context.Context, data any) error {
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

	b.updatePrices(data)

	// 异常：长时间没价格
	b.mu.Lock()
	isPriceZero := b.priceYes == 0 && b.priceNo == 0
	if isPriceZero {
		b.zeroDataStreak++
		if b.zeroDataStreak > 15 {
			b.zeroDataStreak = 0
			b.mu.Unlock()
			services.DashboardManager.Log("⚠️ [GridHedge] 行情疑似断流，重连 WS...", services.LogWarn)
			_ = b.reconnectWebSocket(ctx)
			return nil
		}
	} else {
		b.zeroDataStreak = 0
	}
	tradeSide := b.tradeSide
	priceYes, priceNo := b.priceYes, b.priceNo
	prev := b.lastPrice
	b.mu.Unlock()

	_ = b.checkFills(ctx)

	current := priceYes
	if tradeSide == "NO" {
		current = priceNo
	}
	if current <= 0 {
		return nil
	}

	// 首次初始化 lastPrice
	if prev <= 0 {
		b.mu.Lock()
		b.lastPrice = current
		b.mu.Unlock()
		b.updateDashboard()
		return nil
	}

	// 检测“上穿”网格价
	level := b.detectCrossUp(prev, current)
	if level > 0 {
		services.DashboardManager.Log(fmt.Sprintf("📈 上穿网格 %.2f（%.4f -> %.4f），触发买入", level, prev, current), services.LogTrade)
		_ = b.onCrossUp(ctx, level)
	}

	b.mu.Lock()
	b.lastPrice = current
	b.mu.Unlock()

	b.updateDashboard()
	return nil
}

func (b *GridHedge) detectCrossUp(prev, cur float64) float64 {
	b.mu.Lock()
	levels := append([]float64(nil), b.gridLevels...)
	triggered := b.triggered
	b.mu.Unlock()

	for _, lvl := range levels {
		if _, ok := triggered[lvl]; ok {
			continue
		}
		// prev < lvl && cur >= lvl
		if prev < lvl && cur >= lvl {
			return lvl
		}
	}
	return 0
}

func (b *GridHedge) onCrossUp(ctx context.Context, level float64) error {
	b.mu.Lock()
	if _, ok := b.triggered[level]; ok {
		b.mu.Unlock()
		return nil
	}
	b.triggered[level] = struct{}{}
	tradeSide := b.tradeSide
	entrySize := b.entrySize
	marketBuyMax := b.marketBuyMaxPrice
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	priceYes, priceNo := b.priceYes, b.priceNo
	b.mu.Unlock()

	if yesID == "" || noID == "" {
		return nil
	}

	// 1) 市价买入 tradeSide（FOK + 0.99）
	entryToken := yesID
	entryLabel := "YES"
	entryAsk := priceYes
	hedgeToken := noID
	hedgeLabel := "NO"
	if tradeSide == "NO" {
		entryToken = noID
		entryLabel = "NO"
		entryAsk = priceNo
		hedgeToken = yesID
		hedgeLabel = "YES"
	}

	entryOrderID, err := b.executor.PlaceOrder(ctx, execution.TradeParams{
		TokenID: entryToken,
		Side:    execution.SideBuy,
		Price:   clamp01(marketBuyMax),
		Size:    entrySize,
		Type:    execution.OrderTypeFOK,
	})
	if err != nil {
		services.DashboardManager.Log("❌ 市价买入失败: "+err.Error(), services.LogError)
		return err
	}

	// paper 模式：自动填充“市价”单，便于策略可跑
	if b.acknowledgesPaperFill {
		if p, ok := b.executor.(execution.PaperFillExecutor); ok {
			fillPx := entryAsk
			if fillPx <= 0 {
				// 无行情时用网格价作为近似
				fillPx = level
			}
			_ = p.ExecutePaperFill(ctx, entryOrderID, clamp01(fillPx), entrySize)
		}
	}

	// 等待/读取成交量，决定对冲单规模与对冲价格
	filledQty, fillPrice := b.waitImmediateFill(ctx, entryOrderID, entrySize)
	if filledQty <= 0 {
		services.DashboardManager.Log(fmt.Sprintf("⚠️ entry 订单未成交（%s id=%s），跳过对冲", entryLabel, entryOrderID), services.LogWarn)
		return nil
	}
	services.DashboardManager.Log(fmt.Sprintf("✅ entry 成交: %s %.4g @ $%.4f", entryLabel, filledQty, fillPrice), services.LogTrade)

	b.recordFill(entryLabel, fillPrice, filledQty)

	// 2) 立即挂“反方向”对冲买单（买另一侧），锁定利润
	// 利润 = 1 - (entryPrice + hedgePrice)
	// 要锁定 profitLock：hedgePrice <= 1 - entryPrice - profitLock
	b.mu.Lock()
	profitLock := b.profitLock
	b.mu.Unlock()
	hedgePrice := clamp01(1.0 - fillPrice - profitLock)
	// 避免极端：太小会永远不成交；这里给个下限 0.01（与项目其他策略一致）
	if hedgePrice < 0.01 {
		hedgePrice = 0.01
	}

	hedgeID, err := b.executor.PlaceOrder(ctx, execution.TradeParams{
		TokenID: hedgeToken,
		Side:    execution.SideBuy,
		Price:   hedgePrice,
		Size:    filledQty,
		Type:    execution.OrderTypeGTC,
	})
	if err != nil {
		services.DashboardManager.Log("❌ 对冲挂单失败: "+err.Error(), services.LogError)
		return err
	}
	b.mu.Lock()
	b.activeOrders[hedgeID] = struct {
		side, typ   string
		price, size float64
	}{side: hedgeLabel, typ: "HEDGE", price: hedgePrice, size: filledQty}
	b.mu.Unlock()
	services.DashboardManager.Log(fmt.Sprintf("🛡️ 已挂对冲单: BUY %s %.4g @ $%.4f (锁利≈$%.3f)", hedgeLabel, filledQty, hedgePrice, 1.0-(fillPrice+hedgePrice)), services.LogInfo)
	return nil
}

func (b *GridHedge) waitImmediateFill(ctx context.Context, orderID string, expected float64) (qty float64, px float64) {
	// live 场景：FOK 应该很快出结果；这里最多等 ~1s
	deadline := time.Now().Add(1200 * time.Millisecond)
	for {
		st, _ := b.executor.GetOrderStatus(ctx, orderID)
		if st.Matched > 0 {
			p := st.AvgFillPrice
			if p <= 0 {
				p = 0.5
			}
			return st.Matched, p
		}
		// FOK 订单可能直接取消（未成交）
		if st.Cancelled {
			return 0, 0
		}
		if time.Now().After(deadline) {
			// 超时，返回当前已知状态
			if st.Matched > 0 {
				return st.Matched, st.AvgFillPrice
			}
			// 对于 paper：如果没 auto fill，这里也会走到 0
			if expected > 0 {
				return 0, 0
			}
			return 0, 0
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (b *GridHedge) recordFill(side string, price float64, size float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if side == "YES" {
		b.filledCountYes += size
		b.avgCostYes += price * size
		return
	}
	b.filledCountNo += size
	b.avgCostNo += price * size
}

func (b *GridHedge) updatePrices(data any) {
	m, ok := data.(map[string]any)
	if !ok {
		return
	}
	asks, _ := m["asks"].([]any)
	assetID := fmt.Sprint(m["asset_id"])
	if assetID == "" || len(asks) == 0 {
		return
	}
	first, _ := asks[0].(map[string]any)
	if first == nil {
		return
	}
	p := parseFloat(fmt.Sprint(first["price"]))
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

func (b *GridHedge) checkFills(ctx context.Context) error {
	b.mu.Lock()
	orders := make([]struct {
		id   string
		info struct {
			side, typ string
			price     float64
			size      float64
		}
	}, 0, len(b.activeOrders))
	for id, info := range b.activeOrders {
		orders = append(orders, struct {
			id   string
			info struct {
				side, typ string
				price     float64
				size      float64
			}
		}{id: id, info: info})
	}
	b.mu.Unlock()

	for _, o := range orders {
		st, _ := b.executor.GetOrderStatus(ctx, o.id)
		totalMatched := st.Matched

		b.mu.Lock()
		prev := b.orderFillHistory[o.id]
		b.mu.Unlock()

		newFill := totalMatched - prev
		if newFill > 0.0001 {
			fillPrice := st.AvgFillPrice
			if fillPrice <= 0 {
				fillPrice = o.info.price
			}
			b.mu.Lock()
			b.orderFillHistory[o.id] = totalMatched
			b.mu.Unlock()

			services.DashboardManager.Log(fmt.Sprintf("💥 对冲成交: %s %.4g @ $%.4f", o.info.side, newFill, fillPrice), services.LogTrade)
			b.recordFill(o.info.side, fillPrice, newFill)

			// 完全成交/取消：移除
			if st.Cancelled || (o.info.size > 0 && totalMatched >= o.info.size-0.0001) {
				b.mu.Lock()
				delete(b.activeOrders, o.id)
				delete(b.orderFillHistory, o.id)
				b.mu.Unlock()
			}
		}
	}
	return nil
}

func (b *GridHedge) handleUserFill(ctx context.Context, data any) {
	_ = ctx
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
	if len(b.processedMatchIds) > 2000 {
		b.processedMatchIds = map[string]struct{}{}
	}
	b.mu.Unlock()

	// 只关心 BUY（我们只用买来做双向对冲）
	side := strings.ToUpper(fmt.Sprint(m["side"]))
	if side != "BUY" {
		return
	}
	asset := fmt.Sprint(m["asset_id"])
	price := parseFloat(fmt.Sprint(m["price"]))
	size := parseFloat(fmt.Sprint(m["size"]))
	orderID := fmt.Sprint(firstAny(m, "order_id", "orderId"))
	if asset == "" || price <= 0 || size <= 0 {
		return
	}

	b.mu.Lock()
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	b.mu.Unlock()
	if asset != yesID && asset != noID {
		return
	}
	tokenSide := "YES"
	if asset == noID {
		tokenSide = "NO"
	}
	// 避免 WS + 轮询重复计数：推进该订单已计入的 matched
	if orderID != "" {
		b.mu.Lock()
		b.orderFillHistory[orderID] += size
		b.mu.Unlock()
	}
	b.recordFill(tokenSide, price, size)
}

func (b *GridHedge) cancelAllOrders(ctx context.Context) error {
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
		delete(b.orderFillHistory, id)
		b.mu.Unlock()
	}
	return nil
}

func (b *GridHedge) reconnectWebSocket(ctx context.Context) error {
	_ = ctx
	b.ws.Close()
	time.Sleep(1 * time.Second)
	b.mu.Lock()
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	b.mu.Unlock()
	if yesID == "" || noID == "" {
		return nil
	}
	b.ws.SubscribeMarket([]string{yesID, noID}, func(data any) { _ = b.handleMarketTick(context.Background(), data) })
	return nil
}

func (b *GridHedge) checkConnectionHealth(ctx context.Context) error {
	b.mu.Lock()
	if !b.isRunning {
		b.mu.Unlock()
		return nil
	}
	silence := time.Since(b.lastTickTime)
	b.mu.Unlock()
	if silence > 45*time.Second {
		services.DashboardManager.Log(fmt.Sprintf("💀 [GridHedge] WS 静默 %ds，重连...", int(silence.Seconds())), services.LogWarn)
		return b.reconnectWebSocket(ctx)
	}
	return nil
}

func (b *GridHedge) syncPositions(ctx context.Context) error {
	b.mu.Lock()
	yesID, noID := b.tokenIdYes, b.tokenIdNo
	memYes, memNo := b.filledCountYes, b.filledCountNo
	b.mu.Unlock()
	if yesID == "" || noID == "" {
		return nil
	}

	yes, no, err := b.executor.GetPositions(ctx, yesID, noID)
	if err != nil {
		return nil
	}
	// 轻量纠偏：只在差异较大时更新，避免 API 延迟导致“归零误报”
	if math.Abs(yes-memYes) > 0.5 || math.Abs(no-memNo) > 0.5 {
		b.mu.Lock()
		b.filledCountYes = yes
		b.filledCountNo = no
		// cost 无法精确恢复，保留原值
		b.mu.Unlock()
	}
	return nil
}

func (b *GridHedge) updateDashboard() {
	b.mu.Lock()
	py, pn := b.priceYes, b.priceNo
	y, n := b.filledCountYes, b.filledCountNo
	b.mu.Unlock()
	services.SetBotStatus("GridHedge", fmt.Sprintf("P(Y/N)=%.3f/%.3f | Pos(Y/N)=%.2f/%.2f", py, pn, y, n))
}

func parseGridLevels(s string) []float64 {
	parts := strings.Split(s, ",")
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		f, err := strconv.ParseFloat(p, 64)
		if err != nil {
			continue
		}
		f = clamp01(f)
		if f <= 0 {
			continue
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Float64s(out)
	// 去重
	uniq := out[:0]
	var last float64
	for i, v := range out {
		if i == 0 || math.Abs(v-last) > 1e-9 {
			uniq = append(uniq, v)
			last = v
		}
	}
	return append([]float64(nil), uniq...)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 0.99 {
		return 0.99
	}
	// 保留两位小数（与其他策略一致的“报价粒度”）
	return math.Floor(v*100) / 100
}
