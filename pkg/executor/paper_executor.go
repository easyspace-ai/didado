package executor

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/polymarketbot-go/pkg/types"
)

// Position 持仓跟踪结构
type Position struct {
	TokenID       string
	Side          types.OrderSide
	Quantity      float64
	AvgEntryPrice float64
	TotalInvested float64
}

// PaperExecutor 纸交易执行器
// 模拟交易而不使用真实资金
type PaperExecutor struct {
	balance        float64
	initialBalance float64
	positions      map[string]*Position
	tradeHistory   []interface{}
	pendingOrders  map[string]*types.TradeParams
	filledOrders   map[string]*types.OrderStatus
	mu             sync.RWMutex
}

// NewPaperExecutor 创建新的纸交易执行器
func NewPaperExecutor(initialBalance float64) *PaperExecutor {
	fmt.Printf("\n📝 纸交易账户已初始化\n")
	fmt.Printf("   💰 初始资金: $%.2f\n", initialBalance)
	fmt.Printf("   ------------------------------------------\n\n")

	return &PaperExecutor{
		balance:        initialBalance,
		initialBalance: initialBalance,
		positions:      make(map[string]*Position),
		tradeHistory:   make([]interface{}, 0),
		pendingOrders:  make(map[string]*types.TradeParams),
		filledOrders:   make(map[string]*types.OrderStatus),
	}
}

// GetBalance 获取当前余额
func (p *PaperExecutor) GetBalance() (float64, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.balance, nil
}

// PlaceOrder 下单
func (p *PaperExecutor) PlaceOrder(params types.TradeParams) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 生成订单ID
	orderID := fmt.Sprintf("PAPER-%d-%d", time.Now().Unix()%1000000, rand.Intn(100))

	// 存储为待处理订单
	p.pendingOrders[orderID] = &params

	orderType := ""
	if params.Type != "" {
		orderType = fmt.Sprintf("[%s]", params.Type)
	}

	fmt.Printf("\n📝 [纸交易] 订单已下达（待成交）: %s %.2f @ $%.2f %s (ID: %s)\n",
		params.Side, params.Size, params.Price, orderType, orderID)

	return orderID, nil
}

// CancelAll 取消所有订单
func (p *PaperExecutor) CancelAll() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.pendingOrders = make(map[string]*types.TradeParams)
	fmt.Println("   [纸交易] 所有待处理订单已清除。")
	return nil
}

// CancelOrder 取消订单
func (p *PaperExecutor) CancelOrder(orderID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.pendingOrders[orderID]; exists {
		delete(p.pendingOrders, orderID)
		fmt.Printf("   [纸交易] 已取消待处理订单 %s\n", orderID)
	} else {
		fmt.Printf("   [纸交易] 已取消订单 %s（已成交或不存在）\n", orderID)
	}
	return nil
}

// GetOrderStatus 获取订单状态
func (p *PaperExecutor) GetOrderStatus(orderID string) (*types.OrderStatus, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// 检查是否已成交
	if filled, exists := p.filledOrders[orderID]; exists {
		return filled, nil
	}

	// 检查是否仍在待处理
	if _, exists := p.pendingOrders[orderID]; exists {
		return &types.OrderStatus{
			Matched:      0,
			Cancelled:    false,
			AvgFillPrice: 0,
		}, nil
	}

	// 未找到 - 假设已取消
	return &types.OrderStatus{
		Matched:      0,
		Cancelled:    true,
		AvgFillPrice: 0,
	}, nil
}

// GetPositions 获取持仓
func (p *PaperExecutor) GetPositions(tokenIDYes, tokenIDNo string) (*types.Positions, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	posYes := p.positions[tokenIDYes]
	posNo := p.positions[tokenIDNo]

	yes := 0.0
	no := 0.0

	if posYes != nil {
		yes = posYes.Quantity
	}
	if posNo != nil {
		no = posNo.Quantity
	}

	return &types.Positions{
		Yes: yes,
		No:  no,
	}, nil
}

// GetOpenOrders 获取开放订单
func (p *PaperExecutor) GetOpenOrders(marketSlug string) ([]OpenOrder, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	orders := make([]OpenOrder, 0)
	for orderID, params := range p.pendingOrders {
		orders.append(orders, OpenOrder{
			ID:    orderID,
			Side:  "YES", // 简化处理
			Price: fmt.Sprintf("%.2f", params.Price),
			Size:  params.Size,
		})
	}

	return orders, nil
}

