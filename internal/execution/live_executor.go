package execution

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"strconv"
	"strings"
	"time"

	poly "github.com/0xNetuser/Polymarket-golang/polymarket"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// LiveExecutor executes real trades on Polymarket (REAL FUNDS).
type LiveExecutor struct {
	clob    *poly.ClobClient
	chainID int64

	privateKeyHex string
	funder        common.Address

	rpcURL string
	rpc    *ethclient.Client

	erc20ABI abi.ABI
	ctfABI   abi.ABI
}

const (
	polygonRPCDefault = "https://polygon-rpc.com"
)

func NewLiveExecutor(clob *poly.ClobClient, privateKeyHex string, funderAddress string, chainID int, rpcURL string) (*LiveExecutor, error) {
	if clob == nil {
		return nil, errors.New("nil clob client")
	}
	if rpcURL == "" {
		rpcURL = polygonRPCDefault
	}
	rpc, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, err
	}

	erc20ABI, err := abi.JSON(strings.NewReader(`[
	  {"constant":true,"inputs":[{"name":"owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"}
	]`))
	if err != nil {
		return nil, err
	}
	ctfABI, err := abi.JSON(strings.NewReader(`[
	  {"constant":true,"inputs":[{"name":"owner","type":"address"},{"name":"id","type":"uint256"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},
	  {"constant":false,"inputs":[{"name":"collateralToken","type":"address"},{"name":"parentCollectionId","type":"bytes32"},{"name":"conditionId","type":"bytes32"},{"name":"indexSets","type":"uint256[]"}],"name":"redeemPositions","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"}
	]`))
	if err != nil {
		return nil, err
	}

	funder := common.HexToAddress(funderAddress)
	return &LiveExecutor{
		clob:          clob,
		privateKeyHex: privateKeyHex,
		funder:        funder,
		chainID:       int64(chainID),
		rpcURL:        rpcURL,
		rpc:           rpc,
		erc20ABI:      erc20ABI,
		ctfABI:        ctfABI,
	}, nil
}

func (e *LiveExecutor) GetBalance(ctx context.Context) (float64, error) {
	usdc := common.HexToAddress(USDCAddress)
	out := new(big.Int)
	contract := bind.NewBoundContract(usdc, e.erc20ABI, e.rpc, nil, nil)
	results := []any{out}
	if err := contract.Call(&bind.CallOpts{Context: ctx}, &results, "balanceOf", e.funder); err != nil {
		return 0, err
	}
	// USDC has 6 decimals
	return toFloat(out, 6), nil
}

func (e *LiveExecutor) PlaceOrder(ctx context.Context, params TradeParams) (string, error) {
	_ = ctx
	ot := params.Type
	if ot == "" {
		ot = OrderTypeGTC
	}
	fmt.Printf("📝 [LIVE] Placing Order: %s %.4g @ $%.4f [%s]\n", params.Side, params.Size, params.Price, ot)

	orderArgs := &poly.OrderArgs{
		TokenID:    params.TokenID,
		Price:      params.Price,
		Size:       params.Size,
		Side:       string(params.Side),
		FeeRateBps: 0,
		Nonce:      rand.Intn(1_000_000_000),
		Expiration: 0,
		Taker:      "",
	}

	order, err := e.clob.CreateOrder(orderArgs, nil)
	if err != nil {
		return "", err
	}

	pot := poly.OrderTypeGTC
	switch ot {
	case OrderTypeFOK:
		pot = poly.OrderTypeFOK
	case OrderTypeGTD:
		pot = poly.OrderTypeGTD
	case OrderTypeFAK:
		pot = poly.OrderTypeFAK
	}

	resp, err := e.clob.PostOrder(order, pot)
	if err != nil {
		return "", err
	}

	// Parse order id (TS compatible)
	if m, ok := resp.(map[string]interface{}); ok {
		if v, ok := m["errorMsg"]; ok && v != nil && fmt.Sprint(v) != "" {
			return "", fmt.Errorf("CLOB API Error: %v", v)
		}
		if v, ok := m["error"]; ok && v != nil && fmt.Sprint(v) != "" {
			return "", fmt.Errorf("CLOB API Error: %v", v)
		}
		if v, ok := m["success"]; ok {
			if b, ok := v.(bool); ok && !b {
				return "", fmt.Errorf("CLOB API Error: success=false")
			}
		}

		for _, k := range []string{"orderID", "orderId", "id"} {
			if v, ok := m[k]; ok {
				id := fmt.Sprint(v)
				if id != "" {
					fmt.Println("✅ [LIVE] Order Posted! ID:", id)
					return id, nil
				}
			}
		}
	}

	return "", fmt.Errorf("No Order ID returned: %T", resp)
}

func (e *LiveExecutor) CancelAll(ctx context.Context) error {
	_ = ctx
	_, err := e.clob.CancelAll()
	return err
}

