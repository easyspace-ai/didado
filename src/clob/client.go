package clob

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	// DefaultBaseURL Polymarket CLOB API 基础 URL
	DefaultBaseURL = "https://clob.polymarket.com"
	// PolygonChainID Polygon 主网链 ID
	PolygonChainID = 137
)

// Side 订单方向
type Side string

const (
	SideBUY  Side = "BUY"
	SideSELL Side = "SELL"
)

// OrderType 订单类型
type OrderType string

const (
	OrderTypeGTC OrderType = "GTC" // Good Till Cancel
	OrderTypeFOK OrderType = "FOK" // Fill-Or-Kill
	OrderTypeGTD OrderType = "GTD" // Good Till Date
	OrderTypeFAK OrderType = "FAK" // Fill And Kill
)

// Credentials API 凭证
type Credentials struct {
	Key       string
	Secret    string
	Passphrase string
}

// ClobClient CLOB 客户端
type ClobClient struct {
	baseURL       string
	chainID       int64
	signer        *bind.TransactOpts
	privateKey    *ecdsa.PrivateKey
	creds         *Credentials
	signatureType int
	funderAddress common.Address
	httpClient    *http.Client
}

// NewClobClient 创建新的 CLOB 客户端
func NewClobClient(
	baseURL string,
	chainID int64,
	signer *bind.TransactOpts,
	creds *Credentials,
	signatureType int,
	funderAddress common.Address,
) *ClobClient {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	// 从 signer 中提取私钥
	var privateKey *ecdsa.PrivateKey
	if signer != nil {
		// TransactOpts 没有直接暴露私钥，我们需要通过其他方式获取
		// 这里假设私钥已经通过其他方式传入
		// 在实际使用中，可能需要修改接口来直接传递私钥
	}

	return &ClobClient{
		baseURL:       baseURL,
		chainID:       chainID,
		signer:        signer,
		privateKey:    privateKey,
		creds:         creds,
		signatureType: signatureType,
		funderAddress: funderAddress,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewClobClientWithPrivateKey 使用私钥创建 CLOB 客户端
func NewClobClientWithPrivateKey(
	baseURL string,
	chainID int64,
	privateKey *ecdsa.PrivateKey,
	creds *Credentials,
	signatureType int,
	funderAddress common.Address,
) *ClobClient {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	chainIDBig := big.NewInt(chainID)
	signer, _ := bind.NewKeyedTransactorWithChainID(privateKey, chainIDBig)

	return &ClobClient{
		baseURL:       baseURL,
		chainID:       chainID,
		signer:        signer,
		privateKey:    privateKey,
		creds:         creds,
		signatureType: signatureType,
		funderAddress: funderAddress,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// DeriveApiKey 派生 API 密钥
// 通过钱包签名消息来获取 API 凭证
// 注意：这需要实际的 Polymarket API 端点，可能需要根据实际 API 文档调整
func (c *ClobClient) DeriveApiKey() (*Credentials, error) {
	// 构建签名消息
	// Polymarket 使用特定的消息格式进行签名
	timestamp := time.Now().Unix()
	message := fmt.Sprintf("polymarket_derive_api_key_%d", timestamp)

	if c.privateKey == nil {
		return nil, fmt.Errorf("private key not set")
	}

	// 使用钱包签名消息
	hash := crypto.Keccak256Hash([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)))
	signature, err := crypto.Sign(hash.Bytes(), c.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign message: %w", err)
	}

	// 调整签名格式 (v = 27 或 28)
	if signature[64] < 27 {
		signature[64] += 27
	}

	// 构建请求
	reqBody := map[string]interface{}{
		"signature":     hex.EncodeToString(signature),
		"message":       message,
		"address":       c.funderAddress.Hex(),
		"signatureType": c.signatureType,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/auth", c.baseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Key        string `json:"apiKey"`
		Secret     string `json:"secret"`
		Passphrase string `json:"passphrase"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	creds := &Credentials{
		Key:        result.Key,
		Secret:     result.Secret,
		Passphrase: result.Passphrase,
	}

	// 更新客户端凭证
	c.creds = creds

	return creds, nil
}

// CreateOrder 创建订单
func (c *ClobClient) CreateOrder(req OrderRequest) (*Order, error) {
	// 构建订单对象
	order := &Order{
		TokenID:  req.TokenID,
		Price:    req.Price,
		Side:     req.Side,
		Size:     req.Size,
		FeeRateBps: req.FeeRateBps,
	}

	return order, nil
}

// OrderRequest 订单请求
type OrderRequest struct {
	TokenID    string
	Price      float64
	Side       Side
	Size       float64
	FeeRateBps int
}

// Order 订单
type Order struct {
	TokenID    string  `json:"token_id"`
	Price      float64 `json:"price"`
	Side       Side    `json:"side"`
	Size       float64 `json:"size"`
	FeeRateBps int     `json:"fee_rate_bps"`
}

// PostOrder 提交订单
func (c *ClobClient) PostOrder(order *Order, orderType OrderType) (*OrderResponse, error) {
	// 构建订单数据
	orderData := map[string]interface{}{
		"token_id":    order.TokenID,
		"price":       order.Price,
		"side":        string(order.Side),
		"size":        order.Size,
		"fee_rate_bps": order.FeeRateBps,
		"order_type":  string(orderType),
	}

	jsonData, err := json.Marshal(orderData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal order: %w", err)
	}

	url := fmt.Sprintf("%s/orders", c.baseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// 添加认证头
	if c.creds != nil {
		if err := c.addAuthHeaders(req, "POST", "/orders", jsonData); err != nil {
			return nil, fmt.Errorf("failed to add auth headers: %w", err)
		}
	}

	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		var errorResp struct {
			Error    string `json:"error"`
			ErrorMsg string `json:"errorMsg"`
		}
		json.Unmarshal(body, &errorResp)
		
		errorMsg := errorResp.Error
		if errorMsg == "" {
			errorMsg = errorResp.ErrorMsg
		}
		if errorMsg == "" {
			errorMsg = string(body)
		}
		return nil, fmt.Errorf("API error: status %d, message: %s", resp.StatusCode, errorMsg)
	}

	var result OrderResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// OrderResponse 订单响应
type OrderResponse struct {
	OrderID  string `json:"orderID"`
	OrderId  string `json:"orderId"`
	ID       string `json:"id"`
	Success  bool   `json:"success"`
	Error    string `json:"error"`
	ErrorMsg string `json:"errorMsg"`
}

// CancelOrder 取消订单
func (c *ClobClient) CancelOrder(orderID string) error {
	url := fmt.Sprintf("%s/orders/%s", c.baseURL, orderID)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 添加认证头
	if c.creds != nil {
		if err := c.addAuthHeaders(req, "DELETE", fmt.Sprintf("/orders/%s", orderID), nil); err != nil {
			return fmt.Errorf("failed to add auth headers: %w", err)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// CancelAll 取消所有订单
func (c *ClobClient) CancelAll() error {
	url := fmt.Sprintf("%s/orders", c.baseURL)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 添加认证头
	if c.creds != nil {
		if err := c.addAuthHeaders(req, "DELETE", "/orders", nil); err != nil {
			return fmt.Errorf("failed to add auth headers: %w", err)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// GetOrder 获取订单信息
func (c *ClobClient) GetOrder(orderID string) (*OrderInfo, error) {
	url := fmt.Sprintf("%s/orders/%s", c.baseURL, orderID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 添加认证头
	if c.creds != nil {
		if err := c.addAuthHeaders(req, "GET", fmt.Sprintf("/orders/%s", orderID), nil); err != nil {
			return nil, fmt.Errorf("failed to add auth headers: %w", err)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result OrderInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// OrderInfo 订单信息
type OrderInfo struct {
	OrderID         string  `json:"orderID"`
	TokenID         string  `json:"token_id"`
	Price           string  `json:"price"`
	Size            string  `json:"size"`
	SizeMatched     string  `json:"size_matched"`
	SizeMatchedAlt  string  `json:"sizeMatched"`
	Status          string  `json:"status"`
	Canceled        bool    `json:"canceled"`
	Side            string  `json:"side"`
	AssociateTrades []Trade `json:"associate_trades"`
}

// Trade 交易信息
type Trade struct {
	Size  string `json:"size"`
	Price string `json:"price"`
}

// addAuthHeaders 添加认证头 (HMAC-SHA256)
func (c *ClobClient) addAuthHeaders(req *http.Request, method, path string, body []byte) error {
	if c.creds == nil {
		return fmt.Errorf("credentials not set")
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	message := timestamp + method + path

	if body != nil {
		// 计算 body 的哈希
		hash := sha256.Sum256(body)
		message += hex.EncodeToString(hash[:])
	}

	// 计算 HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(c.creds.Secret))
	mac.Write([]byte(message))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	// 设置认证头
	req.Header.Set("POLY-API-KEY", c.creds.Key)
	req.Header.Set("POLY-PASSPHRASE", c.creds.Passphrase)
	req.Header.Set("POLY-TIMESTAMP", timestamp)
	req.Header.Set("POLY-SIGNATURE", signature)

	return nil
}

// GetCredentials 获取当前凭证 (用于 WebSocket 服务)
func (c *ClobClient) GetCredentials() interface{} {
	return c.creds
}

// SetCredentials 设置凭证
func (c *ClobClient) SetCredentials(creds *Credentials) {
	c.creds = creds
}
