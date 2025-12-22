package execution

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// Position 持仓跟踪结构
type Position struct {
	TokenID       string
	Side          string
	Quantity      float64
	AvgEntryPrice float64
	TotalInvested float64
}

// PaperExecutor 模拟交易执行器
// 不使用真实资金进行交易模拟
type PaperExecutor struct {
	balance        float64
	initialBalance float64
	positions      map[string]*Position
	tradeHistory   []interface{}
	pendingOrders  map[string]TradeParams
	filledOrders   map[string]OrderStatus
	tokenSideMap   map[string]string
	mu             sync.RWMutex
}

// NewPaperExecutor 创建新的模拟执行器
func NewPaperExecutor(initialBalance float64) *PaperExecutor {
	fmt.Printf("\n📝 PAPER TRADING PORTFOLIO INITIALIZED\n")
	fmt.Printf("   💰 Initial Cash: $%.2f\n", initialBalance)
	fmt.Printf("   ------------------------------------------\n\n")

	return &PaperExecutor{
		balance:        initialBalance,
		initialBalance: initialBalance,
		positions:      make(map[string]*Position),
		tradeHistory:   make([]interface{}, 0),
		pendingOrders:  make(map[string]TradeParams),
		filledOrders:   make(map[string]OrderStatus),
		tokenSideMap:   make(map[string]string),
	}
}

// GetBalance 获取当前余额
func (p *PaperExecutor) GetBalance() (float64, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.balance, nil
}

// PlaceOrder 下订单
func (p *PaperExecutor) PlaceOrder(params TradeParams) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 生成订单 ID
	orderID := fmt.Sprintf("PAPER-%d-%d", time.Now().Unix()%1000000, rand.Intn(100))

	// 存储为待处理订单
	p.pendingOrders[orderID] = params

	orderType := ""
	if params.Type != "" {
		orderType = fmt.Sprintf("[%s]", params.Type)
	}

	fmt.Printf("\n📝 [PAPER] Order Placed (Pending): %s %.2f @ $%.2f %s (ID: %s)\n",
		params.Side, params.Size, params.Price, orderType, orderID)

	return orderID, nil
}

// CancelAll 取消所有订单
func (p *PaperExecutor) CancelAll() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.pendingOrders = make(map[string]TradeParams)
	fmt.Println("   [PAPER] All pending orders cleared.")
	return nil
}

// CancelOrder 取消指定订单
func (p *PaperExecutor) CancelOrder(orderID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.pendingOrders[orderID]; exists {
		delete(p.pendingOrders, orderID)
		fmt.Printf("   [PAPER] Cancelled Pending Order %s\n", orderID)
	} else {
		fmt.Printf("   [PAPER] Cancelled Order %s (Already filled or non-existent)\n", orderID)
	}
	return nil
}

// GetOrderStatus 获取订单状态
func (p *PaperExecutor) GetOrderStatus(orderID string) (OrderStatus, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// 检查是否已成交
	if filled, exists := p.filledOrders[orderID]; exists {
		return filled, nil
	}

	// 检查是否仍在待处理
	if _, exists := p.pendingOrders[orderID]; exists {
		return OrderStatus{Matched: 0, Cancelled: false, AvgFillPrice: 0}, nil
	}

	// 未找到 - 假设已取消
	return OrderStatus{Matched: 0, Cancelled: true, AvgFillPrice: 0}, nil
}

// GetPositions 获取持仓
func (p *PaperExecutor) GetPositions(tokenIDYes, tokenIDNo string) (Positions, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	posYes := p.positions[tokenIDYes]
	posNo := p.positions[tokenIDNo]

	yesQty := 0.0
	noQty := 0.0

	if posYes != nil {
		yesQty = posYes.Quantity
	}
	if posNo != nil {
		noQty = posNo.Quantity
	}

	return Positions{Yes: yesQty, No: noQty}, nil
}

// GetOpenOrders 获取开放订单
func (p *PaperExecutor) GetOpenOrders(marketSlug string) ([]OpenOrder, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	orders := make([]OpenOrder, 0)

	for orderID, params := range p.pendingOrders {
		side := p.tokenSideMap[params.TokenID]
		if side == "" {
			side = "YES" // 默认为 YES
		}

		orders = append(orders, OpenOrder{
			ID:    orderID,
			Side:  side,
			Price: fmt.Sprintf("%.2f", params.Price),
			Size:  params.Size,
		})
	}

	return orders, nil
}

// GetAddress 获取用户地址 (模拟交易返回空地址)
func (p *PaperExecutor) GetAddress() (string, error) {
	return "0x0000000000000000000000000000000000000000", nil
}

