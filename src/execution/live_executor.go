package execution

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ClobClient CLOB 客户端接口 (需要实现)
type ClobClient interface {
	CreateOrder(order OrderRequest) (*Order, error)
	PostOrder(order *Order, orderType string) (*OrderResponse, error)
	CancelOrder(req CancelOrderRequest) error
	CancelAll() error
	GetOrder(orderID string) (*OrderInfo, error)
}

// OrderRequest 订单请求
type OrderRequest struct {
	TokenID  string
	Price    float64
	Side     string
	Size     float64
	FeeRateBps int
}

// Order 订单
type Order struct {
	TokenID string
	Price   float64
	Side    string
	Size    float64
}

// OrderResponse 订单响应
type OrderResponse struct {
	OrderID string `json:"orderID"`
	OrderId string `json:"orderId"`
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	ErrorMsg string `json:"errorMsg"`
}

// CancelOrderRequest 取消订单请求
type CancelOrderRequest struct {
	OrderID string
}

// OrderInfo 订单信息
type OrderInfo struct {
	SizeMatched     string `json:"size_matched"`
	SizeMatchedAlt  string `json:"sizeMatched"`
	Status          string `json:"status"`
	Canceled        bool   `json:"canceled"`
	Price           string `json:"price"`
	AssociateTrades []Trade `json:"associate_trades"`
}

// Trade 交易信息
type Trade struct {
	Size  string `json:"size"`
	Price string `json:"price"`
}

// LiveExecutor 实盘交易执行器
// 使用真实资金在 Polymarket 上进行交易
type LiveExecutor struct {
	client        ClobClient
	signer        *bind.TransactOpts
	funderAddress common.Address
	provider      *ethclient.Client
	ctfContract   *bind.BoundContract
	mu            sync.RWMutex
}

const (
	POLYGON_RPC = "https://polygon-rpc.com"
	CTF_ADDRESS = "0x4d97dcd97e9cc2337ed4192e56f4509c6d87f357" // 小写地址
)

// NewLiveExecutor 创建新的实盘执行器
func NewLiveExecutor(client ClobClient, signer *bind.TransactOpts, funderAddress common.Address) (*LiveExecutor, error) {
	provider, err := ethclient.Dial(POLYGON_RPC)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Polygon RPC: %w", err)
	}

	ctfAddr := common.HexToAddress(CTF_CONTRACT_ADDRESS)
	ctfABI, err := abi.JSON(strings.NewReader(ctfABIJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to parse CTF ABI: %w", err)
	}

	ctfContract := bind.NewBoundContract(ctfAddr, ctfABI, provider, provider, provider)

	fmt.Println("🚨 LIVE TRADING MODE ACTIVE 🚨")

	return &LiveExecutor{
		client:        client,
		signer:        signer,
		funderAddress: funderAddress,
		provider:      provider,
		ctfContract:   ctfContract,
	}, nil
}

// GetBalance 获取当前 USDC 余额
func (l *LiveExecutor) GetBalance() (float64, error) {
	usdcAddr := common.HexToAddress(USDC_ADDRESS)
	usdcABI, err := abi.JSON(strings.NewReader(erc20ABIJSON))
	if err != nil {
		return 0, fmt.Errorf("failed to parse ERC20 ABI: %w", err)
	}

	usdcContract := bind.NewBoundContract(usdcAddr, usdcABI, l.provider, l.provider, l.provider)

	var result []interface{}
	err = usdcContract.Call(nil, &result, "balanceOf", l.funderAddress)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch USDC balance: %w", err)
	}

	if len(result) == 0 {
		return 0, fmt.Errorf("no balance returned")
	}

	balance, ok := result[0].(*big.Int)
	if !ok {
		return 0, fmt.Errorf("invalid balance type")
	}

	// USDC 有 6 位小数
	balanceFloat := new(big.Float).SetInt(balance)
	balanceFloat.Quo(balanceFloat, big.NewFloat(1_000_000))

	balanceValue, _ := balanceFloat.Float64()
	return balanceValue, nil
}

