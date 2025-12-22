package types

import "time"

// TradeParams 订单参数
type TradeParams struct {
	TokenID string      // Token ID (YES或NO token地址)
	Side    OrderSide   // 订单方向: BUY 或 SELL
	Price   float64     // 限价价格 (例如 0.46 表示 $0.46)
	Size    float64     // 订单数量（份额）
	Type    OrderType   // 订单类型
}

// OrderSide 订单方向
type OrderSide string

const (
	OrderSideBuy  OrderSide = "BUY"
	OrderSideSell OrderSide = "SELL"
)

// OrderType 订单类型
type OrderType string

const (
	OrderTypeGTC OrderType = "GTC" // Good Till Cancel
	OrderTypeFOK OrderType = "FOK" // Fill-Or-Kill
	OrderTypeGTD OrderType = "GTD" // Good Till Date
	OrderTypeFAK OrderType = "FAK" // Fill And Kill
)

// OrderStatus 订单状态
type OrderStatus struct {
	Matched      float64 // 已成交数量
	Cancelled    bool    // 是否已取消
	AvgFillPrice float64 // 平均成交价格
}

// Positions 持仓信息
type Positions struct {
	Yes float64 // YES份额数量
	No  float64 // NO份额数量
}

// MarketInfo 市场信息
type MarketInfo struct {
	Slug        string    // 市场标识符
	StartTimeMs int64     // 开始时间（毫秒）
	EndTimeMs   int64     // 结束时间（毫秒）
}

// TradeRecord 交易记录
type TradeRecord struct {
	Time             time.Time
	MarketSlug       string
	CapitalInvested  float64
	ProfitPercent    float64
	CapitalEnd       float64
	YesPositions     string
	YesAvg           float64
	YesTotal         float64
	NoPositions      string
	NoAvg            float64
	NoTotal          float64
	PnL              float64
	Outcome          string // WIN, LOSS, BREAKEVEN
}

// APICredentials API凭证
type APICredentials struct {
	Key        string
	Secret     string
	Passphrase string
}

// WebSocketMessage WebSocket消息
type WebSocketMessage struct {
	EventType string      `json:"event_type"`
	AssetID   string      `json:"asset_id"`
	Price     string      `json:"price"`
	Side      string      `json:"side"`
	Size      string      `json:"size"`
	MatchID   string      `json:"match_id"`
	OrderID   string      `json:"order_id"`
	Timestamp int64       `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// TokenPosition 代币持仓（用于赎回）
type TokenPosition struct {
	ConditionID string
	Amount      float64
	TokenID     string
}
