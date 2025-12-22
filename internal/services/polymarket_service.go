package services

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type PolymarketService struct {
	BaseURL string
	Client  *http.Client
}

type GammaEvent struct {
	Markets []GammaMarket `json:"markets"`
}

type GammaMarket struct {
	ClobTokenIds interface{} `json:"clobTokenIds"` // Can be string or []string
	Tokens       []Token     `json:"tokens"`
}

type Token struct {
	Outcome string `json:"outcome"`
	TokenID string `json:"token_id"`
}

func NewPolymarketService() *PolymarketService {
	return &PolymarketService{
		BaseURL: "https://gamma-api.polymarket.com",
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetMarketTokenIds returns [YES_token, NO_token]
func (s *PolymarketService) GetMarketTokenIds(slug string) ([]string, error) {
	log.Printf("[API] 🔍 Fetching Tokens for Slug: %s", slug)

	url := fmt.Sprintf("%s/events?slug=%s", s.BaseURL, slug)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var events []GammaEvent
	if err := json.Unmarshal(body, &events); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	if len(events) == 0 {
		log.Printf("[API] ⚠️ Event not found. Market might be too far in future.")
		return nil, nil // Return empty, not error, to match TS behavior
	}

	event := events[0]
	if len(event.Markets) == 0 {
		log.Printf("[API] ⚠️ Event found but no markets inside.")
		return nil, nil
	}

	market := event.Markets[0]
	var clobTokenIds []string

	// Handle different formats of ClobTokenIds
	switch v := market.ClobTokenIds.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &clobTokenIds); err != nil {
			log.Printf("[API] ❌ Failed to parse clobTokenIds string: %v", err)
		}
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				clobTokenIds = append(clobTokenIds, str)
			}
		}
	}

	// If failed to get from ClobTokenIds, try Tokens array
	if len(clobTokenIds) < 2 {
		if len(market.Tokens) > 0 {
			var yesToken, noToken string
			for _, t := range market.Tokens {
				outcome := strings.ToLower(t.Outcome)
				if outcome == "yes" {
					yesToken = t.TokenID
				} else if outcome == "no" {
					noToken = t.TokenID
				}
			}
			if yesToken != "" && noToken != "" {
				log.Printf("[API] ✅ Found Tokens via tokens array")
				return []string{yesToken, noToken}, nil
			}
		}
		log.Printf("[API] ❌ No valid token format found")
		return nil, nil
	}

	log.Printf("[API] ✅ Found Tokens: YES %s... | NO %s...", 
		clobTokenIds[0][:6], clobTokenIds[1][:6])
	
	return []string{clobTokenIds[0], clobTokenIds[1]}, nil
}
