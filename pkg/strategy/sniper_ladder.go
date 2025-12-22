package strategy

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/polymarketbot-go/pkg/core"
	"github.com/polymarketbot-go/pkg/executor"
	"github.com/polymarketbot-go/pkg/services"
	"github.com/polymarketbot-go/pkg/types"
)

// SniperLadder Sniper Ladder策略
// 实现陷阱+阶梯+动态对冲的交易策略
type SniperLadder struct {
	api      *services.PolymarketService
	ws       *services.WebSocketService
	executor executor.IExecutor
	logger   *services.SpreadsheetLogger
	
	// 配置参数
	initialTrapPrice    float64
	initialTrapSize     float64
	legThreshold        float64
	ladderLevels        []LadderLevel
	pivotTimeBeforeEnd  int64
	maxSpread           float64
	oneCycleOnly        bool
	
	// 状态
	phase              string
	firstFillTime      int64
	currentMarketSlug  string
	marketEndTimeMs    int64
	
	// 订单和持仓
	processedMatchIds  map[string]bool
	activeOrders       map[string]OrderInfo
	orderFillHistory   map[string]float64
	
	filledCountYes     float64
	filledCountNo      float64
	avgCostYes         float64
	avgCostNo          float64
	yesFills           []Fill
	noFills            []Fill
	
	tokenIDYes         string
	tokenIDNo          string
	priceYes           float64
	priceNo            float64
	
	// 控制标志
	isRunning          bool
	isProcessingTick   bool
	isHedging          bool
	lastTickTime       int64
	lastSyncTime       int64
	
	mu                 sync.RWMutex
}

// LadderLevel 阶梯等级
type LadderLevel struct {
	Price float64
	Size  float64
}

// OrderInfo 订单信息
type OrderInfo struct {
	Side  string
	Type  string
	Price float64
}

// Fill 成交记录
type Fill struct {
	Price float64
	Size  float64
}

// NewSniperLadder 创建新的Sniper Ladder策略
func NewSniperLadder(
	api *services.PolymarketService,
	ws *services.WebSocketService,
	exec executor.IExecutor,
) *SniperLadder {
	return &SniperLadder{
		api:      api,
		ws:       ws,
		executor: exec,
		logger:   services.NewSpreadsheetLogger("SniperLadder"),
		
		// 配置
		initialTrapPrice:   0.45,
		initialTrapSize:    5,
		legThreshold:       4.95,
		ladderLevels: []LadderLevel{
			{Price: 0.37, Size: 5},
			{Price: 0.30, Size: 5},
		},
		pivotTimeBeforeEnd: 180000,
		maxSpread:          0.4,
		oneCycleOnly:       true,
		
		// 初始化状态
		phase:             "NEUTRAL",
		processedMatchIds: make(map[string]bool),
		activeOrders:      make(map[string]OrderInfo),
		orderFillHistory:  make(map[string]float64),
		yesFills:          make([]Fill, 0),
		noFills:           make([]Fill, 0),
		lastTickTime:      time.Now().UnixMilli(),
	}
}

// Start 启动策略
func (s *SniperLadder) Start() error {
	s.isRunning = true
	fmt.Println("🚀 SNIPER LADDER 机器人已启动")
	fmt.Println("   🛡️ 逻辑: 事件驱动。仅在成交时重新计算。")
	
	// 启动生命周期
	go s.runLifecycle()
	
	return nil
}

// Stop 停止策略
func (s *SniperLadder) Stop() error {
	fmt.Println("🛑 优雅停止机器人...")
	s.isRunning = false
	
	// 取消所有订单
	if err := s.cancelAllOrders(); err != nil {
		fmt.Printf("❌ 取消订单失败: %v\n", err)
	}
	
	// 关闭WebSocket
	s.ws.Close()
	
	fmt.Println("✅ 机器人已停止。")
	return nil
}

