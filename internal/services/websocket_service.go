package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Credentials struct {
	Key        string
	Secret     string
	Passphrase string
}

type WebSocketService struct {
	MarketURL      string
	UserURL        string
	ActiveTokenIDs []string
	OnMarketMsg    func(interface{})
	OnUserMsg      func(interface{})

	marketWs          *websocket.Conn
	userWs            *websocket.Conn
	userCreds         *Credentials
	reconnectAttempts int
	mu                sync.Mutex
	done              chan struct{}
}

func NewWebSocketService() *WebSocketService {
	return &WebSocketService{
		MarketURL: "wss://ws-subscriptions-clob.polymarket.com/ws/market",
		UserURL:   "wss://ws-subscriptions-clob.polymarket.com/ws/user",
		done:      make(chan struct{}),
	}
}

func (s *WebSocketService) SubscribeMarket(tokenIDs []string, onMessage func(interface{})) {
	s.mu.Lock()
	if s.marketWs != nil {
		s.marketWs.Close()
	}
	s.ActiveTokenIDs = tokenIDs
	s.OnMarketMsg = onMessage
	s.mu.Unlock()

	go s.connectMarket()
}

func (s *WebSocketService) connectMarket() {
	log.Println("🔌 [WS-MARKET] Connecting...")
	
	conn, _, err := websocket.DefaultDialer.Dial(s.MarketURL, nil)
	if err != nil {
		log.Printf("❌ [WS-MARKET] Connection failed: %v", err)
		time.Sleep(5 * time.Second)
		go s.connectMarket()
		return
	}

	s.mu.Lock()
	s.marketWs = conn
	s.mu.Unlock()

	log.Println("✅ [WS-MARKET] Connected.")

	// Send subscription
	if len(s.ActiveTokenIDs) > 0 {
		payload := map[string]interface{}{
			"assets_ids": s.ActiveTokenIDs,
			"token_ids":  s.ActiveTokenIDs,
			"type":       "market",
		}
		if err := conn.WriteJSON(payload); err != nil {
			log.Printf("❌ [WS-MARKET] Subscription failed: %v", err)
			conn.Close()
			return
		}
	}

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("⚠️ [WS-MARKET] Read error: %v", err)
			break
		}

		var msg interface{}
		if err := json.Unmarshal(message, &msg); err == nil && s.OnMarketMsg != nil {
			s.OnMarketMsg(msg)
		}
	}

	s.marketWs.Close()
	time.Sleep(5 * time.Second)
	go s.connectMarket()
}

func (s *WebSocketService) SubscribeUser(creds *Credentials, onMessage func(interface{})) {
	s.mu.Lock()
	if s.userWs != nil {
		s.userWs.Close()
	}
	s.userCreds = creds
	s.OnUserMsg = onMessage
	s.mu.Unlock()

	go s.connectUser()
}

func (s *WebSocketService) connectUser() {
	creds := s.userCreds
	if creds == nil {
		return
	}

	log.Println("🔐 [WS-USER] Generating Auth Signature...")

	timestamp := time.Now().Unix()
	message := fmt.Sprintf("%dGET/ws/user", timestamp)
	
	h := hmac.New(sha256.New, []byte(creds.Secret))
	h.Write([]byte(message))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	header := http.Header{}
	header.Set("POLY-API-KEY", creds.Key)
	header.Set("POLY-PASSPHRASE", creds.Passphrase)
	header.Set("POLY-TIMESTAMP", fmt.Sprintf("%d", timestamp))
	header.Set("POLY-SIGNATURE", signature)

	log.Println("🔐 [WS-USER] Connecting Authenticated Stream...")
	
	conn, _, err := websocket.DefaultDialer.Dial(s.UserURL, header)
	if err != nil {
		log.Printf("❌ [WS-USER] Connection failed: %v", err)
		if s.reconnectAttempts < 10 {
			s.reconnectAttempts++
			time.Sleep(time.Duration(s.reconnectAttempts*5) * time.Second)
			go s.connectUser()
		}
		return
	}

	s.mu.Lock()
	s.userWs = conn
	s.reconnectAttempts = 0
	s.mu.Unlock()

	log.Println("✅ [WS-USER] Connected & Authenticated.")

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("⚠️ [WS-USER] Read error: %v", err)
			break
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err == nil {
			eventType, _ := msg["event_type"].(string)
			if eventType == "trade" || eventType == "order" {
				if s.OnUserMsg != nil {
					s.OnUserMsg(msg)
				}
			} else if eventType == "error" {
				log.Printf("❌ [WS-USER] Server Error: %v", msg["message"])
			}
		}
	}

	s.userWs.Close()
	// Reconnect logic is slightly different here in TS, but simple loop is fine for now
	time.Sleep(5 * time.Second)
	go s.connectUser()
}

func (s *WebSocketService) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.marketWs != nil {
		s.marketWs.Close()
	}
	if s.userWs != nil {
		s.userWs.Close()
	}
}
