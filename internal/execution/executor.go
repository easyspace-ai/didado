package execution

import "context"

type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

type OrderType string

const (
	OrderTypeGTC OrderType = "GTC"
	OrderTypeFOK OrderType = "FOK"
	OrderTypeGTD OrderType = "GTD"
	OrderTypeFAK OrderType = "FAK"
)

type TradeParams struct {
	TokenID string
	Side    Side
	Price   float64
	Size    float64
	Type    OrderType // default GTC if empty
}

type OrderStatus struct {
	Matched      float64
	Cancelled    bool
	AvgFillPrice float64
}

type OpenOrder struct {
	ID    string
	Side  string // "YES" | "NO"
	Price string
	Size  float64
}

type Executor interface {
	GetBalance(ctx context.Context) (float64, error)
	PlaceOrder(ctx context.Context, params TradeParams) (string, error)
	CancelOrder(ctx context.Context, orderID string) error
	CancelAll(ctx context.Context) error
	GetOrderStatus(ctx context.Context, orderID string) (OrderStatus, error)
	GetPositions(ctx context.Context, tokenIDYes, tokenIDNo string) (yes float64, no float64, err error)
	RedeemPositions(ctx context.Context, conditionID string) (txHash string, err error)
	GetAddress(ctx context.Context) (string, error)
	GetOpenOrders(ctx context.Context, marketSlug string) ([]OpenOrder, error)
}

type PaperFillExecutor interface {
	Executor
	ExecutePaperFill(ctx context.Context, orderID string, price float64, quantity float64) error
}

const (
	CTFContractAddress = "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045"
	USDCAddress        = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"
)
