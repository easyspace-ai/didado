package executor

import "github.com/polymarketbot-go/pkg/types"

// IExecutor 执行器接口
// 定义了交易执行的标准接口，支持纸交易和真实交易两种模式
type IExecutor interface {
	// GetBalance 获取当前可用余额（USDC）
	GetBalance() (float64, error)

	// PlaceOrder 下限价单
	// 返回订单ID用于跟踪和取消
	PlaceOrder(params types.TradeParams) (string, error)

	// CancelOrder 取消指定订单
	CancelOrder(orderID string) error

	// CancelAll 取消所有开放订单
	CancelAll() error

	// GetOrderStatus 获取订单状态
	GetOrderStatus(orderID string) (*types.OrderStatus, error)

	// GetPositions 获取YES和NO代币的当前持仓
	GetPositions(tokenIDYes, tokenIDNo string) (*types.Positions, error)

	// RedeemPositions 赎回已解决市场的获胜持仓
	RedeemPositions(conditionID string) (string, error)

	// GetAddress 获取用户钱包地址
	GetAddress() (string, error)

	// GetOpenOrders 获取市场的开放订单（可选）
	GetOpenOrders(marketSlug string) ([]OpenOrder, error)
}

// OpenOrder 开放订单信息
type OpenOrder struct {
	ID    string
	Side  string // YES 或 NO
	Price string
	Size  float64
}

// CTFContractAddress Conditional Token Framework合约地址
const CTFContractAddress = "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045"

// USDCAddress USDC代币地址（Polygon）
const USDCAddress = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"