func (e *LiveExecutor) CancelOrder(ctx context.Context, orderID string) error {
	_ = ctx
	var lastErr error
	for i := 0; i < 3; i++ {
		_, err := e.clob.Cancel(orderID)
		if err == nil {
			return nil
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	return lastErr
}

func (e *LiveExecutor) GetOrderStatus(ctx context.Context, orderID string) (OrderStatus, error) {
	_ = ctx
	resp, err := e.clob.GetOrder(orderID)
	if err != nil {
		return OrderStatus{Matched: 0, Cancelled: false, AvgFillPrice: 0}, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return OrderStatus{Matched: 0, Cancelled: false, AvgFillPrice: 0}, nil
	}

	matched := parseFloatAny(firstAny(m, "size_matched", "sizeMatched", "matched", "sizeMatched"))
	cancelled := false
	if st, _ := m["status"].(string); strings.EqualFold(st, "CANCELLED") {
		cancelled = true
	}
	if c, ok := m["canceled"].(bool); ok && c {
		cancelled = true
	}

	avgFillPrice := parseFloatAny(m["price"])
	if at, ok := m["associate_trades"].([]interface{}); ok && len(at) > 0 {
		totalVal := 0.0
		totalQty := 0.0
		for _, t := range at {
			tm, _ := t.(map[string]interface{})
			if tm == nil {
				continue
			}
			q := parseFloatAny(tm["size"])
			p := parseFloatAny(tm["price"])
			totalVal += q * p
			totalQty += q
		}
		if totalQty > 0 {
			avgFillPrice = totalVal / totalQty
		}
	}

	return OrderStatus{Matched: matched, Cancelled: cancelled, AvgFillPrice: avgFillPrice}, nil
}

func (e *LiveExecutor) GetPositions(ctx context.Context, tokenIDYes, tokenIDNo string) (yes float64, no float64, err error) {
	ctf := common.HexToAddress(strings.ToLower(CTFContractAddress))
	contract := bind.NewBoundContract(ctf, e.ctfABI, e.rpc, nil, nil)

	balYes := new(big.Int)
	balNo := new(big.Int)
	idYes, err := parseBigInt(tokenIDYes)
	if err != nil {
		return 0, 0, err
	}
	idNo, err := parseBigInt(tokenIDNo)
	if err != nil {
		return 0, 0, err
	}

	resYes := []any{balYes}
	if err := contract.Call(&bind.CallOpts{Context: ctx}, &resYes, "balanceOf", e.funder, idYes); err != nil {
		return 0, 0, err
	}
	resNo := []any{balNo}
	if err := contract.Call(&bind.CallOpts{Context: ctx}, &resNo, "balanceOf", e.funder, idNo); err != nil {
		return 0, 0, err
	}
	// positions share decimals in this bot are treated as 1e6 (mirrors TS implementation)
	return toFloat(balYes, 6), toFloat(balNo, 6), nil
}

func (e *LiveExecutor) GetOpenOrders(ctx context.Context, marketSlug string) ([]OpenOrder, error) {
	_ = ctx
	_ = marketSlug
	// Mirror TS: order recovery limited
	return []OpenOrder{}, nil
}

func (e *LiveExecutor) GetAddress(ctx context.Context) (string, error) {
	_ = ctx
	return e.funder.Hex(), nil
}

func (e *LiveExecutor) RedeemPositions(ctx context.Context, conditionID string) (string, error) {
	// This is on-chain redemption (gas required), mirrors TS LiveExecutor.redeemPositions().
	privateKey, err := parseECDSA(e.privateKeyHex)
	if err != nil {
		return "", err
	}
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, big.NewInt(e.chainID))
	if err != nil {
		return "", err
	}
	auth.Context = ctx

	// legacy gasPrice strategy (Polygon)
	gp, err := e.rpc.SuggestGasPrice(ctx)
	if err == nil {
		min := big.NewInt(150_000_000_000) // 150 gwei
		gp3 := new(big.Int).Mul(gp, big.NewInt(3))
		if gp3.Cmp(min) < 0 {
			gp3 = min
		}
		auth.GasPrice = gp3
	}

	ctf := common.HexToAddress(CTFContractAddress)
	contract := bind.NewBoundContract(ctf, e.ctfABI, e.rpc, e.rpc, e.rpc)

	parent := [32]byte{}
	cond, err := parseBytes32(conditionID)
	if err != nil {
		return "", err
	}
	indexSets := []*big.Int{big.NewInt(1), big.NewInt(2)}

	tx, err := contract.Transact(auth, "redeemPositions", common.HexToAddress(USDCAddress), parent, cond, indexSets)
	if err != nil {
		return "", err
	}
	return tx.Hash().Hex(), nil
}

func parseECDSA(pk string) (*ecdsa.PrivateKey, error) {
	pk = strings.TrimSpace(pk)
	if pk == "" {
		return nil, errors.New("empty private key")
	}
	if strings.HasPrefix(pk, "0x") {
		pk = pk[2:]
	}
	return crypto.HexToECDSA(pk)
}

func parseBigInt(s string) (*big.Int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("empty token id")
	}
	i := new(big.Int)
	if _, ok := i.SetString(s, 0); ok {
		return i, nil
	}
	return nil, fmt.Errorf("invalid integer: %q", s)
}

func parseBytes32(s string) ([32]byte, error) {
	var out [32]byte
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "0x") {
		s = s[2:]
	}
	b, err := hexDecode(s)
	if err != nil {
		return out, err
	}
	if len(b) != 32 {
		return out, fmt.Errorf("conditionId must be 32 bytes, got %d", len(b))
	}
	copy(out[:], b)
	return out, nil
}

func parseFloatAny(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	default:
		return 0
	}
}

func firstAny(m map[string]interface{}, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

func toFloat(i *big.Int, decimals int) float64 {
	if i == nil {
		return 0
	}
	den := new(big.Float).SetFloat64(float64Pow10(decimals))
	num := new(big.Float).SetInt(i)
	f, _ := new(big.Float).Quo(num, den).Float64()
	return f
}

func float64Pow10(n int) float64 {
	p := 1.0
	for i := 0; i < n; i++ {
		p *= 10
	}
	return p
}

func hexDecode(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if len(s)%2 == 1 {
		s = "0" + s
	}
	dst := make([]byte, len(s)/2)
	_, err := hex.Decode(dst, []byte(s))
	return dst, err
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
