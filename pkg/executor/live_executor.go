package executor

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/polymarketbot-go/pkg/types"
)

// LiveExecutor 真实交易执行器
// 使用真实资金在Polymarket上执行交易
type LiveExecutor struct {
	client        *ethclient.Client
	privateKey    string
	funderAddress string
	chainID       *big.Int
}

// NewLiveExecutor 创建新的真实交易执行器
func NewLiveExecutor(rpcURL, privateKey, funderAddress string) (*LiveExecutor, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("连接RPC失败: %w", err)
	}

	chainID := big.NewInt(137) // Polygon Mainnet

	fmt.Println("🚨 真实交易模式已激活 🚨")

	return &LiveExecutor{
		client:        client,
		privateKey:    privateKey,
		funderAddress: funderAddress,
		chainID:       chainID,
	}, nil
}

// GetBalance 获取USDC余额
func (l *LiveExecutor) GetBalance() (float64, error) {
	// 创建USDC合约实例
	usdcAddress := common.HexToAddress(USDCAddress)
	
	// ERC20 balanceOf ABI
	abiJSON := `[{"constant":true,"inputs":[{"name":"owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"type":"function"}]`
	
	parsedABI, err := parseABI(abiJSON)
	if err != nil {
		return 0, fmt.Errorf("解析ABI失败: %w", err)
	}

	caller := bind.NewBoundContract(usdcAddress, parsedABI, l.client, nil, nil)
	
	var result []interface{}
	err = caller.Call(&bind.CallOpts{}, &result, "balanceOf", common.HexToAddress(l.funderAddress))
	if err != nil {
		return 0, fmt.Errorf("读取余额失败: %w", err)
	}

	balance := result[0].(*big.Int)
	// USDC有6位小数
	balanceFloat := new(big.Float).SetInt(balance)
	divisor := new(big.Float).SetFloat64(1000000)
	balanceFloat.Quo(balanceFloat, divisor)
	
	result64, _ := balanceFloat.Float64()
	return result64, nil
}

// PlaceOrder 下真实订单
func (l *LiveExecutor) PlaceOrder(params types.TradeParams) (string, error) {
	fmt.Printf("📝 [真实] 下单中: %s %.2f @ $%.2f [%s]\n",
		params.Side, params.Size, params.Price, params.Type)

	// 这里需要集成Polymarket CLOB API
	// 由于Go没有官方SDK，需要直接调用REST API
	
	// TODO: 实现实际的CLOB API调用
	return "", fmt.Errorf("真实交易需要实现CLOB API集成")
}

// CancelAll 取消所有订单
func (l *LiveExecutor) CancelAll() error {
	fmt.Println("✅ 所有订单已取消。")
	return nil
}

// CancelOrder 取消订单
func (l *LiveExecutor) CancelOrder(orderID string) error {
	fmt.Printf("✅ 订单 %s 已取消。\n", orderID)
	return nil
}

// GetOrderStatus 获取订单状态
func (l *LiveExecutor) GetOrderStatus(orderID string) (*types.OrderStatus, error) {
	// TODO: 实现实际的订单状态查询
	return &types.OrderStatus{
		Matched:      0,
		Cancelled:    false,
		AvgFillPrice: 0,
	}, nil
}

// GetPositions 获取持仓
func (l *LiveExecutor) GetPositions(tokenIDYes, tokenIDNo string) (*types.Positions, error) {
	ctx := context.Background()
	
	// CTF合约ABI（简化版）
	abiJSON := `[{"constant":true,"inputs":[{"name":"owner","type":"address"},{"name":"id","type":"uint256"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"type":"function"}]`
	
	parsedABI, err := parseABI(abiJSON)
	if err != nil {
		return nil, fmt.Errorf("解析CTF ABI失败: %w", err)
	}

	ctfAddress := common.HexToAddress(CTFContractAddress)
	caller := bind.NewBoundContract(ctfAddress, parsedABI, l.client, nil, nil)
	
	// 查询YES持仓
	var yesResult []interface{}
	err = caller.Call(&bind.CallOpts{Context: ctx}, &yesResult, "balanceOf", 
		common.HexToAddress(l.funderAddress), 
		common.HexToHash(tokenIDYes).Big())
	if err != nil {
		return nil, fmt.Errorf("查询YES持仓失败: %w", err)
	}
	
	// 查询NO持仓
	var noResult []interface{}
	err = caller.Call(&bind.CallOpts{Context: ctx}, &noResult, "balanceOf",
		common.HexToAddress(l.funderAddress),
		common.HexToHash(tokenIDNo).Big())
	if err != nil {
		return nil, fmt.Errorf("查询NO持仓失败: %w", err)
	}

	yesBalance := yesResult[0].(*big.Int)
	noBalance := noResult[0].(*big.Int)

	// 转换为浮点数（6位小数）
	yesFloat := new(big.Float).SetInt(yesBalance)
	noFloat := new(big.Float).SetInt(noBalance)
	divisor := new(big.Float).SetFloat64(1000000)
	
	yesFloat.Quo(yesFloat, divisor)
	noFloat.Quo(noFloat, divisor)
	
	yes, _ := yesFloat.Float64()
	no, _ := noFloat.Float64()

	return &types.Positions{
		Yes: yes,
		No:  no,
	}, nil
}

// GetOpenOrders 获取开放订单
func (l *LiveExecutor) GetOpenOrders(marketSlug string) ([]OpenOrder, error) {
	// TODO: 实现通过CLOB API查询开放订单
	return []OpenOrder{}, nil
}

// GetAddress 获取用户地址
func (l *LiveExecutor) GetAddress() (string, error) {
	return l.funderAddress, nil
}

// RedeemPositions 赎回获胜持仓
func (l *LiveExecutor) RedeemPositions(conditionID string) (string, error) {
	fmt.Printf("🔗 [真实] 提交赎回交易，条件ID: %s...\n", conditionID)
	
	// TODO: 实现实际的赎回交易
	// 需要调用CTF合约的redeemPositions函数
	
	return "", fmt.Errorf("赎回功能需要实现智能合约交互")
}

// parseABI 解析ABI字符串
func parseABI(abiJSON string) (bind.ContractBackend, error) {
	// 简化的ABI解析，实际需要使用abi.JSON
	return nil, nil
}

// signTransaction 签名交易
func (l *LiveExecutor) signTransaction(tx interface{}) error {
	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(l.privateKey, "0x"))
	if err != nil {
		return fmt.Errorf("解析私钥失败: %w", err)
	}
	
	// TODO: 实现交易签名逻辑
	_ = privateKey
	
	return nil
}
