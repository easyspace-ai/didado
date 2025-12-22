package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// PolymarketService Polymarket REST API 服务
// 处理所有对 Polymarket Gamma API 的 REST API 调用
type PolymarketService struct {
	baseURL string
	client  *http.Client
}

// NewPolymarketService 创建新的 PolymarketService
func NewPolymarketService() *PolymarketService {
	return &PolymarketService{
		baseURL: "https://gamma-api.polymarket.com",
		client:  &http.Client{},
	}
}

// GetMarketTokenIds 获取市场的 token ID
// 每个 Polymarket 市场有两个 token:
// - YES token: 代表 "Yes" 结果
// - NO token: 代表 "No" 结果
func (p *PolymarketService) GetMarketTokenIds(slug string) ([]string, error) {
	fmt.Printf("[API] 🔍 Fetching Tokens for Slug: %s\n", slug)

	url := fmt.Sprintf("%s/events?slug=%s", p.baseURL, slug)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 添加 User-Agent 防止 403 阻止
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := p.client.Do(req)
	if err != nil {
		fmt.Printf("[API] ❌ API Request Failed: %v\n", err)
		return []string{}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[API] ⚠️ Event not found. Market might be too far in future.\n")
		return []string{}, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var events []Event
	if err := json.Unmarshal(body, &events); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if len(events) == 0 {
		fmt.Printf("[API] ⚠️ Event not found. Market might be too far in future.\n")
		return []string{}, nil
	}

	event := events[0]
	if len(event.Markets) == 0 {
		fmt.Printf("[API] ⚠️ Event found but no markets inside.\n")
		return []string{}, nil
	}

	market := event.Markets[0]

	// 解析 token IDs
	var clobTokenIds []string

	// 格式 1: 字符串 JSON
	if str, ok := market.ClobTokenIds.(string); ok {
		if err := json.Unmarshal([]byte(str), &clobTokenIds); err != nil {
			fmt.Printf("[API] ❌ Failed to parse clobTokenIds: %v\n", err)
			return []string{}, nil
		}
	} else if arr, ok := market.ClobTokenIds.([]interface{}); ok {
		// 格式 2: 已经是数组
		clobTokenIds = make([]string, len(arr))
		for i, v := range arr {
			if str, ok := v.(string); ok {
				clobTokenIds[i] = str
			}
		}
	} else {
		// 格式 3: 回退到 tokens 数组
		if len(market.Tokens) > 0 {
			var yesToken, noToken string
			for _, token := range market.Tokens {
				outcome := strings.ToUpper(token.Outcome)
				if outcome == "YES" {
					yesToken = token.TokenID
				} else if outcome == "NO" {
					noToken = token.TokenID
				}
			}

			if yesToken != "" && noToken != "" {
				fmt.Printf("[API] ✅ Found Tokens via tokens array: YES %s... | NO %s...\n",
					yesToken[:6], noToken[:6])
				return []string{yesToken, noToken}, nil
			}
		}

		fmt.Printf("[API] ❌ No valid token format found in market\n")
		return []string{}, nil
	}

	if len(clobTokenIds) < 2 {
		fmt.Printf("[API] ❌ clobTokenIds has less than 2 tokens\n")
		return []string{}, nil
	}

	yesTokenID := clobTokenIds[0]
	noTokenID := clobTokenIds[1]

	fmt.Printf("[API] ✅ Found Tokens: YES %s... | NO %s...\n",
		yesTokenID[:6], noTokenID[:6])

	return []string{yesTokenID, noTokenID}, nil
}

// Event API 响应结构
type Event struct {
	Markets []Market `json:"markets"`
}

type Market struct {
	ClobTokenIds interface{} `json:"clobTokenIds"`
	Tokens       []Token     `json:"tokens"`
}

type Token struct {
	Outcome string `json:"outcome"`
	TokenID string `json:"token_id"`
}
