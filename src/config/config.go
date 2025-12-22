package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config 应用配置
type Config struct {
	// 区块链配置
	ChainID int64
	RPCURL  string

	// CLOB 配置
	ClobBaseURL string

	// 钱包配置
	PrivateKey    string
	ProxyAddress  string
	SignatureType int

	// API 凭证
	APICredentials *APICredentials

	// 交易模式
	IsLiveTrading bool

	// 模拟交易配置
	PaperTradingInitialBalance float64

	// 轮询配置
	TokenClaimerPollInterval time.Duration

	// 超时配置
	APIDeriveTimeout time.Duration
	HTTPTimeout       time.Duration
}

// APICredentials API 凭证
type APICredentials struct {
	Key        string
	Secret     string
	Passphrase string
}

// LoadConfig 从环境变量加载配置
func LoadConfig() (*Config, error) {
	// 加载 .env 文件（如果存在）
	_ = godotenv.Load()

	cfg := &Config{
		ChainID:                   137,
		RPCURL:                    getEnv("RPC_URL", "https://polygon-rpc.com"),
		ClobBaseURL:               getEnv("CLOB_BASE_URL", "https://clob.polymarket.com"),
		PrivateKey:                os.Getenv("PRIVATE_KEY"),
		ProxyAddress:              os.Getenv("POLYMARKET_PROXY_ADDRESS"),
		PaperTradingInitialBalance: 1000.0,
		TokenClaimerPollInterval:  5 * time.Second,
		APIDeriveTimeout:          60 * time.Second,
		HTTPTimeout:               30 * time.Second,
	}

	// 解析链 ID
	if chainIDStr := os.Getenv("CHAIN_ID"); chainIDStr != "" {
		chainID, err := strconv.ParseInt(chainIDStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid CHAIN_ID: %w", err)
		}
		cfg.ChainID = chainID
	}

	// 解析交易模式
	cfg.IsLiveTrading = os.Getenv("LIVE_TRADING") == "true"

	// 设置签名类型
	if cfg.ProxyAddress != "" {
		cfg.SignatureType = 1 // Proxy
	} else {
		cfg.SignatureType = 0 // EOA
	}

	// 加载 API 凭证
	apiKey := os.Getenv("POLYMARKET_API_KEY")
	apiSecret := os.Getenv("POLYMARKET_API_SECRET")
	apiPassphrase := os.Getenv("POLYMARKET_PASSPHRASE")

	if apiKey != "" && apiSecret != "" {
		cfg.APICredentials = &APICredentials{
			Key:        apiKey,
			Secret:     apiSecret,
			Passphrase: apiPassphrase,
		}
	}

	// 验证必需配置
	if cfg.PrivateKey == "" && cfg.IsLiveTrading {
		return nil, fmt.Errorf("PRIVATE_KEY is required for live trading")
	}

	return cfg, nil
}

// getEnv 获取环境变量，如果不存在则返回默认值
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Validate 验证配置有效性
func (c *Config) Validate() error {
	if c.ChainID <= 0 {
		return fmt.Errorf("invalid chain ID: %d", c.ChainID)
	}

	if c.RPCURL == "" {
		return fmt.Errorf("RPC URL is required")
	}

	if c.ClobBaseURL == "" {
		return fmt.Errorf("CLOB base URL is required")
	}

	if c.IsLiveTrading && c.PrivateKey == "" {
		return fmt.Errorf("private key is required for live trading")
	}

	return nil
}