// RedeemPositions 赎回持仓 (模拟交易返回虚拟哈希)
func (p *PaperExecutor) RedeemPositions(conditionID string) (string, error) {
	fmt.Printf("📝 [PAPER] Redemption simulated for condition %s...\n", conditionID[:10])
	return fmt.Sprintf("PAPER-REDEEM-%d", time.Now().Unix()), nil
}

// ExecutePaperFill 手动执行模拟成交
func (p *PaperExecutor) ExecutePaperFill(orderID string, price, quantity float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	params, exists := p.pendingOrders[orderID]
	if !exists {
		fmt.Printf("❌ [PAPER] Cannot fill unknown/cancelled order %s\n", orderID)
		return fmt.Errorf("order not found")
	}

	// 模拟延迟
	time.Sleep(50 * time.Millisecond)

	cost := price * quantity

	// 验证资金
	if params.Side == "BUY" && p.balance < cost {
		fmt.Println("❌ [PAPER] REJECTED: Insufficient Funds")
		delete(p.pendingOrders, orderID)
		return fmt.Errorf("insufficient funds")
	}

	// 执行交易
	if params.Side == "BUY" {
		p.balance -= cost
	} else {
		p.balance += cost // 卖出逻辑
	}

	// 更新持仓
	p.updatePosition(TradeParams{
		TokenID: params.TokenID,
		Side:    params.Side,
		Price:   price,
		Size:    quantity,
	})

	// 标记为已成交
	p.filledOrders[orderID] = OrderStatus{
		Matched:      quantity,
		AvgFillPrice: price,
		Cancelled:    false,
	}

	// 从待处理订单中移除
	delete(p.pendingOrders, orderID)

	// 打印交易日志
	p.printTradeLog(orderID, TradeParams{
		TokenID: params.TokenID,
		Side:    params.Side,
		Price:   price,
		Size:    quantity,
	}, cost)

	p.printPortfolioSummary()

	return nil
}

// updatePosition 更新持仓
func (p *PaperExecutor) updatePosition(params TradeParams) {
	existing := p.positions[params.TokenID]

	if existing != nil {
		// 计算加权平均价格
		totalShares := existing.Quantity + params.Size
		totalCost := existing.TotalInvested + params.Price*params.Size

		existing.Quantity = totalShares
		existing.TotalInvested = totalCost
		existing.AvgEntryPrice = totalCost / totalShares
	} else {
		// 新建持仓
		p.positions[params.TokenID] = &Position{
			TokenID:       params.TokenID,
			Side:          params.Side,
			Quantity:      params.Size,
			AvgEntryPrice: params.Price,
			TotalInvested: params.Price * params.Size,
		}
	}
}

// printTradeLog 打印交易日志
func (p *PaperExecutor) printTradeLog(id string, params TradeParams, cost float64) {
	fmt.Printf("\n✅ [PAPER TRADE EXECUTED] #%s\n", id)
	fmt.Printf("   🔹 Side:      %s\n", params.Side)
	fmt.Printf("   🔹 Asset:     ...%s\n", params.TokenID[len(params.TokenID)-10:])
	fmt.Printf("   🔹 Price:     $%.2f\n", params.Price)
	fmt.Printf("   🔹 Size:      %.2f Shares\n", params.Size)
	fmt.Printf("   🔹 Cost:      $%.2f\n", cost)
}

// printPortfolioSummary 打印投资组合摘要
func (p *PaperExecutor) printPortfolioSummary() {
	totalInvested := 0.0
	for _, pos := range p.positions {
		totalInvested += pos.TotalInvested
	}

	totalEquity := p.balance + totalInvested
	profit := totalEquity - p.initialBalance
	profitColor := "🟢"
	if profit < 0 {
		profitColor = "🔴"
	}

	fmt.Printf("\n📊 [PORTFOLIO SUMMARY]\n")
	fmt.Printf("   💵 Cash Balance:    $%.2f\n", p.balance)
	fmt.Printf("   💼 Invested Amount: $%.2f\n", totalInvested)
	fmt.Printf("   📈 Total Equity:    $%.2f\n", totalEquity)
	fmt.Printf("   %s PnL (Unrealized): $%.2f\n", profitColor, profit)

	fmt.Println("   -----------------------------------------------------------")
	fmt.Println("   POSITIONS:")
	if len(p.positions) == 0 {
		fmt.Println("   (Empty)")
	} else {
		for id, pos := range p.positions {
			fmt.Printf("   • ID ...%s | Qty: %.2f | Avg Entry: $%.3f | Cost: $%.2f\n",
				id[len(id)-8:], pos.Quantity, pos.AvgEntryPrice, pos.TotalInvested)
		}
	}
	fmt.Println("   -----------------------------------------------------------\n")
}
