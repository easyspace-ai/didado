# Polymarket Trading Bot（Go 复刻版）

这是对上游 TypeScript 项目 `langlangsatrio/polymarketbot` 的 **Go 语言 1:1 功能复刻**（基础设施层 + 策略层示例），目标行为与模块划分对齐：

- **执行层**：纸上交易（Paper）/ 实盘交易（Live），下单/撤单/查单/查仓位/余额
- **实时数据**：市场 WebSocket（公开价格）+ 用户 WebSocket（私有成交回报，HMAC 头鉴权）
- **市场服务**：Gamma API 市场发现与 tokenId 获取
- **仓位管理**：周期性同步、断线恢复、0 仓位误报保护
- **Token Redemption**：Data API 扫描可赎回仓位 + Relayer v2 Gasless 赎回
- **日志/监控**：控制台 War Room Dashboard + 文件日志 + CSV 交易报表
- **策略**：`SniperLadder`、`SimpleTrap`（示例，可扩展）

## 运行

1) 安装 Go（建议 1.22+）

2) 配置 `.env`（参考上游项目同名变量）：

```env
PRIVATE_KEY=your_private_key_here
POLYMARKET_PROXY_ADDRESS=0xYourProxyAddress

POLYMARKET_API_KEY=your_api_key
POLYMARKET_API_SECRET=your_api_secret_base64
POLYMARKET_PASSPHRASE=your_passphrase

LIVE_TRADING=false
```

3) 启动：

```bash
go run ./cmd/polymarketbot
```

选择策略：

```bash
go run ./cmd/polymarketbot -strategy sniperladder
go run ./cmd/polymarketbot -strategy simpletrap
```

