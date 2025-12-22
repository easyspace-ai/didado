package services

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"polymarketbot/internal/relayer"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type TokenClaimerConfig struct {
	SignerKey         *ecdsa.PrivateKey
	BuilderKey        string
	BuilderSecretB64  string
	BuilderPassphrase string
	ProxyAddress      string
	DataAPIBase       string
	RelayerURL        string
	ChainID           int
	PollInterval      time.Duration
}

type TokenClaimer struct {
	userAddress string
	dataAPIBase string

	relayClient *relayer.RelayClient
	http        *http.Client
	abi         abi.ABI

	mu       sync.Mutex
	polling  bool
	stopCh   chan struct{}
	interval time.Duration
}

func NewTokenClaimer(cfg TokenClaimerConfig) (*TokenClaimer, error) {
	if cfg.DataAPIBase == "" {
		cfg.DataAPIBase = "https://data-api.polymarket.com"
	}
	if cfg.RelayerURL == "" {
		cfg.RelayerURL = "https://relayer-v2.polymarket.com"
	}
	if cfg.ChainID == 0 {
		cfg.ChainID = 137
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}

	builderCreds := &relayer.BuilderCreds{
		Key:        cfg.BuilderKey,
		SecretB64:  cfg.BuilderSecretB64,
		Passphrase: cfg.BuilderPassphrase,
	}

	rc, err := relayer.NewRelayClient(cfg.RelayerURL, cfg.ChainID, cfg.SignerKey, builderCreds)
	if err != nil {
		return nil, err
	}

	abiCTF, err := abi.JSON(strings.NewReader(`[
	  {"inputs":[{"internalType":"address","name":"collateralToken","type":"address"},{"internalType":"bytes32","name":"parentCollectionId","type":"bytes32"},{"internalType":"bytes32","name":"conditionId","type":"bytes32"},{"internalType":"uint256[]","name":"indexSets","type":"uint256[]"}],"name":"redeemPositions","outputs":[],"stateMutability":"nonpayable","type":"function"}
	]`))
	if err != nil {
		return nil, err
	}

	user := ""
	if cfg.ProxyAddress != "" {
		user = cfg.ProxyAddress
	} else if cfg.SignerKey != nil {
		user = crypto.PubkeyToAddress(cfg.SignerKey.PublicKey).Hex()
	}

	return &TokenClaimer{
		userAddress: user,
		dataAPIBase: strings.TrimRight(cfg.DataAPIBase, "/"),
		relayClient: rc,
		http:        &http.Client{Timeout: 20 * time.Second},
		abi:         abiCTF,
		stopCh:      make(chan struct{}),
		interval:    cfg.PollInterval,
	}, nil
}

func (c *TokenClaimer) StartPolling() {
	c.mu.Lock()
	if c.polling {
		c.mu.Unlock()
		DashboardManager.Log("[CLAIMER] ⚠️ Already polling, skipping start", LogWarn)
		return
	}
	c.polling = true
	c.mu.Unlock()

	DashboardManager.Log(fmt.Sprintf("[CLAIMER] 🚀 Starting automatic token claiming (polling every %ds)", int(c.interval.Seconds())), LogInfo)
	DashboardManager.Log("[CLAIMER] 👤 Checking positions for: "+c.userAddress, LogInfo)

	go func() {
		_, _ = c.CheckAndClaim(context.Background())
	}()

	ticker := time.NewTicker(c.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, _ = c.CheckAndClaim(context.Background())
			case <-c.stopCh:
				return
			}
		}
	}()
}

func (c *TokenClaimer) StopPolling() {
	c.mu.Lock()
	if !c.polling {
		c.mu.Unlock()
		return
	}
	c.polling = false
	close(c.stopCh)
	c.stopCh = make(chan struct{})
	c.mu.Unlock()
	DashboardManager.Log("[CLAIMER] 🛑 Stopped polling for token claims", LogInfo)
}

func (c *TokenClaimer) ClaimOnce(ctx context.Context) (int, error) {
	return c.CheckAndClaim(ctx)
}

