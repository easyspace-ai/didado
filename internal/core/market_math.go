package core

import (
	"fmt"
	"math"
	"time"
)

const IntervalSeconds = 900 // 15 minutes

type MarketInfo struct {
	Slug        string
	StartTimeMs int64
	EndTimeMs   int64
}

type MarketMath struct{}

func NewMarketMath() *MarketMath {
	return &MarketMath{}
}

// GetTargetMarketSlug returns the currently active market
func (m *MarketMath) GetTargetMarketSlug() MarketInfo {
	now := time.Now()
	nowSec := now.Unix()

	// Calculate the start of the current 15-minute interval
	currentStartSec := (nowSec / IntervalSeconds) * IntervalSeconds

	return MarketInfo{
		Slug:        fmt.Sprintf("btc-updown-15m-%d", currentStartSec),
		StartTimeMs: currentStartSec * 1000,
		EndTimeMs:   (currentStartSec + IntervalSeconds) * 1000,
	}
}

// GetNextMarketSlug returns the next market (the one that will start after the current one ends)
func (m *MarketMath) GetNextMarketSlug() MarketInfo {
	now := time.Now()
	nowMs := now.UnixMilli()
	
	msPer15Min := int64(15 * 60 * 1000)
	
	// Calculate current interval start in ms
	currentIntervalStart := (nowMs / msPer15Min) * msPer15Min
	
	// Next market starts when current interval ends
	nextMarketStartTime := currentIntervalStart + msPer15Min
	
	epochSeconds := nextMarketStartTime / 1000
	
	return MarketInfo{
		Slug:        fmt.Sprintf("btc-updown-15m-%d", epochSeconds),
		StartTimeMs: nextMarketStartTime,
		EndTimeMs:   nextMarketStartTime + msPer15Min,
	}
}

// GetMarketAfterNext returns the market after the next one
func (m *MarketMath) GetMarketAfterNext() MarketInfo {
	now := time.Now()
	nowSec := float64(now.Unix())
	
	// Calculate next market start using ceil
	// Math.ceil(nowSec / 900) * 900
	nextStartSec := int64(math.Ceil(nowSec/float64(IntervalSeconds))) * IntervalSeconds
	
	// Market after next starts one interval after next market
	afterNextStartSec := nextStartSec + IntervalSeconds
	
	return MarketInfo{
		Slug:        fmt.Sprintf("btc-updown-15m-%d", afterNextStartSec),
		StartTimeMs: afterNextStartSec * 1000,
		EndTimeMs:   (afterNextStartSec + IntervalSeconds) * 1000,
	}
}

// GetMsUntil returns milliseconds until a given timestamp
func (m *MarketMath) GetMsUntil(timestampMs int64) int64 {
	diff := timestampMs - time.Now().UnixMilli()
	if diff < 0 {
		return 0
	}
	return diff
}
