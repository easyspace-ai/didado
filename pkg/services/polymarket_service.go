package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// PolymarketService Polymarket REST API服务
// 处理所有REST API调用到Polymarket的Gamma API
type PolymarketService struct {
	baseURL string
	client  *http.Client
}

// NewPolymarketService 创建新的Polymarket服务
func NewPolymarketService() *PolymarketService {
	return &PolymarketService{
		baseURL: "https://gamma-api.polymarket.com",
		client: &http.Client{
			Timeout: 30000, // 30秒超时
		},
	}
}

// MarketToken 市场代币信息
type MarketToken struct {
	Outcome string `json:"outcome"`
	TokenID string `json:"token_id"`
}

// Market 市场信息
type Market struct {
	ClobTokenIds interface{}   `json:"clobTokenIds"`
	Tokens       []MarketToken `json:"tokens"`
}

// Event 事件信息
type Event struct {
	Markets []Market `json:"markets"`
}

// GetMarketTokenIds 获取市场的代币ID
// 返回[YES_token, NO_token]
func (s *PolymarketService) GetMarketTokenIds(slug string) ([]string, error) {
	fmt.Printf("[API] 🔍 获取市场代币，Slug: %s\n", slug)

	url := fmt.Sprintf("%s/events?slug=%s", s.baseURL, slug)
	
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var events []Event
	if err := json.Unmarshal(body, &events); err != nil {
		return nil, fmt.Errorf("解析JSON失败: %w", err)
	}

	if len(events) == 0 {
		fmt.Println("[API] ⚠️ 未找到事件。市场可能还不存在。")
		return []string{}, nil
	}

	event := events[0]
	if len(event.Markets) == 0 {
		fmt.Println("[API] ⚠️ 事件找到但没有市场。")
		return []string{}, nil
	}

	market := event.Markets[0]

	// 尝试从clobTokenIds解析
	var clobTokenIds []string
	
	switch v := market.ClobTokenIds.(type) {
	case string:
		// JSON字符串格式
		if err := json.Unmarshal([]byte(v), &clobTokenIds); err != nil {
			return nil, fmt.Errorf("解析clobTokenIds失败: %w", err)
		}
	case []interface{}:
		// 数组格式
		for _, item := range v {
			if str, ok := item.(string); ok {
				clobTokenIds = append(clobTokenIds, str)
			}
		}
	default:
		// 回退到tokens数组
		if len(market.Tokens) > 0 {
			var yesToken, noToken string
			for _, token := range market.Tokens {
				if token.Outcome == "Yes" || token.Outcome == "yes" || token.Outcome == "YES" {
					yesToken = token.TokenID
				}
				if token.Outcome == "No" || token.Outcome == "no" || token.Outcome == "NO" {
					noToken = token.TokenID
				}
			}
			
			if yesToken != "" && noToken != "" {
				fmt.Printf("[API] ✅ 通过tokens数组找到代币: YES %s... | NO %s...\n",
					yesToken[:6], noToken[:6])
				return []string{yesToken, noToken}, nil
			}
		}
		
		return nil, fmt.Errorf("无法解析代币ID格式")
	}

	if len(clobTokenIds) < 2 {
		return nil, fmt.Errorf("clobTokenIds少于2个代币")
	}

	yesTokenID := clobTokenIds[0]
	noTokenID := clobTokenIds[1]

	fmt.Printf("[API] ✅ 找到代币: YES %s... | NO %s...\n",
		yesTokenID[:6], noTokenID[:6])

	return []string{yesTokenID, noTokenID}, nil
}
