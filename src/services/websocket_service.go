package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketService WebSocket 服务
// 管理到 Polymarket 的 WebSocket 连接以获取实时数据
type WebSocketService struct {
	client          interface{} // ClobClient 接口 (需要实现)
	marketWS        *websocket.Conn
	userWS          *websocket.Conn
	activeTokenIds  []string
	onMarketMessage func(data interface{})
	onUserMessage   func(data interface{})
	userCreds       *UserCredentials
	reconnectAttempts int
	maxReconnectAttempts int
	mu              sync.RWMutex
}

// UserCredentials 用户凭证
type UserCredentials struct {
	Key       string
	Secret    string
	Passphrase string
}

const (
	marketWSURL = "wss://ws-subscriptions-clob.polymarket.com/ws/market"
	userWSURL   = "wss://ws-subscriptions-clob.polymarket.com/ws/user"
)

// NewWebSocketService 创建新的 WebSocket 服务
func NewWebSocketService(client interface{}) *WebSocketService {
	return &WebSocketService{
		client:              client,
		maxReconnectAttempts: 10,
	}
}

// SubscribeMarket 订阅市场频道获取公开价格数据
func (w *WebSocketService) SubscribeMarket(tokenIds []string, onMessage func(data interface{})) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.marketWS != nil {
		w.marketWS.Close()
	}

	w.activeTokenIds = tokenIds
	w.onMarketMessage = onMessage
	return w.connectMarket()
}

// connectMarket 连接到市场 WebSocket 频道
func (w *WebSocketService) connectMarket() error {
	fmt.Println("🔌 [WS-MARKET] Connecting...")

	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(marketWSURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to market WS: %w", err)
	}

	w.marketWS = conn

	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// 连接打开时发送订阅消息
	if len(w.activeTokenIds) > 0 {
		payload := map[string]interface{}{
			"assets_ids": w.activeTokenIds,
			"token_ids":  w.activeTokenIds,
			"type":       "market",
		}

		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}

		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return fmt.Errorf("failed to send subscription: %w", err)
		}
	}

	fmt.Println("✅ [WS-MARKET] Connected.")

	// 启动读取循环
	go w.readMarketMessages(conn)

	// 处理关闭和错误
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("⚠️ [WS-MARKET] Disconnected: %v\n", err)
				time.Sleep(5 * time.Second)
				w.connectMarket()
				return
			}
		}
	}()

	return nil
}

// readMarketMessages 读取市场消息
func (w *WebSocketService) readMarketMessages(conn *websocket.Conn) {
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var data interface{}
		if err := json.Unmarshal(message, &data); err != nil {
			continue
		}

		if w.onMarketMessage != nil {
			w.onMarketMessage(data)
		}
	}
}

// SubscribeUser 订阅用户频道获取私有交易数据
func (w *WebSocketService) SubscribeUser(creds *UserCredentials, onMessage func(data interface{})) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.userWS != nil {
		w.userWS.Close()
	}

	w.userCreds = creds
	w.onUserMessage = onMessage
	return w.connectUser(creds)
}

// connectUser 连接到用户 WebSocket 频道并进行身份验证
func (w *WebSocketService) connectUser(creds *UserCredentials) error {
	fmt.Println("🔐 [WS-USER] Generating Auth Signature...")

	// 生成 HMAC-SHA256 签名
	timestamp := time.Now().Unix()
	message := fmt.Sprintf("%dGET/ws/user", timestamp)

	mac := hmac.New(sha256.New, []byte(creds.Secret))
	mac.Write([]byte(message))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	// 构建认证头
	headers := map[string][]string{
		"POLY-API-KEY":    {creds.Key},
		"POLY-PASSPHRASE": {creds.Passphrase},
		"POLY-TIMESTAMP":  {fmt.Sprintf("%d", timestamp)},
		"POLY-SIGNATURE":  {signature},
	}

	fmt.Println("🔐 [WS-USER] Connecting Authenticated Stream...")

	dialer := websocket.Dialer{}
	conn, resp, err := dialer.Dial(userWSURL, headers)
	if err != nil {
		if resp != nil && resp.StatusCode == 401 {
			w.reconnectAttempts = w.maxReconnectAttempts
			return fmt.Errorf("authentication failed: %w", err)
		}
		return fmt.Errorf("failed to connect to user WS: %w", err)
	}

	w.userWS = conn
	w.reconnectAttempts = 0

	fmt.Println("✅ [WS-USER] Connected & Authenticated.")

	// 启动读取循环
	go w.readUserMessages(conn)

	// 处理关闭和错误
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				if w.reconnectAttempts >= w.maxReconnectAttempts {
					fmt.Printf("❌ [WS-USER] Max reconnection attempts (%d) reached. Stopping reconnection.\n", w.maxReconnectAttempts)
					fmt.Println("   Check your API credentials and network connection.")
					return
				}

				w.reconnectAttempts++
				delay := time.Duration(5000*w.reconnectAttempts) * time.Millisecond
				if delay > 30*time.Second {
					delay = 30 * time.Second
				}

				fmt.Printf("⚠️ [WS-USER] Disconnected. Reconnecting... (Attempt %d/%d)\n",
					w.reconnectAttempts, w.maxReconnectAttempts)

				time.Sleep(delay)
				w.connectUser(creds)
				return
			}
		}
	}()

	return nil
}

// readUserMessages 读取用户消息
func (w *WebSocketService) readUserMessages(conn *websocket.Conn) {
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		eventType, ok := msg["event_type"].(string)
		if !ok {
			continue
		}

		if eventType == "trade" || eventType == "order" {
			if w.onUserMessage != nil {
				w.onUserMessage(msg)
			}
		} else if eventType == "error" {
			if msgMsg, ok := msg["message"].(string); ok {
				fmt.Printf("❌ [WS-USER] Server Error: %s\n", msgMsg)
			}
		}
	}
}

// Close 关闭所有 WebSocket 连接
func (w *WebSocketService) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.marketWS != nil {
		w.marketWS.Close()
		w.marketWS = nil
	}

	if w.userWS != nil {
		w.userWS.Close()
		w.userWS = nil
	}
}
