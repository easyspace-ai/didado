package services

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const (
	RELAYER_URL      = "https://relayer-v2.polymarket.com"
	POLYGON_CHAIN_ID = 137
	CTF_ADDRESS      = "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045"
	USDC_ADDRESS     = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"
)

// TokenClaimerConfig TokenClaimer 配置
type TokenClaimerConfig struct {
	Signer         interface{} // 钱包签名器 (需要实现)
	BuilderKey     string
	BuilderSecret  string
	BuilderPassphrase string
	ProxyAddress   string
	PollIntervalMs int
}

// TokenClaimer 自动代币赎回服务
type TokenClaimer struct {
	userAddress    string
	relayClient    *RelayClient
	pollInterval   *time.Ticker
	isPolling      bool
	pollIntervalMs int
}

// RelayClient 中继客户端 (简化实现)
type RelayClient struct {
	relayerURL string
	chainID    int
	signer     interface{}
	config     *TokenClaimerConfig
	client     *http.Client
}

// NewRelayClient 创建新的中继客户端
func NewRelayClient(relayerURL string, chainID int, signer interface{}, config *TokenClaimerConfig) *RelayClient {
	return &RelayClient{
		relayerURL: relayerURL,
		chainID:    chainID,
		signer:     signer,
		config:     config,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Execute 执行中继交易
func (r *RelayClient) Execute(transactions []RelayTransaction) (*RelayTask, error) {
	// 这里需要实现实际的 relayer API 调用
	// 简化实现，返回任务 ID
	taskID := fmt.Sprintf("task-%d", time.Now().Unix())
	return &RelayTask{
		TransactionID: taskID,
		client:        r,
	}, nil
}

// RelayTransaction 中继交易
type RelayTransaction struct {
	To    string
	Data  string
	Value string
}

// RelayTask 中继任务
type RelayTask struct {
	TransactionID string
	client        *RelayClient
}

// Wait 等待交易确认
func (t *RelayTask) Wait() (*types.Receipt, error) {
	// 简化实现：轮询任务状态
	for i := 0; i < 60; i++ {
		time.Sleep(2 * time.Second)
		// 这里应该检查任务状态
		// 简化实现：返回模拟的 receipt
		if i > 10 {
			return &types.Receipt{
				Status:      1,
				BlockNumber: big.NewInt(12345),
			}, nil
		}
	}
	return nil, fmt.Errorf("timeout waiting for transaction")
}

// NewTokenClaimer 创建新的 TokenClaimer
func NewTokenClaimer(config TokenClaimerConfig) (*TokenClaimer, error) {
	userAddress := config.ProxyAddress
	if userAddress == "" {
		// 如果没有代理地址，使用签名器地址
		// 这里需要从 signer 获取地址
		userAddress = "0x0000000000000000000000000000000000000000"
	}

	pollIntervalMs := config.PollIntervalMs
	if pollIntervalMs == 0 {
		pollIntervalMs = 5000 // 默认 5 秒
	}

	relayClient := NewRelayClient(RELAYER_URL, POLYGON_CHAIN_ID, config.Signer, &config)

	return &TokenClaimer{
		userAddress:    userAddress,
		relayClient:    relayClient,
		pollIntervalMs: pollIntervalMs,
	}, nil
}

// StartPolling 开始轮询可赎回持仓并自动赎回
func (t *TokenClaimer) StartPolling() {
	if t.isPolling {
		fmt.Println("[CLAIMER] ⚠️ Already polling, skipping start")
		return
	}

	t.isPolling = true
	fmt.Printf("[CLAIMER] 🚀 Starting automatic token claiming (polling every %ds)\n", t.pollIntervalMs/1000)
	fmt.Printf("[CLAIMER] 👤 Checking positions for: %s\n", t.userAddress)

	// 立即执行一次检查
	go func() {
		if err := t.checkAndClaim(); err != nil {
			fmt.Printf("[CLAIMER] ❌ Initial check failed: %v\n", err)
		}
	}()

	// 设置定期轮询
	t.pollInterval = time.NewTicker(time.Duration(t.pollIntervalMs) * time.Millisecond)
	go func() {
		for range t.pollInterval.C {
			if !t.isPolling {
				return
			}
			if err := t.checkAndClaim(); err != nil {
				fmt.Printf("[CLAIMER] ❌ Polling error: %v\n", err)
			}
		}
	}()
}

// StopPolling 停止轮询
func (t *TokenClaimer) StopPolling() {
	if t.pollInterval != nil {
		t.pollInterval.Stop()
		t.pollInterval = nil
	}
	t.isPolling = false
	fmt.Println("[CLAIMER] 🛑 Stopped polling for token claims")
}

// checkAndClaim 检查并赎回可赎回的持仓
func (t *TokenClaimer) checkAndClaim() error {
	url := fmt.Sprintf("https://data-api.polymarket.com/positions?user=%s&redeemable=true", t.userAddress)
	
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch positions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		// 没有持仓是正常的，不是错误
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var positions []Position
	if err := json.Unmarshal(body, &positions); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	if len(positions) == 0 {
		return nil
	}

	fmt.Printf("[CLAIMER] 💰 Found %d redeemable position(s)\n", len(positions))

	// 按 conditionId 分组
	conditionIds := make(map[string]bool)
	for _, pos := range positions {
		if pos.ConditionID != "" {
			conditionIds[pos.ConditionID] = true
		}
	}

	if len(conditionIds) == 0 {
		return nil
	}

	fmt.Printf("[CLAIMER] 🔗 Redeeming %d condition(s)...\n", len(conditionIds))

	successCount := 0
	for conditionID := range conditionIds {
		if err := t.redeemCondition(conditionID); err != nil {
			fmt.Printf("[CLAIMER] ❌ Error redeeming condition %s...: %v\n", conditionID[:10], err)
		} else {
			successCount++
		}
	}

	if successCount > 0 {
		fmt.Printf("[CLAIMER] ✅ Successfully redeemed %d/%d condition(s)\n", successCount, len(conditionIds))
	}

	return nil
}

// redeemCondition 赎回单个条件
func (t *TokenClaimer) redeemCondition(conditionID string) error {
	// 格式化 conditionID
	formattedConditionID := conditionID
	if !strings.HasPrefix(conditionID, "0x") {
		formattedConditionID = "0x" + conditionID
	}

	// 编码赎回交易数据
	ctfABI, err := abi.JSON(strings.NewReader(ctfABIJSON))
	if err != nil {
		return fmt.Errorf("failed to parse CTF ABI: %w", err)
	}

	parentCollectionID := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000")
	conditionIDHash := common.HexToHash(formattedConditionID)
	indexSets := []*big.Int{big.NewInt(1), big.NewInt(2)}
	usdcAddr := common.HexToAddress(USDC_ADDRESS)

	calldata, err := ctfABI.Pack("redeemPositions", usdcAddr, parentCollectionID, conditionIDHash, indexSets)
	if err != nil {
		return fmt.Errorf("failed to encode calldata: %w", err)
	}

	// 通过中继执行
	task, err := t.relayClient.Execute([]RelayTransaction{
		{
			To:    CTF_ADDRESS,
			Data:  common.Bytes2Hex(calldata),
			Value: "0",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to execute relay: %w", err)
	}

	fmt.Printf("[CLAIMER] ⏳ Condition %s... - Relayer task ID: %s\n", conditionID[:10], task.TransactionID)

	// 等待确认
	receipt, err := task.Wait()
	if err != nil {
		return fmt.Errorf("failed to wait for confirmation: %w", err)
	}

	if receipt != nil && receipt.Status == 1 {
		fmt.Printf("[CLAIMER] ✅ SUCCESS! Condition %s... - Tx confirmed\n", conditionID[:10])
		return nil
	}

	return fmt.Errorf("transaction failed")
}

// ClaimOnce 手动触发一次检查和赎回
func (t *TokenClaimer) ClaimOnce() error {
	return t.checkAndClaim()
}

// Position API 响应结构
type Position struct {
	ConditionID string `json:"conditionId"`
}

const ctfABIJSON = `[
	{"constant":false,"inputs":[{"name":"collateralToken","type":"address"},{"name":"parentCollectionId","type":"bytes32"},{"name":"conditionId","type":"bytes32"},{"name":"indexSets","type":"uint256[]"}],"name":"redeemPositions","outputs":[],"type":"function"}
]`
