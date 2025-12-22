package execution

import "errors"

type TradeParams struct {
	TokenID string
	Price   float64
	Side    string // "BUY" or "SELL"
	Size    float64
	Type    string // "GTC", "FOK", "GTD", "FAK"
}

type Executor interface {
	GetBalance() (float64, error)
	PlaceOrder(params TradeParams) (string, error)
	CancelAll() error
	CancelOrder(orderID string) error
	GetOrderStatus(orderID string) (matched float64, cancelled bool, avgFillPrice float64, err error)
	GetPositions(tokenIDYes, tokenIDNo string) (yes float64, no float64, err error)
	GetOpenOrders(marketSlug string) ([]OrderInfo, error)
	GetAddress() string
	RedeemPositions(conditionID string) (string, error)
}

type OrderInfo struct {
	ID    string
	Side  string
	Price string
	Size  float64
}

// Common errors
var (
	ErrNotImplemented = errors.New("not implemented")
	ErrOrderFailed    = errors.New("order failed")
)