func (c *TokenClaimer) CheckAndClaim(ctx context.Context) (int, error) {
	if c.userAddress == "" {
		return 0, fmt.Errorf("missing user address")
	}

	url := fmt.Sprintf("%s/positions?user=%s&redeemable=true", c.dataAPIBase, c.userAddress)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return 0, nil
	}
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("data api http %d: %s", resp.StatusCode, string(raw))
	}

	var positions []map[string]any
	if err := json.Unmarshal(raw, &positions); err != nil {
		return 0, err
	}
	if len(positions) == 0 {
		return 0, nil
	}

	// conditionId -> indexSet(s)
	conditionIndexSets := map[string]map[string]struct{}{}
	for _, p := range positions {
		if cid, _ := p["conditionId"].(string); cid != "" {
			if _, ok := conditionIndexSets[cid]; !ok {
				conditionIndexSets[cid] = map[string]struct{}{}
			}
			for _, s := range extractIndexSets(p) {
				conditionIndexSets[cid][s] = struct{}{}
			}
		}
	}
	if len(conditionIndexSets) == 0 {
		return 0, nil
	}

	DashboardManager.Log(fmt.Sprintf("[CLAIMER] 💰 Found %d redeemable position(s)", len(positions)), LogInfo)
	DashboardManager.Log(fmt.Sprintf("[CLAIMER] 🔗 Redeeming %d condition(s)...", len(conditionIndexSets)), LogInfo)

	success := 0
	for cid, setStrs := range conditionIndexSets {
		indexSets := make([]*big.Int, 0, len(setStrs))
		for s := range setStrs {
			if bi, ok := parseBigIntLoose(s); ok && bi.Sign() >= 0 {
				indexSets = append(indexSets, bi)
			}
		}
		// Backwards-compatible fallback (previous behavior)
		if len(indexSets) == 0 {
			indexSets = []*big.Int{big.NewInt(1), big.NewInt(2)}
		}

		err := c.redeemCondition(ctx, cid, indexSets)
		if err != nil {
			DashboardManager.Log(fmt.Sprintf("[CLAIMER] ❌ Error redeeming condition %s...: %s", trim10(cid), err.Error()), LogError)
			continue
		}
		success++
	}
	if success > 0 {
		DashboardManager.Log(fmt.Sprintf("[CLAIMER] ✅ Successfully redeemed %d/%d condition(s)", success, len(conditionIndexSets)), LogInfo)
	}
	return success, nil
}

func (c *TokenClaimer) redeemCondition(ctx context.Context, conditionID string, indexSets []*big.Int) error {
	formatted := conditionID
	if !strings.HasPrefix(formatted, "0x") {
		formatted = "0x" + formatted
	}
	if len(formatted) != 66 {
		return fmt.Errorf("invalid conditionId length")
	}

	var parent [32]byte // zero hash
	var cond [32]byte
	b, err := hex.DecodeString(strings.TrimPrefix(formatted, "0x"))
	if err != nil {
		return err
	}
	copy(cond[:], b)

	calldata, err := c.abi.Pack("redeemPositions",
		common.HexToAddress("0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"),
		parent,
		cond,
		indexSets,
	)
	if err != nil {
		return err
	}

	tx := relayer.Transaction{
		To:    "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045",
		Data:  "0x" + hex.EncodeToString(calldata),
		Value: "0",
	}

	sub, err := c.relayClient.ExecuteSafe(ctx, []relayer.Transaction{tx}, "redeem positions")
	if err != nil {
		return err
	}
	DashboardManager.Log(fmt.Sprintf("[CLAIMER] ⏳ Condition %s... - Relayer task ID: %s", trim10(conditionID), sub.TransactionID), LogInfo)
	r, err := c.relayClient.Wait(ctx, sub.TransactionID)
	if err != nil {
		return err
	}
	DashboardManager.Log(fmt.Sprintf("[CLAIMER] ✅ SUCCESS! Condition %s... - Tx: %s", trim10(conditionID), r.TransactionHash), LogInfo)
	return nil
}

func trim10(s string) string {
	rs := []rune(s)
	if len(rs) <= 10 {
		return s
	}
	return string(rs[:10])
}

func extractIndexSets(p map[string]any) []string {
	// Data API variants: indexSet, index_set, indexSets
	var out []string
	for _, k := range []string{"indexSet", "index_set", "indexSets"} {
		v, ok := p[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			if strings.TrimSpace(t) != "" {
				out = append(out, strings.TrimSpace(t))
			}
		case float64:
			out = append(out, fmt.Sprintf("%.0f", t))
		case []any:
			for _, it := range t {
				switch x := it.(type) {
				case string:
					if strings.TrimSpace(x) != "" {
						out = append(out, strings.TrimSpace(x))
					}
				case float64:
					out = append(out, fmt.Sprintf("%.0f", x))
				}
			}
		}
	}
	return out
}

func parseBigIntLoose(s string) (*big.Int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	// allow hex "0x.." or decimal
	i := new(big.Int)
	if _, ok := i.SetString(s, 0); ok {
		return i, true
	}
	// sometimes comes as floatish string
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		i.SetInt64(int64(f))
		return i, true
	}
	return nil, false
}
