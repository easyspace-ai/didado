package utils

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// 以太坊地址正则表达式
	ethAddressRegex = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
)

// ValidateOrderParams 验证订单参数
func ValidateOrderParams(tokenID string, price, size float64, side string) error {
	// 验证 token ID
	if tokenID == "" {
		return NewValidationError("token ID cannot be empty")
	}
	if !ethAddressRegex.MatchString(tokenID) {
		return NewValidationError(fmt.Sprintf("invalid token ID format: %s", tokenID))
	}

	// 验证价格范围（Polymarket 价格在 0-1 之间）
	if price < 0 || price > 1 {
		return NewValidationError(fmt.Sprintf("price must be between 0 and 1, got: %.2f", price))
	}

	// 验证订单大小
	if size <= 0 {
		return NewValidationError(fmt.Sprintf("order size must be positive, got: %.2f", size))
	}

	// 验证订单方向
	sideUpper := strings.ToUpper(side)
	if sideUpper != "BUY" && sideUpper != "SELL" {
		return NewValidationError(fmt.Sprintf("invalid order side: %s (must be BUY or SELL)", side))
	}

	return nil
}

// ValidateAddress 验证以太坊地址
func ValidateAddress(address string) error {
	if address == "" {
		return NewValidationError("address cannot be empty")
	}
	if !ethAddressRegex.MatchString(address) {
		return NewValidationError(fmt.Sprintf("invalid address format: %s", address))
	}
	return nil
}

// ValidatePrivateKey 验证私钥格式
func ValidatePrivateKey(privateKey string) error {
	if privateKey == "" {
		return NewValidationError("private key cannot be empty")
	}

	// 移除 0x 前缀（如果存在）
	key := strings.TrimPrefix(privateKey, "0x")

	// 验证长度（64 个十六进制字符 = 32 字节）
	if len(key) != 64 {
		return NewValidationError(fmt.Sprintf("private key must be 64 hex characters (32 bytes), got: %d", len(key)))
	}

	// 验证是否为有效的十六进制
	hexRegex := regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	if !hexRegex.MatchString(key) {
		return NewValidationError("private key must be valid hexadecimal")
	}

	return nil
}
