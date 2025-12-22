# Polymarket Sniper Bot (Go Version)

这是 [polymarketbot](https://github.com/langlangsatrio/polymarketbot) 的 Go 语言一比一复刻版本。

## 功能

- **Paper Trading**: 模拟交易模式，使用假资金测试策略。
- **Live Trading**: 实盘交易模式，对接 Polymarket CLOB API 和 Polygon 区块链。
- **Sniper Ladder 策略**: 
  - 自动计算 BTC 15分钟期权市场的时间窗口。
  - 在市场开始前预埋 Trap 单。
  - 监听实时行情，动态调整阶梯单 (Ladder) 和对冲单 (Hedge)。
  - 自动止盈止损。

## 架构

- `cmd/bot`: 程序入口。
- `internal/config`: 配置管理。
- `internal/core`: 核心逻辑（市场时间计算）。
- `internal/services`: 外部服务（Polymarket API, WebSocket）。
- `internal/execution`: 交易执行器（Paper/Live）。
- `internal/strategies`: 策略实现。
- `internal/polymarket`: Polymarket CLOB 客户端（EIP-712 签名骨架）。

## 快速开始

1. **安装 Go 1.22+**

2. **配置环境变量**
   复制 `.env.example` 为 `.env` 并填入配置：
   ```bash
   PRIVATE_KEY=your_private_key_here
   RPC_URL=https://polygon-rpc.com
   LIVE_TRADING=false # 设置为 true 开启实盘
   ```

3. **运行**
   ```bash
   go run cmd/bot/main.go
   ```

## 注意事项

- 实盘交易需要配置私钥和 Polygon RPC。
- EIP-712 签名部分在 `internal/polymarket/clob_client.go` 中仅为骨架，实盘前需要补全具体的 Order 结构体签名逻辑。
- 默认开启 Paper Trading 模式，安全无风险。