// runLifecycle 运行生命周期
func (s *SniperLadder) runLifecycle() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("❌ 生命周期崩溃: %v\n", r)
			// 重试
			time.Sleep(5 * time.Second)
			if s.isRunning {
				go s.runLifecycle()
			}
		}
	}()
	
	marketMath := core.NewMarketMath()
	
	// 获取目标市场
	target := marketMath.GetNextMarketSlug()
	s.currentMarketSlug = target.Slug
	s.marketEndTimeMs = target.EndTimeMs
	
	fmt.Printf("🔍 系统启动。检查市场: %s...\n", target.Slug[:10])
	
	// 等待市场准备
	msUntilStart := marketMath.GetMsUntil(target.StartTimeMs)
	msUntilPrep := msUntilStart - 60000
	if msUntilPrep > 0 {
		waitSecs := msUntilPrep / 1000
		fmt.Printf("⏳ 等待 %d 秒直到市场准备...\n", waitSecs)
		time.Sleep(time.Duration(msUntilPrep) * time.Millisecond)
	}
	
	// 获取代币
	tokenIds, err := s.api.GetMarketTokenIds(target.Slug)
	if err != nil || len(tokenIds) < 2 {
		fmt.Printf("❌ 获取代币失败: %v\n", err)
		return
	}
	
	s.tokenIDYes = tokenIds[0]
	s.tokenIDNo = tokenIds[1]
	fmt.Println("✅ 代币已获取")
	
	// 下陷阱单
	if err := s.placeTrapOrders(); err != nil {
		fmt.Printf("❌ 下陷阱单失败: %v\n", err)
		return
	}
	
	// 订阅市场数据
	s.ws.SubscribeMarket([]string{s.tokenIDYes, s.tokenIDNo}, s.handleMarketTick)
	
	// 设置市场结束定时器
	msUntilEnd := marketMath.GetMsUntil(s.marketEndTimeMs)
	fmt.Printf("⏰ 定时器设置为 %.1f 分钟\n", float64(msUntilEnd)/60000)
	
	time.AfterFunc(time.Duration(msUntilEnd+2000)*time.Millisecond, func() {
		s.endCycleAndRestart()
	})
	
	// 启动健康检查
	go s.healthCheckLoop()
	go s.syncPositionsLoop()
}

// handleMarketTick 处理市场数据更新
func (s *SniperLadder) handleMarketTick(msg types.WebSocketMessage) {
	if s.isProcessingTick {
		return
	}
	s.isProcessingTick = true
	defer func() { s.isProcessingTick = false }()
	
	s.lastTickTime = time.Now().UnixMilli()
	
	// 更新价格
	if msg.EventType == "price_change" || msg.EventType == "book" {
		if msg.AssetID == s.tokenIDYes && msg.Price != "" {
			if price := parseFloat(msg.Price); price > 0 {
				s.priceYes = price
			}
		}
		if msg.AssetID == s.tokenIDNo && msg.Price != "" {
			if price := parseFloat(msg.Price); price > 0 {
				s.priceNo = price
			}
		}
	}
	
	// 定期检查订单状态
	go s.checkFills()
	
	// 检查是否接近市场结束（pivot逻辑）
	if s.phase != "NEUTRAL" && s.phase != "EXIT" && s.marketEndTimeMs > 0 {
		msUntilEnd := s.marketEndTimeMs - time.Now().UnixMilli()
		if msUntilEnd < s.pivotTimeBeforeEnd {
			s.executeLiquidityEscape()
		}
	}
	
	// 更新仪表板
	s.updateDashboard()
}

// placeTrapOrders 下陷阱单
func (s *SniperLadder) placeTrapOrders() error {
	if s.tokenIDYes == "" || s.tokenIDNo == "" {
		return fmt.Errorf("代币未设置")
	}
	
	fmt.Printf("🪤 下陷阱单 @ $%.2f...\n", s.initialTrapPrice)
	
	// YES陷阱
	idYes, err := s.executor.PlaceOrder(types.TradeParams{
		TokenID: s.tokenIDYes,
		Side:    types.OrderSideBuy,
		Price:   s.initialTrapPrice,
		Size:    s.initialTrapSize,
		Type:    types.OrderTypeGTC,
	})
	if err != nil {
		return fmt.Errorf("下YES陷阱单失败: %w", err)
	}
	
	// NO陷阱
	idNo, err := s.executor.PlaceOrder(types.TradeParams{
		TokenID: s.tokenIDNo,
		Side:    types.OrderSideBuy,
		Price:   s.initialTrapPrice,
		Size:    s.initialTrapSize,
		Type:    types.OrderTypeGTC,
	})
	if err != nil {
		return fmt.Errorf("下NO陷阱单失败: %w", err)
	}
	
	s.activeOrders[idYes] = OrderInfo{Side: "YES", Type: "TRAP", Price: s.initialTrapPrice}
	s.activeOrders[idNo] = OrderInfo{Side: "NO", Type: "TRAP", Price: s.initialTrapPrice}
	
	fmt.Printf("✅ 陷阱已设置 ($%.2f)\n", s.initialTrapPrice)
	return nil
}

