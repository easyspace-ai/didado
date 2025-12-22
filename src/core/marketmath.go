package core

import (
	"strconv"
	"time"
)

// MarketMath 市场数学工具类
// 提供比特币 15 分钟市场信息的计算功能
type MarketMath struct{}

// MarketInfo 市场信息
type MarketInfo struct {
	Slug        string    // 市场 slug (例如: "btc-updown-15m-1765895400")
	StartTimeMs int64     // 市场开始时间 (毫秒)
	EndTimeMs   int64     // 市场结束时间 (毫秒)
	StartTime   time.Time // 开始时间
	EndTime     time.Time // 结束时间
}

const (
	// INTERVAL_SECONDS 15 分钟间隔（秒）
	INTERVAL_SECONDS = 900 // 15 分钟
)

// GetTargetMarketSlug 获取当前活跃的市场
// 返回当前正在运行的市场
func (m *MarketMath) GetTargetMarketSlug() MarketInfo {
	nowSec := time.Now().Unix()

	// 计算当前 15 分钟间隔的开始时间
	currentStartSec := (nowSec / INTERVAL_SECONDS) * INTERVAL_SECONDS

	startTime := time.Unix(currentStartSec, 0)
	endTime := startTime.Add(INTERVAL_SECONDS * time.Second)

	return MarketInfo{
		Slug:        "btc-updown-15m-" + strconv.FormatInt(currentStartSec, 10),
		StartTimeMs: int64(currentStartSec * 1000),
		EndTimeMs:   int64((currentStartSec + INTERVAL_SECONDS) * 1000),
		StartTime:   startTime,
		EndTime:     endTime,
	}
}

// GetNextMarketSlug 获取下一个市场
// 返回当前市场结束后将开始的下一个市场
func (m *MarketMath) GetNextMarketSlug() MarketInfo {
	now := time.Now()

	// 计算 15 分钟间隔边界
	msPer15Min := 15 * 60 * 1000
	currentIntervalStart := (now.UnixMilli() / int64(msPer15Min)) * int64(msPer15Min)

	// 下一个市场开始时间
	nextMarketStartTime := currentIntervalStart + int64(msPer15Min)

	// 转换为 epoch 秒
	epochSeconds := nextMarketStartTime / 1000

	startTime := time.Unix(epochSeconds, 0)
	endTime := startTime.Add(INTERVAL_SECONDS * time.Second)

	return MarketInfo{
		Slug:        "btc-updown-15m-" + strconv.FormatInt(epochSeconds, 10),
		StartTimeMs: nextMarketStartTime,
		EndTimeMs:   nextMarketStartTime + int64(msPer15Min),
		StartTime:   startTime,
		EndTime:     endTime,
	}
}

// GetMsUntil 计算到指定时间戳的毫秒数
func (m *MarketMath) GetMsUntil(timestampMs int64) int64 {
	nowMs := time.Now().UnixMilli()
	diff := timestampMs - nowMs
	if diff < 0 {
		return 0
	}
	return diff
}

// GetMarketAfterNext 获取下下个市场
// 用于在市场开始前预先下单
func (m *MarketMath) GetMarketAfterNext() MarketInfo {
	nowSec := time.Now().Unix()

	// 使用向上取整计算下一个市场开始时间
	nextStartSec := ((nowSec + INTERVAL_SECONDS - 1) / INTERVAL_SECONDS) * INTERVAL_SECONDS

	// 下下个市场开始时间
	afterNextStartSec := nextStartSec + INTERVAL_SECONDS

	startTime := time.Unix(afterNextStartSec, 0)
	endTime := startTime.Add(INTERVAL_SECONDS * time.Second)

	return MarketInfo{
		Slug:        "btc-updown-15m-" + strconv.FormatInt(afterNextStartSec, 10),
		StartTimeMs: afterNextStartSec * 1000,
		EndTimeMs:   (afterNextStartSec + INTERVAL_SECONDS) * 1000,
		StartTime:   startTime,
		EndTime:     endTime,
	}
}
