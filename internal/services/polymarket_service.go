package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PolymarketService: Gamma API（公共市场数据）
type PolymarketService struct {
	baseURL string
	http    *http.Client
}

func NewPolymarketService(baseURL string) *PolymarketService {
	if baseURL == "" {
		baseURL = "https://gamma-api.polymarket.com"
	}
	return &PolymarketService{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// GetMarketTokenIDs mirrors TS: returns [YES_tokenId, NO_tokenId].
func (s *PolymarketService) GetMarketTokenIDs(slug string) ([]string, error) {
	DashboardManager.Log(fmt.Sprintf("[API] 🔍 Fetching Tokens for Slug: %s", slug), LogInfo)

	url := s.baseURL + "/events?slug=" + slug
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := s.http.Do(req)
	if err != nil {
		DashboardManager.LogEvent("SNIPER", "❌ API Error: "+err.Error())
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		DashboardManager.LogEvent("SNIPER", fmt.Sprintf("❌ API Error: HTTP %d", resp.StatusCode))
		return nil, fmt.Errorf("gamma api http %d: %s", resp.StatusCode, string(body))
	}

	var events []map[string]any
	if err := json.Unmarshal(body, &events); err != nil {
		DashboardManager.LogEvent("SNIPER", "❌ API: Parse error")
		return nil, err
	}
	if len(events) == 0 {
		DashboardManager.LogEvent("SNIPER", fmt.Sprintf("⚠️ API: Event not found (%s)", slug))
		return []string{}, nil
	}

	event := events[0]
	marketsAny, ok := event["markets"]
	if !ok {
		DashboardManager.LogEvent("SNIPER", "⚠️ API: No markets in event")
		return []string{}, nil
	}
	markets, ok := marketsAny.([]any)
	if !ok || len(markets) == 0 {
		DashboardManager.LogEvent("SNIPER", "⚠️ API: No markets in event")
		return []string{}, nil
	}
	market, _ := markets[0].(map[string]any)
	if market == nil {
		DashboardManager.LogEvent("SNIPER", "❌ API: Token format error")
		return []string{}, nil
	}

	// Format 1: clobTokenIds as JSON string: "[\"0x...\",\"0x...\"]"
	if v, ok := market["clobTokenIds"]; ok {
		switch tv := v.(type) {
		case string:
			var ids []string
			if err := json.Unmarshal([]byte(tv), &ids); err == nil && len(ids) >= 2 {
				return []string{ids[0], ids[1]}, nil
			}
		case []any:
			if len(tv) >= 2 {
				id0, _ := tv[0].(string)
				id1, _ := tv[1].(string)
				if id0 != "" && id1 != "" {
					return []string{id0, id1}, nil
				}
			}
		}
	}

	// Format 3: tokens array: [{outcome:"Yes", token_id:"0x..."}, ...]
	if v, ok := market["tokens"]; ok {
		if arr, ok := v.([]any); ok {
			var yesID, noID string
			for _, it := range arr {
				m, _ := it.(map[string]any)
				if m == nil {
					continue
				}
				outcome, _ := m["outcome"].(string)
				tokenID, _ := m["token_id"].(string)
				switch strings.ToLower(outcome) {
				case "yes":
					yesID = tokenID
				case "no":
					noID = tokenID
				}
			}
			if yesID != "" && noID != "" {
				DashboardManager.LogEvent("SNIPER", "✅ Tokens Acquired")
				return []string{yesID, noID}, nil
			}
		}
	}

	DashboardManager.LogEvent("SNIPER", "❌ API: Token format error")
	return []string{}, nil
}