// GetAddress 获取地址
func (p *PaperExecutor) GetAddress() (string, error) {
	return "0x0000000000000000000000000000000000000000", nil
}

// RedeemPositions 赎回持仓（纸交易模拟）
func (p *PaperExecutor) RedeemPositions(conditionID string) (string, error) {
	fmt.Printf("📝 [纸交易] 模拟赎回条件 %s...\n", conditionID[:10])
	return fmt.Sprintf("PAPER-REDEEM-%d", time.Now().Unix()), nil
}

// ExecutePaperFill 手动触发纸交易成交（用于策略测试）
func (p *PaperExecutor) ExecutePaperFill(orderID string, price, quantity float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	params, exists := p.pendingOrders[orderID]
	if !exists {
		return fmt.Errorf("无法成交未知/已取消的订单 %s", orderID)
	}

	// 模拟延迟
	time.Sleep(50 * time.Millisecond)

	cost := price * quantity

	// 验证资金
	if params.Side == types.OrderSideBuy && p.balance < cost {
		delete(p.pendingOrders, orderID)
		return fmt.Errorf("资金不足")
	}

	// 执行交易
	if params.Side == types.OrderSideBuy {
		p.balance -= cost
	} else {
		p.balance += cost
	}

	// 更新持仓
	p.updatePosition(params, price, quantity)

	// 标记为已成交
	p.filledOrders[orderID] = &types.OrderStatus{
		Matched:      quantity,
		AvgFillPrice: price,
		Cancelled:    false,
	}

	// 从待处理中移除
	delete(p.pendingOrders, orderID)

	// 打印日志
	p.printTradeLog(orderID, params, price, quantity, cost)
	p.printPortfolioSummary()

	return nil
}

// updatePosition 更新持仓
func (p *PaperExecutor) updatePosition(params *types.TradeParams, price, size float64) {
	existing := p.positions[params.TokenID]

	if existing != nil {
		// 计算加权平均价格
		totalShares := existing.Quantity + size
		totalCost := existing.TotalInvested + price*size

		existing.Quantity = totalShares
		existing.TotalInvested = totalCost
		existing.AvgEntryPrice = totalCost / totalShares
	} else {
		// 新持仓
		p.positions[params.TokenID] = &Position{
			TokenID:       params.TokenID,
			Side:          params.Side,
			Quantity:      size,
			AvgEntryPrice: price,
			TotalInvested: price * size,
		}
	}
}

// printTradeLog 打印交易日志
func (p *PaperExecutor) printTradeLog(id string, params *types.TradeParams, price, size, cost float64) {
	fmt.Printf("\n✅ [纸交易已执行] #%s\n", id)
	fmt.Printf("   🔹 方向:      %s\n", params.Side)
	fmt.Printf("   🔹 资产:     ...%s\n", params.TokenID[len(params.TokenID)-10:])
	fmt.Printf("   🔹 价格:     $%.2f\n", price)
	fmt.Printf("   🔹 数量:      %.2f 份额\n", size)
	fmt.Printf("   🔹 成本:      $%.2f\n", cost)
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

	fmt.Printf("\n📊 [投资组合摘要]\n")
	fmt.Printf("   💵 现金余额:    $%.2f\n", p.balance)
	fmt.Printf("   💼 投资金额: $%.2f\n", totalInvested)
	fmt.Printf("   📈 总权益:    $%.2f\n", totalEquity)
	fmt.Printf("   %s 盈亏（未实现）: $%.2f\n", profitColor, profit)
	fmt.Printf("   -----------------------------------------------------------\n")
	fmt.Printf("   持仓:\n")

	if len(p.positions) == 0 {
		fmt.Printf("   （空）\n")
	} else {
		for id, pos := range p.positions {
			fmt.Printf("   • ID ...%s | 数量: %.2f | 平均入场: $%.3f | 成本: $%.2f\n",
				id[len(id)-8:], pos.Quantity, pos.AvgEntryPrice, pos.TotalInvested)
		}
	}
	fmt.Printf("   -----------------------------------------------------------\n\n")
}
