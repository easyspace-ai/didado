package utils

import (
	"context"
	"fmt"
	"math"
	"time"
)

// RetryConfig 重试配置
type RetryConfig struct {
	MaxAttempts int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
}

// DefaultRetryConfig 默认重试配置
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     10 * time.Second,
		Multiplier:   2.0,
	}
}

// RetryFunc 重试函数类型
type RetryFunc func() error

// Retry 执行重试逻辑
func Retry(ctx context.Context, config RetryConfig, fn RetryFunc) error {
	var lastErr error
	delay := config.InitialDelay

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 执行函数
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// 检查错误是否可重试
		if appErr, ok := err.(*AppError); ok && !appErr.IsRetryable() {
			return err
		}

		// 如果不是最后一次尝试，等待后重试
		if attempt < config.MaxAttempts-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				// 指数退避
				delay = time.Duration(float64(delay) * config.Multiplier)
				if delay > config.MaxDelay {
					delay = config.MaxDelay
				}
			}
		}
	}

	return fmt.Errorf("retry failed after %d attempts: %w", config.MaxAttempts, lastErr)
}

// RetryWithBackoff 带指数退避的重试
func RetryWithBackoff(ctx context.Context, maxAttempts int, fn RetryFunc) error {
	config := DefaultRetryConfig()
	config.MaxAttempts = maxAttempts
	return Retry(ctx, config, fn)
}