// checkFills 检查订单成交
func (s *SniperLadder) checkFills() {
	for orderID, orderInfo := range s.activeOrders {
		status, err := s.executor.GetOrderStatus(orderID)
		if err != nil {
			continue
		}
		
		// 计算新成交量
		previouslyProcessed := s.orderFillHistory[orderID]
		newFillAmount := status.Matched - previouslyProcessed
		
		if newFillAmount > 0.0001 {
			fillPrice := status.AvgFillPrice
			if fillPrice == 0 {
				fillPrice = orderInfo.Price
			}
			
			s.orderFillHistory[orderID] = status.Matched
			s.handleOrderFilled(orderID, orderInfo, fillPrice, newFillAmount)
		}
	}
}

// handleOrderFilled 处理订单成交
func (s *SniperLadder) handleOrderFilled(orderID string, orderInfo OrderInfo, fillPrice, filledSize float64) {
	fmt.Printf("💥 %s %s @ $%.3f (x%.2f)\n", orderInfo.Side, orderInfo.Type, fillPrice, filledSize)
	
	// 更新持仓
	if orderInfo.Side == "YES" {
		s.filledCountYes += filledSize
		s.avgCostYes += fillPrice * filledSize
		s.yesFills = append(s.yesFills, Fill{Price: fillPrice, Size: filledSize})
	} else {
		s.filledCountNo += filledSize
		s.avgCostNo += fillPrice * filledSize
		s.noFills = append(s.noFills, Fill{Price: fillPrice, Size: filledSize})
	}
	
	// 检查阶段转换
	if s.phase == "NEUTRAL" {
		// 立即取消对面的陷阱
		loserSide := "NO"
		if orderInfo.Side == "NO" {
			loserSide = "YES"
		}
		s.cancelOrdersByType(loserSide, "TRAP")
		
		currentSideCount := s.filledCountYes
		if orderInfo.Side == "NO" {
			currentSideCount = s.filledCountNo
		}
		
		if currentSideCount >= s.legThreshold {
			fmt.Println("🦵 腿部完成。阶梯激活！")
			s.firstFillTime = time.Now().UnixMilli()
			s.phase = "TRIGGERED"
			s.transitionToTriggered(orderInfo.Side)
		}
	} else {
		// 检查是否达到完美平衡
		if s.filledCountYes > 0.1 && math.Abs(s.filledCountYes-s.filledCountNo) < 0.1 {
			fmt.Println("⚖️ 完美平衡。退出...")
			s.transitionToExit()
			return
		}
		
		// 重新计算对冲
		s.recalculateAndHedge()
	}
}

// transitionToTriggered 转换到触发阶段
func (s *SniperLadder) transitionToTriggered(winnerSide string) {
	fmt.Printf("⚡ 阶段2: 被%s触发\n", winnerSide)
	s.placeLadderOrders(winnerSide)
	s.recalculateAndHedge()
}

// placeLadderOrders 下阶梯单
func (s *SniperLadder) placeLadderOrders(side string) {
	if s.tokenIDYes == "" || s.tokenIDNo == "" {
		return
	}
	
	currentSpread := s.priceYes + s.priceNo - 1.0
	if currentSpread > s.maxSpread {
		fmt.Printf("⚠️ 跳过阶梯: 价差%.3f > %.2f\n", currentSpread, s.maxSpread)
		return
	}
	
	fmt.Printf("🪜 为%s下阶梯单...\n", side)
	
	tokenID := s.tokenIDYes
	if side == "NO" {
		tokenID = s.tokenIDNo
	}
	
	for _, level := range s.ladderLevels {
		id, err := s.executor.PlaceOrder(types.TradeParams{
			TokenID: tokenID,
			Side:    types.OrderSideBuy,
			Price:   level.Price,
			Size:    level.Size,
			Type:    types.OrderTypeGTC,
		})
		if err != nil {
			fmt.Printf("❌ 阶梯失败: %v\n", err)
			continue
		}
		
		s.activeOrders[id] = OrderInfo{Side: side, Type: "LADDER", Price: level.Price}
	}
}

