package core

import (
	"fmt"
	"math"
	"time"

	"github.com/polymarketbot-go/pkg/types"
)

// MarketMath 市场计算工具
// 提供Bitcoin 15分钟市场的时间计算功能
type MarketMath struct {
	intervalSeconds int64
}

// NewMarketMath 创建新的市场计算工具
func NewMarketMath() *MarketMath {
	return &MarketMath{
		intervalSeconds: 900, // 15分钟 = 900秒
	}
}

// GetTargetMarketSlug 获取当前活跃的市场
// 返回正在运行的市场信息
func (m *MarketMath) GetTargetMarketSlug() types.MarketInfo {
	nowMs := time.Now().UnixMilli()
	nowSec := nowMs / 1000

	// 计算当前15分钟间隔的开始时间
	currentStartSec := (nowSec / m.intervalSeconds) * m.intervalSeconds

	return types.MarketInfo{
		Slug:        fmt.Sprintf("btc-updown-15m-%d", currentStartSec),
		StartTimeMs: currentStartSec * 1000,
		EndTimeMs:   (currentStartSec + m.intervalSeconds) * 1000,
	}
}

// GetNextMarketSlug 获取下一个市场
// 返回当前市场结束后将开始的市场信息
func (m *MarketMath) GetNextMarketSlug() types.MarketInfo {
	now := time.Now()
	msPer15Min := int64(15 * 60 * 1000)

	currentIntervalStart := (now.UnixMilli() / msPer15Min) * msPer15Min
	nextMarketStartTime := currentIntervalStart + msPer15Min
	epochSeconds := nextMarketStartTime / 1000

	slug := fmt.Sprintf("btc-updown-15m-%d", epochSeconds)
	endTimeMs := nextMarketStartTime + msPer15Min

	return types.MarketInfo{
		Slug:        slug,
		StartTimeMs: nextMarketStartTime,
		EndTimeMs:   endTimeMs,
	}
}

// GetMarketAfterNext 获取下下个市场
// 用于提前下单
func (m *MarketMath) GetMarketAfterNext() types.MarketInfo {
	nowMs := time.Now().UnixMilli()
	nowSec := nowMs / 1000

	nextStartSec := int64(math.Ceil(float64(nowSec)/float64(m.intervalSeconds))) * m.intervalSeconds
	afterNextStartSec := nextStartSec + m.intervalSeconds

	return types.MarketInfo{
		Slug:        fmt.Sprintf("btc-updown-15m-%d", afterNextStartSec),
		StartTimeMs: afterNextStartSec * 1000,
		EndTimeMs:   (afterNextStartSec + m.intervalSeconds) * 1000,
	}
}

// GetMsUntil 计算距离给定时间戳的毫秒数
func (m *MarketMath) GetMsUntil(timestampMs int64) int64 {
	diff := timestampMs - time.Now().UnixMilli()
	if diff < 0 {
		return 0
	}
	return diff
}
