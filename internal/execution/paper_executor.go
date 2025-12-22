package execution

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

type PaperExecutor struct {
	Balance     float64
	Positions   map[string]float64 // TokenID -> Size
	Orders      map[string]*PaperOrder
	OpenOrders  map[string]*PaperOrder
	mu          sync.Mutex
}

type PaperOrder struct {
	ID           string
	TokenID      string
	Price        float64
	Side         string
	Size         float64
	Type         string
	MatchedSize  float64
	Status       string // "OPEN", "FILLED", "CANCELLED"
	AvgFillPrice float64
}

func NewPaperExecutor(initialBalance float64) *PaperExecutor {
	return &PaperExecutor{
		Balance:    initialBalance,
		Positions:  make(map[string]float64),
		Orders:     make(map[string]*PaperOrder),
		OpenOrders: make(map[string]*PaperOrder),
	}
}

func (e *PaperExecutor) GetBalance() (float64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.Balance, nil
}

func (e *PaperExecutor) PlaceOrder(params TradeParams) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	orderID := uuid.New().String()
	order := &PaperOrder{
		ID:      orderID,
		TokenID: params.TokenID,
		Price:   params.Price,
		Side:    params.Side,
		Size:    params.Size,
		Type:    params.Type,
		Status:  "OPEN",
	}

	e.Orders[orderID] = order
	e.OpenOrders[orderID] = order

	log.Printf("📝 [PAPER] Placing Order: %s %.2f @ $%.2f", params.Side, params.Size, params.Price)

	// Simulate immediate fill for simplicity, or keep open if we want strict logic
	// But TS PaperExecutor usually simulates fills based on probability or immediately.
	// Let's assume immediate fill for now as "Simulated trading".
	// In a real backtest/paper trade we might wait for market price match.
	// For "replication", let's look at original PaperExecutor behavior.
	
	go e.simulateFill(orderID)

	return orderID, nil
}

func (e *PaperExecutor) simulateFill(orderID string) {
	time.Sleep(100 * time.Millisecond) // Simulate network delay
	
	e.mu.Lock()
	defer e.mu.Unlock()

	order, ok := e.Orders[orderID]
	if !ok || order.Status != "OPEN" {
		return
	}

	// Update order
	order.Status = "FILLED"
	order.MatchedSize = order.Size
	order.AvgFillPrice = order.Price
	
	delete(e.OpenOrders, orderID)

	// Update positions and balance
	cost := order.Size * order.Price
	if order.Side == "BUY" {
		e.Balance -= cost
		e.Positions[order.TokenID] += order.Size
	} else {
		e.Balance += cost
		e.Positions[order.TokenID] -= order.Size
	}

	log.Printf("✅ [PAPER] Order Filled! ID: %s", orderID)
}

func (e *PaperExecutor) CancelAll() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for id, order := range e.OpenOrders {
		order.Status = "CANCELLED"
		delete(e.OpenOrders, id)
	}
	log.Println("✅ [PAPER] All orders cancelled.")
	return nil
}

func (e *PaperExecutor) CancelOrder(orderID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if order, ok := e.OpenOrders[orderID]; ok {
		order.Status = "CANCELLED"
		delete(e.OpenOrders, orderID)
		log.Printf("✅ [PAPER] Order %s cancelled.", orderID)
		return nil
	}
	return nil
}

func (e *PaperExecutor) GetOrderStatus(orderID string) (float64, bool, float64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if order, ok := e.Orders[orderID]; ok {
		cancelled := order.Status == "CANCELLED"
		return order.MatchedSize, cancelled, order.AvgFillPrice, nil
	}
	return 0, false, 0, fmt.Errorf("order not found")
}

func (e *PaperExecutor) GetPositions(tokenIDYes, tokenIDNo string) (float64, float64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.Positions[tokenIDYes], e.Positions[tokenIDNo], nil
}

func (e *PaperExecutor) GetOpenOrders(marketSlug string) ([]OrderInfo, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var res []OrderInfo
	for _, o := range e.OpenOrders {
		res = append(res, OrderInfo{
			ID:    o.ID,
			Side:  o.Side,
			Price: fmt.Sprintf("%.2f", o.Price),
			Size:  o.Size,
		})
	}
	return res, nil
}

func (e *PaperExecutor) GetAddress() string {
	return "0x0000000000000000000000000000000000000000" // Mock address
}

func (e *PaperExecutor) RedeemPositions(conditionID string) (string, error) {
	log.Printf("🔗 [PAPER] Redeeming positions for %s", conditionID)
	return "0xmocktxhash", nil
}
