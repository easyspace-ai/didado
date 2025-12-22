package clob

import (
	"polymarketbot-go/src/execution"
)

// CreateOrderForExecution 创建订单 (适配 execution 包)
func (c *ClobClient) CreateOrderForExecution(order execution.OrderRequest) (*execution.Order, error) {
	clobOrder, err := c.CreateOrder(OrderRequest{
		TokenID:    order.TokenID,
		Price:      order.Price,
		Side:       Side(order.Side),
		Size:       order.Size,
		FeeRateBps: order.FeeRateBps,
	})
	if err != nil {
		return nil, err
	}

	return &execution.Order{
		TokenID: clobOrder.TokenID,
		Price:   clobOrder.Price,
		Side:    string(clobOrder.Side),
		Size:    clobOrder.Size,
	}, nil
}

// PostOrderForExecution 提交订单 (适配 execution 包)
func (c *ClobClient) PostOrderForExecution(order *execution.Order, orderType string) (*execution.OrderResponse, error) {
	clobOrder := &Order{
		TokenID: order.TokenID,
		Price:   order.Price,
		Side:    Side(order.Side),
		Size:    order.Size,
	}

	clobResp, err := c.PostOrder(clobOrder, OrderType(orderType))
	if err != nil {
		return nil, err
	}

	return &execution.OrderResponse{
		OrderID:  clobResp.OrderID,
		OrderId:  clobResp.OrderId,
		ID:       clobResp.ID,
		Success:  clobResp.Success,
		Error:    clobResp.Error,
		ErrorMsg: clobResp.ErrorMsg,
	}, nil
}

// GetOrderForExecution 获取订单 (适配 execution 包)
func (c *ClobClient) GetOrderForExecution(orderID string) (*execution.OrderInfo, error) {
	clobInfo, err := c.GetOrder(orderID)
	if err != nil {
		return nil, err
	}

	trades := make([]execution.Trade, len(clobInfo.AssociateTrades))
	for i, t := range clobInfo.AssociateTrades {
		trades[i] = execution.Trade{
			Size:  t.Size,
			Price: t.Price,
		}
	}

	return &execution.OrderInfo{
		OrderID:         clobInfo.OrderID,
		TokenID:         clobInfo.TokenID,
		Price:           clobInfo.Price,
		Size:            clobInfo.Size,
		SizeMatched:     clobInfo.SizeMatched,
		SizeMatchedAlt:  clobInfo.SizeMatchedAlt,
		Status:          clobInfo.Status,
		Canceled:        clobInfo.Canceled,
		Side:            clobInfo.Side,
		AssociateTrades: trades,
	}, nil
}
