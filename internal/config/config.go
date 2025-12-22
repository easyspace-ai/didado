package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	PrivateKey      string
	RPCURL          string
	ProxyAddress    string
	IsProxy         bool
	LiveTrading     bool
	ChainID         int64
	PolymarketAPI   string
	GammaAPI        string
	ClobAPI         string
}

func Load() *Config {
	err := godotenv.Load()
	if err != nil {
		log.Println("⚠️ No .env file found, using defaults or env vars")
	}

	cfg := &Config{
		PrivateKey:      os.Getenv("PRIVATE_KEY"),
		RPCURL:          getEnv("RPC_URL", "https://polygon-rpc.com"),
		ProxyAddress:    os.Getenv("POLYMARKET_PROXY_ADDRESS"),
		LiveTrading:     os.Getenv("LIVE_TRADING") == "true",
		ChainID:         137, // Polygon Mainnet
		PolymarketAPI:   "https://clob.polymarket.com",
		GammaAPI:        "https://gamma-api.polymarket.com",
		ClobAPI:         "https://clob.polymarket.com",
	}

	if cfg.ProxyAddress != "" {
		cfg.IsProxy = true
	} else {
		// If no proxy address, fallback to using the signer address logic later
		// But in config struct we just mark if it was explicitly set
	}

	return cfg
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
