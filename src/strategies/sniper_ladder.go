package strategies

import (
	"fmt"
	"sync"
	"time"

	"polymarketbot-go/src/core"
	"polymarketbot-go/src/execution"
	"polymarketbot-go/src/services"
)

// SniperLadder SniperLadder 交易策略
type SniperLadder struct {
	api      *services.PolymarketService
	ws       *services.WebSocketService
	executor execution.IExecutor
	logger   *services.SpreadsheetLogger
	creds    *services.UserCredentials
	signer   interface{}
	proxyAddr string
	math     *core.MarketMath

	isRunning bool
	mu        sync.RWMutex
}

// NewSniperLadder 创建新的 SniperLadder 策略
func NewSniperLadder(
	api *services.PolymarketService,
	ws *services.WebSocketService,
	executor execution.IExecutor,
	creds *services.UserCredentials,
	signer interface{},
	proxyAddr string,
	math *core.MarketMath,
) *SniperLadder {
	return &SniperLadder{
		api:       api,
		ws:        ws,
		executor:  executor,
		logger:    services.NewSpreadsheetLogger("SniperLadder"),
		creds:     creds,
		signer:    signer,
		proxyAddr: proxyAddr,
		math:      math,
	}
}

// Start 启动策略
func (s *SniperLadder) Start() error {
	s.mu.Lock()
	s.isRunning = true
	s.mu.Unlock()

	fmt.Println("🚀 SNIPER LADDER BOT STARTED (v5 - SIT & WAIT)")
	fmt.Println("   🛡️ Logic: Event-Driven. Recalculates ONLY on Fills.")

	// 订阅用户 WebSocket
	if s.creds != nil && s.creds.Key != "" && s.creds.Secret != "" {
		if err := s.ws.SubscribeUser(s.creds, s.handleUserFill); err != nil {
			fmt.Printf("⚠️ Failed to subscribe to user WS: %v\n", err)
		}
	}

	// 启动生命周期
	go s.safeRunLifecycle()

	return nil
}

// Stop 停止策略
func (s *SniperLadder) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.isRunning = false
	s.ws.Close()
	return nil
}

// handleUserFill 处理用户成交事件
func (s *SniperLadder) handleUserFill(data interface{}) {
	// 实现成交处理逻辑
	fmt.Printf("[SNIPER] User fill received: %v\n", data)
}

// safeRunLifecycle 安全运行生命周期
func (s *SniperLadder) safeRunLifecycle() {
	for {
		s.mu.RLock()
		running := s.isRunning
		s.mu.RUnlock()

		if !running {
			return
		}

		// 获取下一个市场
		nextMarket := s.math.GetNextMarketSlug()
		fmt.Printf("[SNIPER] Next market: %s (starts at %s)\n", nextMarket.Slug, nextMarket.StartTime.Format("15:04:05"))

		// 等待市场开始
		waitTime := s.math.GetMsUntil(nextMarket.StartTimeMs)
		if waitTime > 0 {
			fmt.Printf("[SNIPER] Waiting %d seconds for market to start...\n", waitTime/1000)
			time.Sleep(time.Duration(waitTime) * time.Millisecond)
		}

		// 获取 token IDs
		tokenIds, err := s.api.GetMarketTokenIds(nextMarket.Slug)
		if err != nil || len(tokenIds) < 2 {
			fmt.Printf("[SNIPER] ⚠️ Failed to get tokens for market %s, retrying...\n", nextMarket.Slug)
			time.Sleep(5 * time.Second)
			continue
		}

		fmt.Printf("[SNIPER] ✅ Got tokens: YES %s... NO %s...\n", tokenIds[0][:6], tokenIds[1][:6])

		// 执行策略逻辑
		if err := s.executeStrategy(nextMarket.Slug, tokenIds[0], tokenIds[1], nextMarket.EndTimeMs); err != nil {
			fmt.Printf("[SNIPER] ❌ Strategy execution error: %v\n", err)
		}

		// 检查是否只运行一个周期
		// 这里可以根据配置决定是否继续
		break
	}
}

// executeStrategy 执行策略
func (s *SniperLadder) executeStrategy(marketSlug, tokenIDYes, tokenIDNo string, endTimeMs int64) error {
	fmt.Printf("[SNIPER] Executing strategy for market %s\n", marketSlug)

	// 策略配置
	initialTrapPrice := 0.45
	initialTrapSize := 5.0

	// 放置初始陷阱订单
	trapOrderYes, err := s.executor.PlaceOrder(execution.TradeParams{
		TokenID: tokenIDYes,
		Side:    "BUY",
		Price:   initialTrapPrice,
		Size:    initialTrapSize,
		Type:    "GTC",
	})
	if err != nil {
		return fmt.Errorf("failed to place YES trap: %w", err)
	}
	fmt.Printf("[SNIPER] Placed YES trap order: %s\n", trapOrderYes)

	trapOrderNo, err := s.executor.PlaceOrder(execution.TradeParams{
		TokenID: tokenIDNo,
		Side:    "BUY",
		Price:   initialTrapPrice,
		Size:    initialTrapSize,
		Type:    "GTC",
	})
	if err != nil {
		return fmt.Errorf("failed to place NO trap: %w", err)
	}
	fmt.Printf("[SNIPER] Placed NO trap order: %s\n", trapOrderNo)

	// 等待市场结束
	endTime := time.Unix(endTimeMs/1000, 0)
	waitTime := time.Until(endTime)
	if waitTime > 0 {
		fmt.Printf("[SNIPER] Waiting for market to end (%s)...\n", waitTime)
		time.Sleep(waitTime)
	}

	// 取消所有订单
	s.executor.CancelAll()

	// 获取最终持仓
	positions, err := s.executor.GetPositions(tokenIDYes, tokenIDNo)
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}

	fmt.Printf("[SNIPER] Final positions: YES %.2f, NO %.2f\n", positions.Yes, positions.No)

	return nil
}
