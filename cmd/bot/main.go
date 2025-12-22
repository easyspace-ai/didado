package main

import (
	"log"
	"os"
	"os/signal"
	"polymarket-go/internal/config"
	"polymarket-go/internal/execution"
	"polymarket-go/internal/polymarket"
	"polymarket-go/internal/services"
	"polymarket-go/internal/strategies"
	"syscall"
)

func main() {
	log.Println("🚀 POLYMARKET SNIPER BOT INITIALIZING... (Go)")

	// 1. Config
	cfg := config.Load()

	// 2. Services
	polyService := services.NewPolymarketService()
	wsService := services.NewWebSocketService()

	// 3. Executor
	var executor execution.Executor
	if cfg.LiveTrading {
		log.Println("🚨 MODE: LIVE TRADING (REAL FUNDS) 🚨")
		if cfg.PrivateKey == "" {
			log.Fatal("❌ PRIVATE_KEY required for live trading")
		}
		
		clobClient, err := polymarket.NewClobClient(cfg.ClobAPI, cfg.ChainID, cfg.PrivateKey)
		if err != nil {
			log.Fatalf("❌ Failed to create ClobClient: %v", err)
		}
		
		funderAddr := cfg.ProxyAddress
		if funderAddr == "" {
			funderAddr = clobClient.Address.Hex()
		}
		
		liveExec, err := execution.NewLiveExecutor(clobClient, funderAddr, cfg.RPCURL)
		if err != nil {
			log.Fatalf("❌ Failed to create LiveExecutor: %v", err)
		}
		executor = liveExec
	} else {
		log.Println("📝 MODE: PAPER TRADING (SIMULATION)")
		executor = execution.NewPaperExecutor(1000)
	}

	// 4. Strategy
	sniper := strategies.NewSniperLadder(polyService, wsService, executor)
	
	// 5. Start
	sniper.Start()

	// 6. Graceful Shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	
	log.Println("\n🛑 Shutting down...")
	sniper.Stop()
	wsService.Close()
	os.Exit(0)
}
