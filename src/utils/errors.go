package utils

import (
	"fmt"
)

// ErrorType 错误类型
type ErrorType string

const (
	ErrorTypeAPI        ErrorType = "API_ERROR"
	ErrorTypeNetwork    ErrorType = "NETWORK_ERROR"
	ErrorTypeValidation ErrorType = "VALIDATION_ERROR"
	ErrorTypeAuth       ErrorType = "AUTH_ERROR"
	ErrorTypeBusiness   ErrorType = "BUSINESS_ERROR"
)

// AppError 应用错误
type AppError struct {
	Type    ErrorType
	Code    string
	Message string
	Err     error
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Type, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Type, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Err
}

// NewAPIError 创建 API 错误
func NewAPIError(code string, message string, err error) *AppError {
	return &AppError{
		Type:    ErrorTypeAPI,
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// NewNetworkError 创建网络错误
func NewNetworkError(message string, err error) *AppError {
	return &AppError{
		Type:    ErrorTypeNetwork,
		Message: message,
		Err:     err,
	}
}

// NewValidationError 创建验证错误
func NewValidationError(message string) *AppError {
	return &AppError{
		Type:    ErrorTypeValidation,
		Message: message,
	}
}

// NewAuthError 创建认证错误
func NewAuthError(message string, err error) *AppError {
	return &AppError{
		Type:    ErrorTypeAuth,
		Message: message,
		Err:     err,
	}
}

// NewBusinessError 创建业务错误
func NewBusinessError(message string) *AppError {
	return &AppError{
		Type:    ErrorTypeBusiness,
		Message: message,
	}
}

// IsRetryable 判断错误是否可重试
func (e *AppError) IsRetryable() bool {
	switch e.Type {
	case ErrorTypeNetwork:
		return true
	case ErrorTypeAPI:
		// 某些 API 错误可以重试（如 429, 500, 503）
		return e.Code == "429" || e.Code == "500" || e.Code == "503"
	default:
		return false
	}
}
