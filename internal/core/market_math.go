package core

import (
	"fmt"
	"time"
)

// MarketMath mirrors upstream TS behavior for BTC 15m markets.
type MarketMath struct{}

const intervalSeconds = 900

type MarketInfo struct {
	Slug        string
	StartTime   time.Time
	EndTime     time.Time
	StartTimeMs int64
	EndTimeMs   int64
}

func GetTargetMarketSlug() MarketInfo {
	now := time.Now()
	nowSec := now.Unix()
	currentStartSec := (nowSec / intervalSeconds) * intervalSeconds

	start := time.Unix(currentStartSec, 0)
	end := time.Unix(currentStartSec+intervalSeconds, 0)
	return MarketInfo{
		Slug:        "btc-updown-15m-" + itoa(currentStartSec),
		StartTime:   start,
		EndTime:     end,
		StartTimeMs: start.UnixMilli(),
		EndTimeMs:   end.UnixMilli(),
	}
}

func GetNextMarketSlug() MarketInfo {
	now := time.Now()
	msPer15Min := int64(15 * 60 * 1000)
	currentStart := (now.UnixMilli() / msPer15Min) * msPer15Min
	nextStartMs := currentStart + msPer15Min
	endMs := nextStartMs + msPer15Min

	start := time.UnixMilli(nextStartMs)
	end := time.UnixMilli(endMs)
	return MarketInfo{
		Slug:        "btc-updown-15m-" + itoa(nextStartMs/1000),
		StartTime:   start,
		EndTime:     end,
		StartTimeMs: nextStartMs,
		EndTimeMs:   endMs,
	}
}

func GetMarketAfterNext() MarketInfo {
	nowSec := time.Now().Unix()
	nextStartSec := ((nowSec + intervalSeconds - 1) / intervalSeconds) * intervalSeconds
	afterNextStartSec := nextStartSec + intervalSeconds

	start := time.Unix(afterNextStartSec, 0)
	end := time.Unix(afterNextStartSec+intervalSeconds, 0)
	return MarketInfo{
		Slug:        "btc-updown-15m-" + itoa(afterNextStartSec),
		StartTime:   start,
		EndTime:     end,
		StartTimeMs: start.UnixMilli(),
		EndTimeMs:   end.UnixMilli(),
	}
}

func GetMsUntil(timestampMs int64) int64 {
	now := time.Now().UnixMilli()
	if timestampMs <= now {
		return 0
	}
	return timestampMs - now
}

func itoa(i int64) string {
	// micro-optimizations are irrelevant; keep minimal deps
	return fmt.Sprintf("%d", i)
}
