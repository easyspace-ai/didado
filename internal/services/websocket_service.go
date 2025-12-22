package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type UserCreds struct {
	Key        string
	Secret     string
	Passphrase string
}

type WebSocketService struct {
	marketURL string
	userURL   string

	mu sync.Mutex

	marketConn *websocket.Conn
	userConn   *websocket.Conn

	activeTokenIDs []string
	onMarketMsg    func(any)
	onUserMsg      func(any)

	userCreds *UserCreds

	reconnectAttempts int
	maxReconnect      int

	closeCh chan struct{}
}

func NewWebSocketService(marketURL, userURL string) *WebSocketService {
	if marketURL == "" {
		marketURL = "wss://ws-subscriptions-clob.polymarket.com/ws/market"
	}
	if userURL == "" {
		userURL = "wss://ws-subscriptions-clob.polymarket.com/ws/user"
	}
	return &WebSocketService{
		marketURL:         marketURL,
		userURL:           userURL,
		maxReconnect:      10,
		reconnectAttempts: 0,
		closeCh:           make(chan struct{}),
	}
}

func (w *WebSocketService) SubscribeMarket(tokenIDs []string, onMessage func(any)) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// close existing connection
	if w.marketConn != nil {
		_ = w.marketConn.Close()
		w.marketConn = nil
	}

	w.activeTokenIDs = append([]string(nil), tokenIDs...)
	w.onMarketMsg = onMessage

	go w.connectMarket()
}

func (w *WebSocketService) connectMarket() {
	DashboardManager.Log("🔌 [WS-MARKET] Connecting...", LogInfo)

	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(w.marketURL, nil)
	if err != nil {
		DashboardManager.Log("[WS-MARKET] Error: "+err.Error(), LogError)
		select {
		case <-time.After(5 * time.Second):
			w.connectMarket()
		case <-w.closeCh:
		}
		return
	}

	w.mu.Lock()
	w.marketConn = conn
	tokenIDs := append([]string(nil), w.activeTokenIDs...)
	onMsg := w.onMarketMsg
	w.mu.Unlock()

	DashboardManager.Log("✅ [WS-MARKET] Connected.", LogInfo)

	if len(tokenIDs) > 0 {
		payload := map[string]any{
			"assets_ids": tokenIDs,
			"token_ids":  tokenIDs,
			"type":       "market",
		}
		_ = conn.WriteJSON(payload)
	}

	go w.readLoop(conn, func(raw []byte) {
		if onMsg == nil {
			return
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return
		}
		onMsg(v)
	}, func(err error) {
		if err != nil && !errors.Is(err, websocket.ErrCloseSent) {
			DashboardManager.Log("[WS-MARKET] Closed: "+err.Error(), LogWarn)
		}
		select {
		case <-time.After(5 * time.Second):
			w.connectMarket()
		case <-w.closeCh:
		}
	})
}

func (w *WebSocketService) SubscribeUser(creds UserCreds, onMessage func(any)) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.userConn != nil {
		_ = w.userConn.Close()
		w.userConn = nil
	}

	w.userCreds = &creds
	w.onUserMsg = onMessage
	w.reconnectAttempts = 0

	go w.connectUser(creds)
}

func (w *WebSocketService) connectUser(creds UserCreds) {
	DashboardManager.Log("🔐 [WS-USER] Generating Auth Signature...", LogInfo)

	timestamp := time.Now().Unix()
	message := strconv.FormatInt(timestamp, 10) + "GET/ws/user"
	// Polymarket secret is typically base64-encoded; decode if possible.
	keyBytes, err := base64.StdEncoding.DecodeString(creds.Secret)
	if err != nil || len(keyBytes) == 0 {
		keyBytes = []byte(creds.Secret)
	}
	sig := hmacSHA256Base64(keyBytes, []byte(message))

	header := http.Header{}
	header.Set("POLY-API-KEY", creds.Key)
	header.Set("POLY-PASSPHRASE", creds.Passphrase)
	header.Set("POLY-TIMESTAMP", strconv.FormatInt(timestamp, 10))
	header.Set("POLY-SIGNATURE", sig)

	DashboardManager.Log("🔐 [WS-USER] Connecting Authenticated Stream...", LogInfo)
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(w.userURL, header)
	if err != nil {
		DashboardManager.Log("[WS-USER] Error: "+err.Error(), LogError)

		// Stop retrying on obvious auth failures
		if isAuthErr(err) {
			w.mu.Lock()
			w.reconnectAttempts = w.maxReconnect
			w.mu.Unlock()
			DashboardManager.Log("❌ [WS-USER] Authentication error detected. Stopping reconnection.", LogError)
			return
		}

		w.scheduleUserReconnect()
		return
	}

	w.mu.Lock()
	w.userConn = conn
	w.reconnectAttempts = 0
	onMsg := w.onUserMsg
	w.mu.Unlock()

	DashboardManager.Log("✅ [WS-USER] Connected & Authenticated.", LogInfo)

	go w.readLoop(conn, func(raw []byte) {
		if onMsg == nil {
			return
		}
		var v map[string]any
		if err := json.Unmarshal(raw, &v); err != nil {
			return
		}
		evt, _ := v["event_type"].(string)
		if evt == "trade" || evt == "order" {
			onMsg(v)
		}
		if evt == "error" {
			if msg, _ := v["message"].(string); msg != "" {
				DashboardManager.Log("❌ [WS-USER] Server Error: "+msg, LogError)
			}
		}
	}, func(err error) {
		if err != nil {
			DashboardManager.Log("[WS-USER] Closed: "+err.Error(), LogWarn)
		}
		w.scheduleUserReconnect()
	})
}

func (w *WebSocketService) scheduleUserReconnect() {
	w.mu.Lock()
	attempt := w.reconnectAttempts + 1
	w.reconnectAttempts = attempt
	max := w.maxReconnect
	creds := w.userCreds
	w.mu.Unlock()

	if attempt > max {
		DashboardManager.Log("❌ [WS-USER] Max reconnection attempts reached. Stopping reconnection.", LogError)
		return
	}

	delay := time.Duration(attempt*5) * time.Second
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	DashboardManager.Log(
		"⚠️ [WS-USER] Disconnected. Reconnecting... (Attempt "+strconv.Itoa(attempt)+"/"+strconv.Itoa(max)+")",
		LogWarn,
	)

	select {
	case <-time.After(delay):
		if creds != nil {
			w.connectUser(*creds)
		}
	case <-w.closeCh:
	}
}

func (w *WebSocketService) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	select {
	case <-w.closeCh:
		// already closed
	default:
		close(w.closeCh)
	}
	if w.marketConn != nil {
		_ = w.marketConn.Close()
		w.marketConn = nil
	}
	if w.userConn != nil {
		_ = w.userConn.Close()
		w.userConn = nil
	}
}

func (w *WebSocketService) readLoop(conn *websocket.Conn, onMessage func([]byte), onClose func(error)) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			onClose(err)
			return
		}
		onMessage(data)
	}
}

func hmacSHA256Base64(key, msg []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(msg)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func isAuthErr(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "auth") || strings.Contains(s, "401") || strings.Contains(s, "403")
}
