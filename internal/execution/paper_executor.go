package execution

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

type paperPosition struct {
	TokenID       string
	Side          Side
	Quantity      float64
	AvgEntryPrice float64
	TotalInvested float64
}

type PaperExecutor struct {
	mu sync.Mutex

	balance        float64
	initialBalance float64

	positions     map[string]*paperPosition
	pendingOrders map[string]TradeParams
	filledOrders  map[string]struct {
		matched      float64
		avgFillPrice float64
	}

	// tokenID -> "YES"|"NO" (placeholder, mirrors TS)
	tokenSideMap map[string]string
}

func NewPaperExecutor(initialBalance float64) *PaperExecutor {
	if initialBalance <= 0 {
		initialBalance = 1000
	}
	fmt.Println("\n📝 PAPER TRADING PORTFOLIO INITIALIZED")
	fmt.Printf("   💰 Initial Cash: $%.2f\n", initialBalance)
	fmt.Println("   ------------------------------------------")

	return &PaperExecutor{
		balance:        initialBalance,
		initialBalance: initialBalance,
		positions:      map[string]*paperPosition{},
		pendingOrders:  map[string]TradeParams{},
		filledOrders:   map[string]struct{ matched, avgFillPrice float64 }{},
		tokenSideMap:   map[string]string{},
	}
}

func (p *PaperExecutor) GetBalance(ctx context.Context) (float64, error) {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.balance, nil
}

func (p *PaperExecutor) PlaceOrder(ctx context.Context, params TradeParams) (string, error) {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()

	orderID := fmt.Sprintf("PAPER-%06d-%02d", time.Now().UnixMilli()%1_000_000, rand.Intn(100))
	p.pendingOrders[orderID] = params

	orderType := ""
	if params.Type != "" {
		orderType = "[" + string(params.Type) + "]"
	}
	fmt.Printf("\n📝 [PAPER] Order Placed (Pending): %s %.4g @ $%.2f %s (ID: %s)\n",
		params.Side, params.Size, params.Price, orderType, orderID,
	)
	return orderID, nil
}

func (p *PaperExecutor) CancelAll(ctx context.Context) error {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pendingOrders = map[string]TradeParams{}
	fmt.Println("   [PAPER] All pending orders cleared.")
	return nil
}

func (p *PaperExecutor) CancelOrder(ctx context.Context, orderID string) error {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.pendingOrders[orderID]; ok {
		delete(p.pendingOrders, orderID)
		fmt.Printf("   [PAPER] Cancelled Pending Order %s\n", orderID)
		return nil
	}
	fmt.Printf("   [PAPER] Cancelled Order %s (Already filled or non-existent)\n", orderID)
	return nil
}

func (p *PaperExecutor) GetOrderStatus(ctx context.Context, orderID string) (OrderStatus, error) {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()

	if v, ok := p.filledOrders[orderID]; ok {
		return OrderStatus{Matched: v.matched, Cancelled: false, AvgFillPrice: v.avgFillPrice}, nil
	}
	if _, ok := p.pendingOrders[orderID]; ok {
		return OrderStatus{Matched: 0, Cancelled: false, AvgFillPrice: 0}, nil
	}
	return OrderStatus{Matched: 0, Cancelled: true, AvgFillPrice: 0}, nil
}

func (p *PaperExecutor) GetPositions(ctx context.Context, tokenIDYes, tokenIDNo string) (yes float64, no float64, err error) {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()

	if pos := p.positions[tokenIDYes]; pos != nil {
		yes = pos.Quantity
	}
	if pos := p.positions[tokenIDNo]; pos != nil {
		no = pos.Quantity
	}
	return yes, no, nil
}

func (p *PaperExecutor) GetOpenOrders(ctx context.Context, _ string) ([]OpenOrder, error) {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]OpenOrder, 0, len(p.pendingOrders))
	for id, params := range p.pendingOrders {
		side := p.tokenSideMap[params.TokenID]
		if side == "" {
			side = "YES"
		}
		out = append(out, OpenOrder{
			ID:    id,
			Side:  side,
			Price: fmt.Sprintf("%.2f", params.Price),
			Size:  params.Size,
		})
	}
	return out, nil
}

