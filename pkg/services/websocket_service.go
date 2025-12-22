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
	"github.com/polymarketbot-go/pkg/types"
)

// WebSocketService WebSocket连接服务
// 管理与Polymarket的WebSocket连接以获取实时数据
type WebSocketService struct {
	marketWs          *websocket.Conn
	userWs            *websocket.Conn
	activeTokenIds    []string
	onMarketMessage   func(types.WebSocketMessage)
	onUserMessage     func(types.WebSocketMessage)
	userCreds         *types.APICredentials
	reconnectAttempts int
	maxReconnects     int
	mu                sync.RWMutex
	stopChan          chan struct{}
}

// NewWebSocketService 创建新的WebSocket服务
func NewWebSocketService() *WebSocketService {
	return &WebSocketService{
		maxReconnects: 10,
		stopChan:      make(chan struct{}),
	}
}

// SubscribeMarket 订阅市场频道（公开价格数据）
func (ws *WebSocketService) SubscribeMarket(tokenIds []string, onMessage func(types.WebSocketMessage)) error {
	ws.mu.Lock()
	ws.activeTokenIds = tokenIds
	ws.onMarketMessage = onMessage
	ws.mu.Unlock()

	return ws.connectMarket()
}

// connectMarket 连接到市场WebSocket频道
func (ws *WebSocketService) connectMarket() error {
	fmt.Println("🔌 [WS-MARKET] 连接中...")

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial("wss://ws-subscriptions-clob.polymarket.com/ws/market", nil)
	if err != nil {
		return fmt.Errorf("连接市场WebSocket失败: %w", err)
	}

	ws.mu.Lock()
	ws.marketWs = conn
	ws.mu.Unlock()

	fmt.Println("✅ [WS-MARKET] 已连接。")

	// 发送订阅消息
	if len(ws.activeTokenIds) > 0 {
		payload := map[string]interface{}{
			"assets_ids": ws.activeTokenIds,
			"token_ids":  ws.activeTokenIds,
			"type":       "market",
		}

		if err := conn.WriteJSON(payload); err != nil {
			return fmt.Errorf("发送订阅消息失败: %w", err)
		}
	}

	// 启动消息监听
	go ws.listenMarket()

	return nil
}

// listenMarket 监听市场消息
func (ws *WebSocketService) listenMarket() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("❌ [WS-MARKET] Panic恢复: %v\n", r)
		}
	}()

	for {
		select {
		case <-ws.stopChan:
			return
		default:
			ws.mu.RLock()
			conn := ws.marketWs
			ws.mu.RUnlock()

			if conn == nil {
				return
			}

			_, message, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("❌ [WS-MARKET] 读取消息错误: %v\n", err)
				// 自动重连
				time.Sleep(5 * time.Second)
				ws.connectMarket()
				return
			}

			var msg types.WebSocketMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				continue // 忽略格式错误的消息
			}

			if ws.onMarketMessage != nil {
				ws.onMarketMessage(msg)
			}
		}
	}
}

// SubscribeUser 订阅用户频道（私有交易数据，需要认证）
func (ws *WebSocketService) SubscribeUser(creds types.APICredentials, onMessage func(types.WebSocketMessage)) error {
	ws.mu.Lock()
	ws.userCreds = &creds
	ws.onUserMessage = onMessage
	ws.mu.Unlock()

	return ws.connectUser(creds)
}

// connectUser 连接到用户WebSocket频道（需要认证）
func (ws *WebSocketService) connectUser(creds types.APICredentials) error {
	fmt.Println("🔐 [WS-USER] 生成认证签名...")

	// 生成HMAC-SHA256签名
	timestamp := time.Now().Unix()
	message := fmt.Sprintf("%dGET/ws/user", timestamp)
	
	h := hmac.New(sha256.New, []byte(creds.Secret))
	h.Write([]byte(message))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// 设置认证头
	headers := map[string][]string{
		"POLY-API-KEY":    {creds.Key},
		"POLY-PASSPHRASE": {creds.Passphrase},
		"POLY-TIMESTAMP":  {fmt.Sprintf("%d", timestamp)},
		"POLY-SIGNATURE":  {signature},
	}

	fmt.Println("🔐 [WS-USER] 连接认证流...")

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial("wss://ws-subscriptions-clob.polymarket.com/ws/user", headers)
	if err != nil {
		return fmt.Errorf("连接用户WebSocket失败: %w", err)
	}

	ws.mu.Lock()
	ws.userWs = conn
	ws.reconnectAttempts = 0
	ws.mu.Unlock()

	fmt.Println("✅ [WS-USER] 已连接并认证。")

	// 启动消息监听
	go ws.listenUser()

	return nil
}

// listenUser 监听用户消息
func (ws *WebSocketService) listenUser() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("❌ [WS-USER] Panic恢复: %v\n", r)
		}
	}()

	for {
		select {
		case <-ws.stopChan:
			return
		default:
			ws.mu.RLock()
			conn := ws.userWs
			ws.mu.RUnlock()

			if conn == nil {
				return
			}

			_, message, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("❌ [WS-USER] 连接断开: %v\n", err)
				
				ws.mu.Lock()
				attempts := ws.reconnectAttempts
				ws.reconnectAttempts++
				ws.mu.Unlock()

				if attempts >= ws.maxReconnects {
					fmt.Printf("❌ [WS-USER] 达到最大重连次数(%d)。停止重连。\n", ws.maxReconnects)
					return
				}

				// 指数退避重连
				delay := time.Duration(5*(attempts+1)) * time.Second
				if delay > 30*time.Second {
					delay = 30 * time.Second
				}

				fmt.Printf("⚠️ [WS-USER] 重连中... (尝试 %d/%d)\n", attempts+1, ws.maxReconnects)
				time.Sleep(delay)

				if ws.userCreds != nil {
					ws.connectUser(*ws.userCreds)
				}
				return
			}

			var msg types.WebSocketMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			// 过滤相关事件
			if msg.EventType == "trade" || msg.EventType == "order" {
				if ws.onUserMessage != nil {
					ws.onUserMessage(msg)
				}
			}

			if msg.EventType == "error" {
				fmt.Printf("❌ [WS-USER] 服务器错误: %v\n", msg.Data)
			}
		}
	}
}

// Close 关闭所有WebSocket连接
func (ws *WebSocketService) Close() {
	close(ws.stopChan)

	ws.mu.Lock()
	defer ws.mu.Unlock()

	if ws.marketWs != nil {
		ws.marketWs.Close()
		ws.marketWs = nil
	}

	if ws.userWs != nil {
		ws.userWs.Close()
		ws.userWs = nil
	}
}
