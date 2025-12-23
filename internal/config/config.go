package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Strategy string

const (
	StrategySniperLadder Strategy = "sniperladder"
	StrategySimpleTrap   Strategy = "simpletrap"
	StrategyGridHedge    Strategy = "gridhedge"
)

type LoadOptions struct {
	StrategyOverride string
}

type Config struct {
	ChainID int

	RPCURL   string
	CLOBHost string

	WsMarketURL string
	WsUserURL   string

	GammaAPIBase string
	DataAPIBase  string
	RelayerURL   string

	PrivateKeyHex string
	ProxyAddress  string
	LiveTrading   bool

	PolymarketAPIKey        string
	PolymarketAPISecretB64  string
	PolymarketAPIPassphrase string

	Strategy Strategy
}

func Load(opts LoadOptions) (Config, error) {
	// 兼容TS项目：默认尝试读取 .env（如果不存在就忽略）
	_ = godotenv.Load()

	cfg := Config{
		ChainID: 137,

		RPCURL:   "https://polygon-rpc.com",
		CLOBHost: "https://clob.polymarket.com",

		WsMarketURL: "wss://ws-subscriptions-clob.polymarket.com/ws/market",
		WsUserURL:   "wss://ws-subscriptions-clob.polymarket.com/ws/user",

		GammaAPIBase: "https://gamma-api.polymarket.com",
		DataAPIBase:  "https://data-api.polymarket.com",
		RelayerURL:   "https://relayer-v2.polymarket.com",

		Strategy: StrategySniperLadder,
	}

	cfg.PrivateKeyHex = strings.TrimSpace(os.Getenv("PRIVATE_KEY"))
	cfg.ProxyAddress = strings.TrimSpace(os.Getenv("POLYMARKET_PROXY_ADDRESS"))
	cfg.LiveTrading = strings.TrimSpace(os.Getenv("LIVE_TRADING")) == "true"

	cfg.PolymarketAPIKey = strings.TrimSpace(os.Getenv("POLYMARKET_API_KEY"))
	cfg.PolymarketAPISecretB64 = strings.TrimSpace(os.Getenv("POLYMARKET_API_SECRET"))
	cfg.PolymarketAPIPassphrase = strings.TrimSpace(os.Getenv("POLYMARKET_PASSPHRASE"))

	if s := strings.TrimSpace(opts.StrategyOverride); s != "" {
		switch strings.ToLower(s) {
		case string(StrategySniperLadder):
			cfg.Strategy = StrategySniperLadder
		case string(StrategySimpleTrap):
			cfg.Strategy = StrategySimpleTrap
		case string(StrategyGridHedge):
			cfg.Strategy = StrategyGridHedge
		default:
			return Config{}, fmt.Errorf("未知策略: %q（支持 sniperladder/simpletrap/gridhedge）", s)
		}
	}

	// TS项目行为：没有PRIVATE_KEY就随机钱包（仅纸上交易）。Go版也允许，但LiveTrading必须有。
	if cfg.LiveTrading && cfg.PrivateKeyHex == "" {
		return Config{}, errors.New("LIVE_TRADING=true 但未设置 PRIVATE_KEY")
	}

	return cfg, nil
}
