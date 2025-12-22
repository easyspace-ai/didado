# CLOB 客户端实现

这是 Polymarket CLOB (Central Limit Order Book) API 的 Go 语言客户端实现，完全复刻了官方 TypeScript 版本的功能。

## 功能特性

- ✅ **API 密钥派生**: 通过钱包签名自动派生 API 凭证
- ✅ **订单管理**: 创建、提交、取消订单
- ✅ **订单查询**: 获取订单状态和成交信息
- ✅ **批量操作**: 取消所有订单
- ✅ **认证支持**: HMAC-SHA256 签名认证
- ✅ **代理钱包支持**: 支持 EOA 和 Proxy 钱包类型

## 主要方法

### DeriveApiKey()
通过钱包签名消息来获取 API 凭证。这是首次使用或未设置 API 凭证时的必需步骤。

```go
creds, err := client.DeriveApiKey()
```

### CreateOrder()
创建订单对象（不提交到交易所）。

```go
order, err := client.CreateOrder(clob.OrderRequest{
    TokenID:    "0x...",
    Price:      0.45,
    Side:       clob.SideBUY,
    Size:       10.0,
    FeeRateBps: 0,
})
```

### PostOrder()
将订单提交到 Polymarket 交易所。

```go
response, err := client.PostOrder(order, clob.OrderTypeGTC)
```

### CancelOrder()
取消指定的订单。

```go
err := client.CancelOrder(orderID)
```

### CancelAll()
取消所有开放订单。

```go
err := client.CancelAll()
```

### GetOrder()
获取订单的详细信息和状态。

```go
orderInfo, err := client.GetOrder(orderID)
```

## 使用示例

```go
import (
    "polymarketbot-go/src/clob"
    "github.com/ethereum/go-ethereum/accounts/abi/bind"
    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/crypto"
)

// 1. 创建客户端
privateKey, _ := crypto.HexToECDSA("your_private_key")
client := clob.NewClobClientWithPrivateKey(
    "https://clob.polymarket.com",
    137, // Polygon Chain ID
    privateKey,
    nil, // creds (可选，可通过 DeriveApiKey 获取)
    0,   // signatureType: 0 = EOA, 1/2 = Proxy
    common.HexToAddress("0x..."), // funderAddress
)

// 2. 派生 API 密钥（如果未设置）
if client.GetCredentials() == nil {
    creds, err := client.DeriveApiKey()
    if err != nil {
        // 处理错误
    }
    client.SetCredentials(creds)
}

// 3. 创建并提交订单
order, _ := client.CreateOrder(clob.OrderRequest{
    TokenID: "0x...",
    Price:   0.45,
    Side:    clob.SideBUY,
    Size:    10.0,
})

response, _ := client.PostOrder(order, clob.OrderTypeGTC)
fmt.Printf("Order ID: %s\n", response.OrderID)
```

## 订单类型

- `OrderTypeGTC`: Good Till Cancel - 订单保持有效直到成交或取消
- `OrderTypeFOK`: Fill-Or-Kill - 必须完全成交，否则取消
- `OrderTypeGTD`: Good Till Date - 订单在指定日期前有效
- `OrderTypeFAK`: Fill And Kill - 尽可能成交，剩余部分取消

## 订单方向

- `SideBUY`: 买入
- `SideSELL`: 卖出

## 认证

客户端使用 HMAC-SHA256 签名进行 API 认证。认证头包括：
- `POLY-API-KEY`: API 密钥
- `POLY-PASSPHRASE`: 密码短语
- `POLY-TIMESTAMP`: 时间戳
- `POLY-SIGNATURE`: HMAC-SHA256 签名

## 与 execution 包的集成

CLOB 客户端通过 `execution_adapter.go` 中的适配器方法与 `execution` 包集成，避免了循环导入问题。

适配器方法：
- `CreateOrderForExecution()`: 适配 execution.OrderRequest
- `PostOrderForExecution()`: 适配 execution.Order
- `GetOrderForExecution()`: 适配 execution.OrderInfo

## 注意事项

1. **API 端点**: 默认使用 `https://clob.polymarket.com`，可在创建客户端时自定义
2. **链 ID**: Polygon 主网使用链 ID 137
3. **签名类型**: 
   - 0 = EOA (直接钱包，如 MetaMask)
   - 1/2 = Proxy (Polymarket Web UI 钱包)
4. **错误处理**: 所有方法都返回错误，应妥善处理
5. **超时**: HTTP 客户端默认超时为 30 秒

## 实现细节

- 使用标准的 `net/http` 包进行 HTTP 请求
- 使用 `crypto/hmac` 和 `crypto/sha256` 进行签名
- 使用 `encoding/json` 进行 JSON 序列化/反序列化
- 完全复刻了 TypeScript 版本的 API 调用逻辑
