package execution

// TradeParams 订单参数
type TradeParams struct {
	TokenID string  // Token ID (YES 或 NO token 地址)
	Side    string  // 订单方向: "BUY" 或 "SELL"
	Price   float64 // 限价 (例如: 0.46 表示 $0.46)
	Size    float64 // 订单大小 (股数)
	Type    string  // 订单类型: "GTC", "FOK", "GTD", "FAK" (可选，默认为 GTC)
}

// OrderStatus 订单状态
type OrderStatus struct {
	Matched      float64 // 已成交数量
	Cancelled    bool    // 是否已取消
	AvgFillPrice float64 // 平均成交价格
}

// Positions 持仓信息
type Positions struct {
	Yes float64 // YES 持仓数量
	No  float64 // NO 持仓数量
}

// OpenOrder 开放订单
type OpenOrder struct {
	ID    string  // 订单 ID
	Side  string  // "YES" 或 "NO"
	Price string  // 价格字符串
	Size  float64 // 订单大小
}

// IExecutor 执行器接口
// 定义了在模拟和实盘交易模式下的订单执行接口
type IExecutor interface {
	// GetBalance 获取当前可用余额
	GetBalance() (float64, error)

	// PlaceOrder 下订单
	PlaceOrder(params TradeParams) (string, error)

	// CancelOrder 取消指定订单
	CancelOrder(orderID string) error

	// CancelAll 取消所有订单
	CancelAll() error

	// GetOrderStatus 获取订单状态
	GetOrderStatus(orderID string) (OrderStatus, error)

	// GetPositions 获取当前持仓
	GetPositions(tokenIDYes, tokenIDNo string) (Positions, error)

	// GetAddress 获取用户地址
	GetAddress() (string, error)

	// RedeemPositions 赎回已结算市场的获胜持仓
	RedeemPositions(conditionID string) (string, error)

	// ExecutePaperFill 手动执行模拟成交 (仅用于 PaperExecutor)
	ExecutePaperFill(orderID string, price, quantity float64) error

	// GetOpenOrders 获取开放订单 (可选)
	GetOpenOrders(marketSlug string) ([]OpenOrder, error)
}

// 合约地址常量
const (
	// CTF_CONTRACT_ADDRESS Conditional Token Framework 合约地址
	CTF_CONTRACT_ADDRESS = "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045"

	// USDC_ADDRESS Polygon 上的 USDC 代币地址
	USDC_ADDRESS = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"
)