// PlaceOrder 下订单
func (l *LiveExecutor) PlaceOrder(params TradeParams) (string, error) {
	fmt.Printf("📝 [LIVE] Placing Order: %s %.2f @ $%.2f [%s]\n",
		params.Side, params.Size, params.Price, params.Type)

	orderType := "GTC"
	if params.Type != "" {
		orderType = params.Type
	}

	side := "BUY"
	if params.Side == "SELL" {
		side = "SELL"
	}

	order, err := l.client.CreateOrder(OrderRequest{
		TokenID:    params.TokenID,
		Price:      params.Price,
		Side:       side,
		Size:       params.Size,
		FeeRateBps: 0,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create order: %w", err)
	}

	response, err := l.client.PostOrder(order, orderType)
	if err != nil {
		return "", fmt.Errorf("failed to post order: %w", err)
	}

	if response.Error != "" || response.ErrorMsg != "" || !response.Success {
		errorMsg := response.Error
		if errorMsg == "" {
			errorMsg = response.ErrorMsg
		}
		if errorMsg == "" {
			errorMsg = "Unknown Error"
		}
		return "", fmt.Errorf("CLOB API Error: %s", errorMsg)
	}

	orderID := response.OrderID
	if orderID == "" {
		orderID = response.OrderId
	}
	if orderID == "" {
		orderID = response.ID
	}
	if orderID == "" {
		return "", fmt.Errorf("no order ID returned")
	}

	fmt.Printf("✅ [LIVE] Order Posted! ID: %s\n", orderID)
	return orderID, nil
}

// CancelAll 取消所有订单
func (l *LiveExecutor) CancelAll() error {
	err := l.client.CancelAll()
	if err != nil {
		return fmt.Errorf("failed to cancel orders: %w", err)
	}
	fmt.Println("✅ All orders cancelled.")
	return nil
}

// CancelOrder 取消指定订单
func (l *LiveExecutor) CancelOrder(orderID string) error {
	for i := 0; i < 3; i++ {
		err := l.client.CancelOrder(CancelOrderRequest{OrderID: orderID})
		if err == nil {
			fmt.Printf("✅ Order %s cancelled.\n", orderID)
			return nil
		}
		if i < 2 {
			time.Sleep(500 * time.Millisecond)
		} else {
			return err
		}
	}
	return nil
}

// GetOrderStatus 获取订单状态
func (l *LiveExecutor) GetOrderStatus(orderID string) (OrderStatus, error) {
	order, err := l.client.GetOrder(orderID)
	if err != nil {
		return OrderStatus{Matched: 0, Cancelled: false, AvgFillPrice: 0}, nil
	}

	matched := 0.0
	if order.SizeMatched != "" {
		fmt.Sscanf(order.SizeMatched, "%f", &matched)
	} else if order.SizeMatchedAlt != "" {
		fmt.Sscanf(order.SizeMatchedAlt, "%f", &matched)
	}

	cancelled := order.Status == "CANCELLED" || order.Canceled

	avgFillPrice := 0.0
	if order.Price != "" {
		fmt.Sscanf(order.Price, "%f", &avgFillPrice)
	}

	// 如果有关联交易，计算平均成交价
	if len(order.AssociateTrades) > 0 {
		totalVal := 0.0
		totalQty := 0.0
		for _, t := range order.AssociateTrades {
			var size, price float64
			fmt.Sscanf(t.Size, "%f", &size)
			fmt.Sscanf(t.Price, "%f", &price)
			totalVal += size * price
			totalQty += size
		}
		if totalQty > 0 {
			avgFillPrice = totalVal / totalQty
		}
	}

	return OrderStatus{
		Matched:      matched,
		Cancelled:    cancelled,
		AvgFillPrice: avgFillPrice,
	}, nil
}

// GetPositions 获取持仓
func (l *LiveExecutor) GetPositions(tokenIDYes, tokenIDNo string) (Positions, error) {
	ctfAddr := common.HexToAddress(CTF_ADDRESS)
	ctfABI, err := abi.JSON(strings.NewReader(ctfABIJSON))
	if err != nil {
		return Positions{Yes: 0, No: 0}, fmt.Errorf("failed to parse CTF ABI: %w", err)
	}

	ctfContract := bind.NewBoundContract(ctfAddr, ctfABI, l.provider, l.provider, l.provider)

	tokenIDYesBig := new(big.Int)
	tokenIDYesBig.SetString(tokenIDYes, 0)

	tokenIDNoBig := new(big.Int)
	tokenIDNoBig.SetString(tokenIDNo, 0)

	var balYes, balNo []interface{}
	err = ctfContract.Call(nil, &balYes, "balanceOf", l.funderAddress, tokenIDYesBig)
	if err != nil {
		fmt.Printf("❌ [RPC ERROR] Could not read YES positions: %v\n", err)
		return Positions{Yes: 0, No: 0}, nil
	}

	err = ctfContract.Call(nil, &balNo, "balanceOf", l.funderAddress, tokenIDNoBig)
	if err != nil {
		fmt.Printf("❌ [RPC ERROR] Could not read NO positions: %v\n", err)
		return Positions{Yes: 0, No: 0}, nil
	}

	yesBalance := 0.0
	noBalance := 0.0

	if len(balYes) > 0 {
		if bal, ok := balYes[0].(*big.Int); ok {
			balFloat := new(big.Float).Quo(new(big.Float).SetInt(bal), big.NewFloat(1_000_000))
			yesBalance, _ = balFloat.Float64()
		}
	}

	if len(balNo) > 0 {
		if bal, ok := balNo[0].(*big.Int); ok {
			balFloat := new(big.Float).Quo(new(big.Float).SetInt(bal), big.NewFloat(1_000_000))
			noBalance, _ = balFloat.Float64()
		}
	}

	return Positions{Yes: yesBalance, No: noBalance}, nil
}

// GetOpenOrders 获取开放订单
func (l *LiveExecutor) GetOpenOrders(marketSlug string) ([]OpenOrder, error) {
	fmt.Println("⚠️ [LIVE] getOpenOrders: Order recovery limited. ClobClient doesn't expose user orders endpoint.")
	return []OpenOrder{}, nil
}

// GetAddress 获取用户地址
func (l *LiveExecutor) GetAddress() (string, error) {
	return l.funderAddress.Hex(), nil
}

// RedeemPositions 赎回持仓
func (l *LiveExecutor) RedeemPositions(conditionID string) (string, error) {
	parentCollectionID := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000")
	indexSets := []*big.Int{big.NewInt(1), big.NewInt(2)}

	fmt.Printf("🔗 [LIVE] Submitting Redemption for %s...\n", conditionID)

	// 获取 gas price
	gasPrice, err := l.provider.SuggestGasPrice(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to get gas price: %w", err)
	}

	// 设置较高的 gas price (3倍，最低 150 gwei)
	minGasPrice := big.NewInt(150000000000) // 150 gwei
	highGasPrice := new(big.Int).Mul(gasPrice, big.NewInt(3))
	if highGasPrice.Cmp(minGasPrice) < 0 {
		highGasPrice = minGasPrice
	}

	fmt.Printf("⛽ Gas Price: %s (%s gwei)\n", highGasPrice.String(), new(big.Int).Div(highGasPrice, big.NewInt(1e9)).String())

	// 格式化 conditionID
	conditionIDHash := common.HexToHash(conditionID)
	if !strings.HasPrefix(conditionID, "0x") {
		conditionIDHash = common.HexToHash("0x" + conditionID)
	}

	usdcAddr := common.HexToAddress(USDC_ADDRESS)

	// 估算 gas (简化实现，实际应该调用合约方法)
	fmt.Printf("⛽ Estimating gas...\n")

	// 创建合约绑定
	ctfAddr := common.HexToAddress(CTF_CONTRACT_ADDRESS)
	ctfABI, err := abi.JSON(strings.NewReader(ctfABIJSON))
	if err != nil {
		return "", fmt.Errorf("failed to parse CTF ABI: %w", err)
	}

	ctfContract := bind.NewBoundContract(ctfAddr, ctfABI, l.provider, l.provider, l.provider)

	// 发送交易
	tx, err := ctfContract.Transact(l.signer, "redeemPositions", usdcAddr, parentCollectionID, conditionIDHash, indexSets)
	if err != nil {
		return "", fmt.Errorf("failed to send transaction: %w", err)
	}

	fmt.Printf("✅ [LIVE] Redemption Sent! Hash: %s\n", tx.Hash().Hex())
	fmt.Printf("   View on PolygonScan: https://polygonscan.com/tx/%s\n", tx.Hash().Hex())

	// 等待确认
	time.Sleep(2 * time.Second)

	receipt, err := bind.WaitMined(context.Background(), l.provider, tx)
	if err != nil {
		return "", fmt.Errorf("failed to wait for confirmation: %w", err)
	}

	if receipt.Status == 0 {
		return "", fmt.Errorf("transaction reverted on-chain")
	}

	fmt.Printf("✅ [LIVE] Redemption Confirmed! Block: %d\n", receipt.BlockNumber.Uint64())
	return tx.Hash().Hex(), nil
}

// ExecutePaperFill 实盘执行器不支持此方法
func (l *LiveExecutor) ExecutePaperFill(orderID string, price, quantity float64) error {
	return fmt.Errorf("ExecutePaperFill not supported in LiveExecutor")
}

// ABI JSON 字符串
const erc20ABIJSON = `[{"constant":true,"inputs":[{"name":"owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"type":"function"}]`

const ctfABIJSON = `[
	{"constant":true,"inputs":[{"name":"owner","type":"address"},{"name":"id","type":"uint256"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"type":"function"},
	{"constant":false,"inputs":[{"name":"collateralToken","type":"address"},{"name":"parentCollectionId","type":"bytes32"},{"name":"conditionId","type":"bytes32"},{"name":"indexSets","type":"uint256[]"}],"name":"redeemPositions","outputs":[],"type":"function"}
]`
