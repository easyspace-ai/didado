package app

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"strings"

	"polymarketbot/internal/config"
	"polymarketbot/internal/execution"
	"polymarketbot/internal/services"
	"polymarketbot/internal/strategies"

	poly "github.com/0xNetuser/Polymarket-golang/polymarket"
	"github.com/ethereum/go-ethereum/crypto"
)

func Run(ctx context.Context, cfg config.Config) error {
	fmt.Println("🚀 POLYMARKET BOT (Go) 启动中...")
	fmt.Println("   ChainID:", cfg.ChainID)
	fmt.Println("   CLOB:", cfg.CLOBHost)
	fmt.Println("   RPC:", cfg.RPCURL)
	fmt.Println("   LiveTrading:", cfg.LiveTrading)
	fmt.Println("   Strategy:", cfg.Strategy)

	api := services.NewPolymarketService(cfg.GammaAPIBase)
	ws := services.NewWebSocketService(cfg.WsMarketURL, cfg.WsUserURL)

	var signerKey *ecdsa.PrivateKey
	if strings.TrimSpace(cfg.PrivateKeyHex) != "" {
		pk := strings.TrimSpace(cfg.PrivateKeyHex)
		if strings.HasPrefix(pk, "0x") {
			pk = pk[2:]
		}
		k, err := crypto.HexToECDSA(pk)
		if err != nil {
			return fmt.Errorf("PRIVATE_KEY 无效: %w", err)
		}
		signerKey = k
	}

	// Executor
	var ex execution.Executor
	if cfg.LiveTrading {
		if signerKey == nil {
			return fmt.Errorf("LIVE_TRADING=true 但 PRIVATE_KEY 不可用")
		}
		funder := ""
		if strings.TrimSpace(cfg.ProxyAddress) != "" {
			funder = strings.TrimSpace(cfg.ProxyAddress)
		} else {
			funder = crypto.PubkeyToAddress(signerKey.PublicKey).Hex()
		}
		signatureType := poly.SignatureTypeEOA
		creds := &poly.ApiCreds{
			APIKey:        cfg.PolymarketAPIKey,
			APISecret:     cfg.PolymarketAPISecretB64,
			APIPassphrase: cfg.PolymarketAPIPassphrase,
		}
		clob, err := poly.NewClobClient(cfg.CLOBHost, cfg.ChainID, cfg.PrivateKeyHex, creds, &signatureType, funder)
		if err != nil {
			return fmt.Errorf("初始化CLOB失败: %w", err)
		}
		live, err := execution.NewLiveExecutor(clob, cfg.PrivateKeyHex, funder, cfg.ChainID, cfg.RPCURL)
		if err != nil {
			return fmt.Errorf("初始化LiveExecutor失败: %w", err)
		}
		ex = live
	} else {
		ex = execution.NewPaperExecutor(1000)
	}

	// TokenClaimer (optional)
	var claimer *services.TokenClaimer
	if signerKey != nil && cfg.PolymarketAPIKey != "" && cfg.PolymarketAPISecretB64 != "" && cfg.PolymarketAPIPassphrase != "" {
		c, err := services.NewTokenClaimer(services.TokenClaimerConfig{
			SignerKey:         signerKey,
			BuilderKey:        cfg.PolymarketAPIKey,
			BuilderSecretB64:  cfg.PolymarketAPISecretB64,
			BuilderPassphrase: cfg.PolymarketAPIPassphrase,
			ProxyAddress:      cfg.ProxyAddress,
			DataAPIBase:       cfg.DataAPIBase,
			RelayerURL:        cfg.RelayerURL,
			ChainID:           cfg.ChainID,
		})
		if err == nil {
			claimer = c
		}
	}

	// Strategy select
	var strat strategies.Strategy
	var done <-chan struct{}
	switch cfg.Strategy {
	case config.StrategySimpleTrap:
		s := strategies.NewSimpleTrap(api, ws, ex)
		strat = s
		done = s.Done()
	case config.StrategyGridHedge:
		s := strategies.NewGridHedge(api, ws, ex)
		strat = s
		done = s.Done()
	default:
		s := strategies.NewSniperLadder(api, ws, ex, claimer)
		strat = s
		done = s.Done()
	}

	// Subscribe private user stream (optional; still works via polling)
	if cfg.PolymarketAPIKey != "" && cfg.PolymarketAPISecretB64 != "" && cfg.PolymarketAPIPassphrase != "" {
		userCreds := services.UserCreds{
			Key:        cfg.PolymarketAPIKey,
			Secret:     cfg.PolymarketAPISecretB64,
			Passphrase: cfg.PolymarketAPIPassphrase,
		}
		switch s := strat.(type) {
		case *strategies.SniperLadder:
			ws.SubscribeUser(userCreds, func(v any) { s.HandleUserWS(ctx, v) })
		case *strategies.SimpleTrap:
			ws.SubscribeUser(userCreds, func(v any) { s.HandleUserWS(ctx, v) })
		case *strategies.GridHedge:
			ws.SubscribeUser(userCreds, func(v any) { s.HandleUserWS(ctx, v) })
		default:
			// ignore
		}
	} else {
		services.DashboardManager.Log("⚠️ 未配置POLYMARKET_API_*，将跳过私有用户WS（仅靠轮询订单状态）", services.LogWarn)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := strat.Start(runCtx); err != nil {
		return err
	}

	select {
	case <-done:
		_ = strat.Stop(context.Background())
		return nil
	case <-ctx.Done():
		_ = strat.Stop(context.Background())
		return ctx.Err()
	}
}