// recalculateAndHedge 重新计算并对冲
func (s *SniperLadder) recalculateAndHedge() {
	if s.isHedging {
		return
	}
	s.isHedging = true
	defer func() { s.isHedging = false }()
	
	var aggressorSide, defenderSide string
	var aggressorCount, defenderCount float64
	
	if s.filledCountYes > s.filledCountNo {
		aggressorSide = "YES"
		aggressorCount = s.filledCountYes
		defenderSide = "NO"
		defenderCount = s.filledCountNo
	} else if s.filledCountNo > s.filledCountYes {
		aggressorSide = "NO"
		aggressorCount = s.filledCountNo
		defenderSide = "YES"
		defenderCount = s.filledCountYes
	} else {
		return // 平衡
	}
	
	sharesNeeded := aggressorCount - defenderCount
	
	if sharesNeeded < 2 {
		return // 差距太小
	}
	
	// 计算对冲价格
	anchorPrice := 0.52 // 初始对冲固定价格
	if s.phase != "TRIGGERED" {
		// 后续对冲根据预算计算
		totalRevenueTarget := aggressorCount * 1.0
		totalSunkCost := s.avgCostYes + s.avgCostNo
		anchorProfitMargin := 0.02
		totalCostAllowed := totalRevenueTarget / (1 + anchorProfitMargin)
		remainingBudget := totalCostAllowed - totalSunkCost
		anchorPrice = remainingBudget / sharesNeeded
		
		// 应用盈亏平衡保护
		breakEvenPrice := 1.0 - s.initialTrapPrice
		if anchorPrice > breakEvenPrice {
			anchorPrice = breakEvenPrice
		}
	}
	
	tokenID := s.tokenIDNo
	if defenderSide == "YES" {
		tokenID = s.tokenIDYes
	}
	
	// 取消旧的对冲单
	s.cancelOrdersByType(defenderSide, "HEDGE")
	
	// 下新的对冲单
	fmt.Printf("🛡️ 对冲: 需要%.1f %s @ $%.2f\n", sharesNeeded, defenderSide, anchorPrice)
	
	id, err := s.executor.PlaceOrder(types.TradeParams{
		TokenID: tokenID,
		Side:    types.OrderSideBuy,
		Price:   anchorPrice,
		Size:    sharesNeeded,
		Type:    types.OrderTypeGTC,
	})
	if err != nil {
		fmt.Printf("❌ 对冲失败: %v\n", err)
		return
	}
	
	s.activeOrders[id] = OrderInfo{Side: defenderSide, Type: "HEDGE", Price: anchorPrice}
}

// cancelOrdersByType 按类型取消订单
func (s *SniperLadder) cancelOrdersByType(side, orderType string) {
	toCancel := make([]string, 0)
	for id, info := range s.activeOrders {
		if info.Side == side && info.Type == orderType {
			toCancel = append(toCancel, id)
		}
	}
	
	for _, id := range toCancel {
		s.executor.CancelOrder(id)
		delete(s.activeOrders, id)
	}
}

// cancelAllOrders 取消所有订单
func (s *SniperLadder) cancelAllOrders() error {
	for id := range s.activeOrders {
		s.executor.CancelOrder(id)
	}
	s.activeOrders = make(map[string]OrderInfo)
	return nil
}

// transitionToExit 转换到退出阶段
func (s *SniperLadder) transitionToExit() {
	s.cancelAllOrders()
	
	totalCost := s.avgCostYes + s.avgCostNo
	hedgedPairs := math.Min(s.filledCountYes, s.filledCountNo)
	payout := hedgedPairs * 1.0
	profit := payout - totalCost
	
	fmt.Printf("🏆 胜利! +$%.3f (%.1fY/%.1fN)\n", profit, s.filledCountYes, s.filledCountNo)
	
	s.phase = "EXIT"
	
	// 记录交易
	s.logTrade(profit, "WIN")
}

// executeLiquidityEscape 执行流动性逃逸
func (s *SniperLadder) executeLiquidityEscape() {
	if s.phase == "EXIT" {
		return
	}
	
	netExposure := math.Abs(s.filledCountYes - s.filledCountNo)
	if netExposure > 5 {
		sideToSell := "YES"
		tokenID := s.tokenIDYes
		if s.filledCountNo > s.filledCountYes {
			sideToSell = "NO"
			tokenID = s.tokenIDNo
		}
		
		fmt.Printf("📤 抛售 %.1f %s（风险降低）\n", netExposure, sideToSell)
		
		s.executor.PlaceOrder(types.TradeParams{
			TokenID: tokenID,
			Side:    types.OrderSideSell,
			Price:   0.01,
			Size:    netExposure,
			Type:    types.OrderTypeGTC,
		})
	}
}

// endCycleAndRestart 结束周期并重启
func (s *SniperLadder) endCycleAndRestart() {
	fmt.Println("🏁 市场已结束")
	
	// 记录未完成的持仓
	if s.phase != "EXIT" && (s.filledCountYes > 0 || s.filledCountNo > 0) {
		totalCost := s.avgCostYes + s.avgCostNo
		hedgedPairs := math.Min(s.filledCountYes, s.filledCountNo)
		profit := hedgedPairs*1.0 - totalCost
		s.logTrade(profit, "LOSS")
	}
	
	s.ws.Close()
	s.resetState()
	
	if s.oneCycleOnly {
		fmt.Println("✅ 单周期完成。")
		return
	}
	
	// 重启新周期
	go s.runLifecycle()
}

