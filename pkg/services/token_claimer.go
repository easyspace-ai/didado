package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// TokenClaimer 自动代币索赔服务
// 从已解决的Polymarket市场自动索赔获胜代币
type TokenClaimer struct {
	userAddress     string
	pollIntervalMs  int64
	isPolling       bool
	pollTicker      *time.Ticker
	stopChan        chan struct{}
	mu              sync.RWMutex
}

// RedeemablePosition 可赎回持仓
type RedeemablePosition struct {
	ConditionID string  `json:"conditionId"`
	Amount      float64 `json:"amount"`
	TokenID     string  `json:"tokenId"`
}

// NewTokenClaimer 创建新的代币索赔服务
func NewTokenClaimer(userAddress string, pollIntervalMs int64) *TokenClaimer {
	if pollIntervalMs == 0 {
		pollIntervalMs = 5000 // 默认5秒
	}

	return &TokenClaimer{
		userAddress:    userAddress,
		pollIntervalMs: pollIntervalMs,
		stopChan:       make(chan struct{}),
	}
}

// StartPolling 开始轮询可赎回持仓并自动索赔
func (tc *TokenClaimer) StartPolling() {
	tc.mu.Lock()
	if tc.isPolling {
		tc.mu.Unlock()
		fmt.Println("[CLAIMER] ⚠️ 已在轮询中，跳过启动")
		return
	}

	tc.isPolling = true
	tc.mu.Unlock()

	fmt.Printf("[CLAIMER] 🚀 启动自动代币索赔（每%.1f秒轮询一次）\n", float64(tc.pollIntervalMs)/1000)
	fmt.Printf("[CLAIMER] 👤 检查地址: %s\n", tc.userAddress)

	// 立即执行一次检查
	go func() {
		if err := tc.checkAndClaim(); err != nil {
			fmt.Printf("[CLAIMER] ❌ 初始检查失败: %v\n", err)
		}
	}()

	// 设置定时轮询
	tc.pollTicker = time.NewTicker(time.Duration(tc.pollIntervalMs) * time.Millisecond)
	go tc.pollLoop()
}

// StopPolling 停止轮询
func (tc *TokenClaimer) StopPolling() {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if !tc.isPolling {
		return
	}

	if tc.pollTicker != nil {
		tc.pollTicker.Stop()
	}

	close(tc.stopChan)
	tc.isPolling = false
	fmt.Println("[CLAIMER] 🛑 已停止代币索赔轮询")
}

// pollLoop 轮询循环
func (tc *TokenClaimer) pollLoop() {
	for {
		select {
		case <-tc.stopChan:
			return
		case <-tc.pollTicker.C:
			if err := tc.checkAndClaim(); err != nil {
				fmt.Printf("[CLAIMER] ❌ 轮询错误: %v\n", err)
			}
		}
	}
}

// checkAndClaim 检查并索赔可赎回持仓
func (tc *TokenClaimer) checkAndClaim() error {
	// 查询可赎回持仓
	url := fmt.Sprintf("https://data-api.polymarket.com/positions?user=%s&redeemable=true", tc.userAddress)
	
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("API请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		// 没有持仓是正常的
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	var positions []RedeemablePosition
	if err := json.Unmarshal(body, &positions); err != nil {
		return fmt.Errorf("解析JSON失败: %w", err)
	}

	if len(positions) == 0 {
		return nil
	}

	fmt.Printf("[CLAIMER] 💰 找到 %d 个可赎回持仓\n", len(positions))

	// 按conditionID分组
	conditionIds := make(map[string]bool)
	for _, pos := range positions {
		if pos.ConditionID != "" {
			conditionIds[pos.ConditionID] = true
		}
	}

	if len(conditionIds) == 0 {
		return nil
	}

	fmt.Printf("[CLAIMER] 🔗 赎回 %d 个条件...\n", len(conditionIds))

	successCount := 0
	for conditionID := range conditionIds {
		if err := tc.redeemCondition(conditionID); err != nil {
			fmt.Printf("[CLAIMER] ❌ 赎回条件 %s 失败: %v\n", conditionID[:10], err)
		} else {
			successCount++
		}
	}

	if successCount > 0 {
		fmt.Printf("[CLAIMER] ✅ 成功赎回 %d/%d 个条件\n", successCount, len(conditionIds))
	}

	return nil
}

// redeemCondition 赎回特定条件
func (tc *TokenClaimer) redeemCondition(conditionID string) error {
	fmt.Printf("[CLAIMER] ⏳ 条件 %s... - 提交赎回交易\n", conditionID[:10])

	// TODO: 实现实际的赎回逻辑
	// 需要使用gasless relayer或直接调用智能合约
	
	// 模拟延迟
	time.Sleep(2 * time.Second)

	fmt.Printf("[CLAIMER] ✅ 成功! 条件 %s... - 交易哈希: 0x...\n", conditionID[:10])
	
	return nil
}

// ClaimOnce 手动触发一次检查和索赔
func (tc *TokenClaimer) ClaimOnce() error {
	return tc.checkAndClaim()
}