func (p *PaperExecutor) ExecutePaperFill(ctx context.Context, orderID string, price float64, quantity float64) error {
	_ = ctx

	p.mu.Lock()
	params, ok := p.pendingOrders[orderID]
	p.mu.Unlock()
	if !ok {
		fmt.Printf("❌ [PAPER] Cannot fill unknown/cancelled order %s\n", orderID)
		return nil
	}

	time.Sleep(50 * time.Millisecond)

	cost := price * quantity

	p.mu.Lock()
	defer p.mu.Unlock()

	if params.Side == SideBuy && p.balance < cost {
		fmt.Println("❌ [PAPER] REJECTED: Insufficient Funds")
		delete(p.pendingOrders, orderID)
		return nil
	}

	if params.Side == SideBuy {
		p.balance -= cost
	} else {
		p.balance += cost
	}

	// update position with fill price/qty
	p.updatePositionLocked(TradeParams{
		TokenID: params.TokenID,
		Side:    params.Side,
		Price:   price,
		Size:    quantity,
		Type:    params.Type,
	})

	p.filledOrders[orderID] = struct {
		matched      float64
		avgFillPrice float64
	}{matched: quantity, avgFillPrice: price}
	delete(p.pendingOrders, orderID)

	p.printTradeLogLocked(orderID, params, price, quantity, cost)
	p.printPortfolioSummaryLocked()

	return nil
}

func (p *PaperExecutor) updatePositionLocked(params TradeParams) {
	existing := p.positions[params.TokenID]
	if existing != nil {
		totalShares := existing.Quantity + params.Size
		totalCost := existing.TotalInvested + params.Price*params.Size
		existing.Quantity = totalShares
		existing.TotalInvested = totalCost
		if totalShares > 0 {
			existing.AvgEntryPrice = totalCost / totalShares
		}
		return
	}

	p.positions[params.TokenID] = &paperPosition{
		TokenID:       params.TokenID,
		Side:          params.Side,
		Quantity:      params.Size,
		AvgEntryPrice: params.Price,
		TotalInvested: params.Price * params.Size,
	}
}

func (p *PaperExecutor) printTradeLogLocked(id string, params TradeParams, price, qty, cost float64) {
	fmt.Printf("\n✅ [PAPER TRADE EXECUTED] #%s\n", id)
	fmt.Printf("   🔹 Side:      %s\n", params.Side)
	suffix := params.TokenID
	if len(suffix) > 10 {
		suffix = suffix[len(suffix)-10:]
	}
	fmt.Printf("   🔹 Asset:     ...%s\n", suffix)
	fmt.Printf("   🔹 Price:     $%.2f\n", price)
	fmt.Printf("   🔹 Size:      %.4g Shares\n", qty)
	fmt.Printf("   🔹 Cost:      $%.2f\n", cost)
}

func (p *PaperExecutor) printPortfolioSummaryLocked() {
	totalInvested := 0.0
	for _, v := range p.positions {
		totalInvested += v.TotalInvested
	}
	totalEquity := p.balance + totalInvested
	profit := totalEquity - p.initialBalance
	profitIcon := "🟢"
	if profit < 0 {
		profitIcon = "🔴"
	}

	fmt.Printf("\n📊 [PORTFOLIO SUMMARY]\n")
	fmt.Printf("   💵 Cash Balance:    $%.2f\n", p.balance)
	fmt.Printf("   💼 Invested Amount: $%.2f\n", totalInvested)
	fmt.Printf("   📈 Total Equity:    $%.2f\n", totalEquity)
	fmt.Printf("   %s PnL (Unrealized): $%.2f\n", profitIcon, profit)

	fmt.Println("   -----------------------------------------------------------")
	fmt.Println("   POSITIONS:")
	if len(p.positions) == 0 {
		fmt.Println("   (Empty)")
	} else {
		for id, pos := range p.positions {
			suffix := id
			if len(suffix) > 8 {
				suffix = suffix[len(suffix)-8:]
			}
			fmt.Printf("   • ID ...%s | Qty: %.4g | Avg Entry: $%.3f | Cost: $%.2f\n",
				suffix, pos.Quantity, pos.AvgEntryPrice, pos.TotalInvested,
			)
		}
	}
	fmt.Println("   -----------------------------------------------------------")
}

func (p *PaperExecutor) GetAddress(ctx context.Context) (string, error) {
	_ = ctx
	return "0x0000000000000000000000000000000000000000", nil
}

func (p *PaperExecutor) RedeemPositions(ctx context.Context, conditionID string) (string, error) {
	_ = ctx
	fmt.Printf("📝 [PAPER] Redemption simulated for condition %s...\n", trim(conditionID, 10))
	return fmt.Sprintf("PAPER-REDEEM-%d", time.Now().UnixMilli()), nil
}

func trim(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n])
}

func init() {
	rand.Seed(time.Now().UnixNano())
	_ = math.MaxFloat64
}