// logTrade 记录交易
func (s *SniperLadder) logTrade(profit float64, outcome string) {
	yesAvg := services.CalculateAvg(convertFills(s.yesFills))
	noAvg := services.CalculateAvg(convertFills(s.noFills))
	
	record := types.TradeRecord{
		Time:             time.Now(),
		MarketSlug:       s.currentMarketSlug,
		CapitalInvested:  s.avgCostYes + s.avgCostNo,
		ProfitPercent:    0,
		CapitalEnd:       s.avgCostYes + s.avgCostNo + profit,
		YesPositions:     services.FormatPositions(convertFills(s.yesFills)),
		YesAvg:           yesAvg,
		YesTotal:         s.filledCountYes,
		NoPositions:      services.FormatPositions(convertFills(s.noFills)),
		NoAvg:            noAvg,
		NoTotal:          s.filledCountNo,
		PnL:              profit,
		Outcome:          outcome,
	}
	
	s.logger.LogTrade(record)
}

// resetState 重置状态
func (s *SniperLadder) resetState() {
	s.phase = "NEUTRAL"
	s.firstFillTime = 0
	s.activeOrders = make(map[string]OrderInfo)
	s.orderFillHistory = make(map[string]float64)
	s.filledCountYes = 0
	s.filledCountNo = 0
	s.avgCostYes = 0
	s.avgCostNo = 0
	s.priceYes = 0
	s.priceNo = 0
	s.yesFills = make([]Fill, 0)
	s.noFills = make([]Fill, 0)
	s.processedMatchIds = make(map[string]bool)
	s.isHedging = false
}

// updateDashboard 更新仪表板
func (s *SniperLadder) updateDashboard() {
	totalCost := s.avgCostYes + s.avgCostNo
	yesAvg := 0.0
	if s.filledCountYes > 0 {
		yesAvg = s.avgCostYes / s.filledCountYes
	}
	noAvg := 0.0
	if s.filledCountNo > 0 {
		noAvg = s.avgCostNo / s.filledCountNo
	}
	
	unrealizedPnL := s.filledCountYes*s.priceYes + s.filledCountNo*s.priceNo - totalCost
	
	dashboard := services.GetDashboard()
	dashboard.Update(services.BotStatus{
		Name:          "SniperLadder",
		Phase:         s.phase,
		PriceYes:      s.priceYes,
		PriceNo:       s.priceNo,
		YesShares:     s.filledCountYes,
		YesAvg:        yesAvg,
		NoShares:      s.filledCountNo,
		NoAvg:         noAvg,
		ActiveOrders:  len(s.activeOrders),
		UnrealizedPnL: unrealizedPnL,
		IsComplete:    s.phase == "EXIT",
	})
}

// healthCheckLoop 健康检查循环
func (s *SniperLadder) healthCheckLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	
	for range ticker.C {
		if !s.isRunning {
			return
		}
		
		silenceDuration := time.Now().UnixMilli() - s.lastTickTime
		if silenceDuration > 45000 {
			fmt.Printf("💀 监视器: 连接死亡（%.0f秒）。重置...\n", float64(silenceDuration)/1000)
			// TODO: 重连WebSocket
		}
	}
}

// syncPositionsLoop 同步持仓循环
func (s *SniperLadder) syncPositionsLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for range ticker.C {
		if !s.isRunning {
			return
		}
		s.syncPositions()
	}
}

// syncPositions 同步持仓
func (s *SniperLadder) syncPositions() {
	if s.tokenIDYes == "" || s.tokenIDNo == "" {
		return
	}
	
	positions, err := s.executor.GetPositions(s.tokenIDYes, s.tokenIDNo)
	if err != nil {
		return
	}
	
	// 检查是否有显著偏差
	driftYes := positions.Yes - s.filledCountYes
	driftNo := positions.No - s.filledCountNo
	
	if math.Abs(driftYes) > 0.1 || math.Abs(driftNo) > 0.1 {
		fmt.Printf("⚖️ 同步修正: YES %.1f->%.1f | NO %.1f->%.1f\n",
			s.filledCountYes, positions.Yes, s.filledCountNo, positions.No)
		
		s.filledCountYes = positions.Yes
		s.filledCountNo = positions.No
	}
}

// 辅助函数

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

func convertFills(fills []Fill) []struct{ Price, Size float64 } {
	result := make([]struct{ Price, Size float64 }, len(fills))
	for i, f := range fills {
		result[i] = struct{ Price, Size float64 }{Price: f.Price, Size: f.Size}
	}
	return result
}
