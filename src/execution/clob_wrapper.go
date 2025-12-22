package execution

// ClobClientWrapper 包装 CLOB 客户端以实现 execution.ClobClient 接口
// 使用接口避免循环导入
type ClobClientWrapper struct {
	client ClobClientWrapperInterface
}

// ClobClientWrapperInterface 包装器接口
type ClobClientWrapperInterface interface {
	CreateOrderForExecution(OrderRequest) (*Order, error)
	PostOrderForExecution(*Order, string) (*OrderResponse, error)
	CancelOrder(string) error
	CancelAll() error
	GetOrderForExecution(string) (*OrderInfo, error)
}

// NewClobClientWrapper 创建包装器
func NewClobClientWrapper(client ClobClientWrapperInterface) *ClobClientWrapper {
	return &ClobClientWrapper{client: client}
}

// CreateOrder 创建订单
func (w *ClobClientWrapper) CreateOrder(order OrderRequest) (*Order, error) {
	return w.client.CreateOrderForExecution(order)
}

// PostOrder 提交订单
func (w *ClobClientWrapper) PostOrder(order *Order, orderType string) (*OrderResponse, error) {
	return w.client.PostOrderForExecution(order, orderType)
}

// CancelOrder 取消订单
func (w *ClobClientWrapper) CancelOrder(orderID string) error {
	return w.client.CancelOrder(orderID)
}

// CancelAll 取消所有订单
func (w *ClobClientWrapper) CancelAll() error {
	return w.client.CancelAll()
}

// GetOrder 获取订单
func (w *ClobClientWrapper) GetOrder(orderID string) (*OrderInfo, error) {
	return w.client.GetOrderForExecution(orderID)
}
