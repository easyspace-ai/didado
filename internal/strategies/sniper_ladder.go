package strategies

import (
	"log"
	"polymarket-go/internal/core"
	"polymarket-go/internal/execution"
	"polymarket-go/internal/services"
	"sync"
	"time"
)

type SniperLadder struct {
	api      *services.PolymarketService
	ws       *services.WebSocketService
	executor execution.Executor
	
	// Config
	InitialTrapPrice float64
	InitialTrapSize  float64
	LegThreshold     float64
	LadderLevels     []LadderLevel
	
	// State
	Phase           string
	FilledCountYes  float64
	FilledCountNo   float64
	AvgCostYes      float64
	AvgCostNo       float64
	TokenIDYes      string
	TokenIDNo       string
	PriceYes        float64
	PriceNo         float64
	
	ActiveOrders    map[string]*ActiveOrder
	OrderFillHistory map[string]float64
	
	MarketEndTimeMs int64
	CurrentSlug     string
	
	mu sync.Mutex
	
	marketMsgChan chan interface{}
	userMsgChan   chan interface{}
	stopChan      chan struct{}
}

type LadderLevel struct {
	Price float64
	Size  float64
}

type ActiveOrder struct {
	ID    string
	Side  string
	Type  string // "TRAP", "LADDER", "HEDGE"
	Price float64
}

func NewSniperLadder(api *services.PolymarketService, ws *services.WebSocketService, exec execution.Executor) *SniperLadder {
	return &SniperLadder{
		api:              api,
		ws:               ws,
		executor:         exec,
		InitialTrapPrice: 0.45,
		InitialTrapSize:  5.0,
		LegThreshold:     4.95,
		LadderLevels: []LadderLevel{
			{Price: 0.37, Size: 5},
			{Price: 0.30, Size: 5},
		},
		Phase:            "NEUTRAL",
		ActiveOrders:     make(map[string]*ActiveOrder),
		OrderFillHistory: make(map[string]float64),
		marketMsgChan:    make(chan interface{}, 100),
		userMsgChan:      make(chan interface{}, 100),
		stopChan:         make(chan struct{}),
	}
}

func (s *SniperLadder) Start() {
	log.Println("🚀 SNIPER LADDER BOT STARTED (Go Version)")
	
	go s.runLifecycle()
}

func (s *SniperLadder) Stop() {
	close(s.stopChan)
}

func (s *SniperLadder) runLifecycle() {
	mm := core.NewMarketMath()
	target := mm.GetNextMarketSlug()
	// Check current first (simplified for now, strictly following TS logic would require more calls)
	
	log.Printf("🔍 Target Market: %s", target.Slug)
	
	// Wait logic
	waitMs := mm.GetMsUntil(target.StartTimeMs) - 60000
	if waitMs > 0 {
		log.Printf("⏳ Waiting %d ms...", waitMs)
		time.Sleep(time.Duration(waitMs) * time.Millisecond)
	}

	// Get Tokens
	tokens, err := s.api.GetMarketTokenIds(target.Slug)
	if err != nil || len(tokens) < 2 {
		log.Printf("❌ Failed to get tokens")
		return
	}
	s.TokenIDYes = tokens[0]
	s.TokenIDNo = tokens[1]
	
	// Place Traps
	s.placeTrapOrders()
	
	// Subscribe WS
	s.ws.SubscribeMarket([]string{s.TokenIDYes, s.TokenIDNo}, func(msg interface{}) {
		s.marketMsgChan <- msg
	})
	
	// Assuming creds are handled in main or passed in
	// s.ws.SubscribeUser(...) 

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg := <-s.marketMsgChan:
			s.handleMarketTick(msg)
		case msg := <-s.userMsgChan:
			s.handleUserFill(msg)
		case <-ticker.C:
			// Periodic checks
		case <-s.stopChan:
			return
		}
	}
}

func (s *SniperLadder) handleMarketTick(msg interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Parse msg (map[string]interface{})
	m, ok := msg.(map[string]interface{})
	if !ok {
		return
	}
	
	// Update prices logic (simplified)
	if assetID, ok := m["asset_id"].(string); ok {
		if priceStr, ok := m["price"].(string); ok {
			// parse float
			// update s.PriceYes / s.PriceNo
			_ = assetID
			_ = priceStr
		}
	}
}

func (s *SniperLadder) handleUserFill(msg interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Parse trade event
	// Update inventory
	// Check triggers
}

func (s *SniperLadder) placeTrapOrders() {
	log.Printf("🪤 Placing TRAP orders @ $%.2f...", s.InitialTrapPrice)
	
	idYes, err := s.executor.PlaceOrder(execution.TradeParams{
		TokenID: s.TokenIDYes,
		Side:    "BUY",
		Price:   s.InitialTrapPrice,
		Size:    s.InitialTrapSize,
		Type:    "GTC",
	})
	if err == nil {
		s.ActiveOrders[idYes] = &ActiveOrder{ID: idYes, Side: "YES", Type: "TRAP", Price: s.InitialTrapPrice}
	}
	
	idNo, err := s.executor.PlaceOrder(execution.TradeParams{
		TokenID: s.TokenIDNo,
		Side:    "BUY",
		Price:   s.InitialTrapPrice,
		Size:    s.InitialTrapSize,
		Type:    "GTC",
	})
	if err == nil {
		s.ActiveOrders[idNo] = &ActiveOrder{ID: idNo, Side: "NO", Type: "TRAP", Price: s.InitialTrapPrice}
	}
}
