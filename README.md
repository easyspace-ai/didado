# Polymarket Trading Bot (Golang版)

这是一个用Golang实现的Polymarket预测市场自动交易机器人框架，从TypeScript版本完整复刻。

## 🎯 策略无关架构

机器人将**基础设施**和**策略逻辑**分离：

- **基础设施层**（机器人提供）：
  - 订单执行（买/卖/取消）
  - WebSocket连接（实时价格和成交数据）
  - REST API客户端（市场数据、持仓）
  - 持仓同步（从断线恢复）
  - 代币赎回（自动索赔）
  - 交易日志（CSV导出）
  - 市场工具（时间计算、市场发现）

- **策略层**（您实现）：
  - 您的交易逻辑
  - 何时买/卖
  - 使用什么价格
  - 风险管理规则
  - 退出条件

机器人预配置了**SniperLadder**策略作为示例，但您可以轻松创建自己的策略或修改现有策略。

## ✨ 机器人功能

这些是**真实的机器人功能** - 无论您使用哪种策略都能工作的基础设施：

### 订单执行
- **纸交易**: 使用虚拟$1,000 USDC模拟交易（无风险）
- **真实交易**: 在Polymarket上使用真实资金进行真实订单
- **订单类型**: GTC（Good Till Cancel）、FOK（Fill-Or-Kill）、GTD（Good Till Date）、FAK（Fill And Kill）
- **订单管理**: 下单、取消、检查状态、全部取消

### 实时数据
- **WebSocket市场数据流**: 任何代币的实时价格更新
- **WebSocket用户数据流**: 实时接收您的订单成交（需要认证）
- **自动重连**: 优雅处理断线
- **连接健康监控**: 检测并从死连接中恢复

### 市场服务
- **市场发现**: 通过slug查找市场，获取代币ID
- **市场数学工具**: 计算市场时间、间隔、等待周期
- **持仓查询**: 从区块链获取您的YES/NO持仓
- **余额检查**: 检查USDC余额（真实交易）

### 持仓管理
- **持仓同步**: 定期将链上持仓与机器人内存同步
- **状态恢复**: 如果机器人在周期中重启，恢复持仓
- **零值保护**: 忽略可能错误显示0持仓的API延迟

### 代币赎回
- **自动索赔**: 每5秒轮询可赎回持仓
- **无Gas交易**: 使用Polymarket的中继器（无gas费用）
- **后台运行**: 在市场周期后持续运行

### 日志和监控
- **CSV交易日志**: 所有交易导出到`trading_*.csv`
- **性能指标**: 盈亏、胜率、ROI、最佳/最差交易
- **仪表板更新**: 控制台实时状态
- **文件日志**: `logs/bot-YYYY-MM-DD.log`中的详细日志

## 📋 前置要求

- **Golang** v1.21+
- **Polymarket账户**，已启用交易
- **钱包**，包含：
  - MATIC用于gas费用（推荐：0.1+ MATIC）
  - USDC用于交易（金额取决于您的策略）

## 📦 安装

### 1. 克隆仓库

```bash
git clone <repository-url>
cd polymarketbot-go
```

### 2. 安装依赖

```bash
go mod download
```

### 3. 创建环境文件

```bash
cp .env.example .env
```

编辑`.env`并填写您的配置：

```env
# 必需: 您的钱包私钥（不带0x前缀）
PRIVATE_KEY=your_private_key_here

# 可选: 如果您使用Polymarket网页界面（代理钱包）
POLYMARKET_PROXY_ADDRESS=0xYourProxyAddress

# 代币索赔所需: API凭证
POLYMARKET_API_KEY=your_api_key
POLYMARKET_API_SECRET=your_api_secret
POLYMARKET_PASSPHRASE=your_passphrase

# 交易模式: "true"为真实交易，"false"或留空为纸交易
LIVE_TRADING=false
```

## 🚀 运行机器人

### 启动机器人

```bash
go run cmd/main.go
```

或编译后运行：

```bash
go build -o polymarketbot cmd/main.go
./polymarketbot
```

### 纸交易（默认）

默认情况下，机器人在**纸交易模式**下运行：
- 使用虚拟$1,000 USDC模拟交易
- 没有真实资金风险
- 完美用于测试策略

### 真实交易

启用真实交易：

1. 在`.env`中设置：
   ```env
   LIVE_TRADING=true
   ```

2. 确保您的钱包有：
   - **MATIC**: 用于gas费用（推荐0.1+）
   - **USDC**: 用于交易（金额取决于策略）

3. 启动机器人：
   ```bash
   go run cmd/main.go
   ```

### 停止机器人

按`Ctrl+C`优雅停止。机器人将：
- 取消所有开放订单
- 关闭WebSocket连接
- 保存交易日志

## 📊 策略

此机器人是**策略无关的** - 您可以通过创建新的策略类来实现任何交易策略。机器人提供所有基础设施（订单执行、WebSocket数据流、持仓管理），您只需实现交易逻辑。

机器人预配置了**SniperLadder**策略作为示例。

## 📁 项目结构

```
polymarketbot-go/
├── cmd/
│   └── main.go                  # 主入口点
├── pkg/
│   ├── types/                   # 类型定义
│   │   └── types.go
│   ├── executor/                # 订单执行层
│   │   ├── interface.go         # 执行器接口
│   │   ├── paper_executor.go    # 纸交易实现
│   │   └── live_executor.go     # 真实交易实现
│   ├── services/                # 核心服务
│   │   ├── polymarket_service.go    # REST API客户端
│   │   ├── websocket_service.go     # WebSocket客户端
│   │   ├── token_claimer.go         # 自动代币赎回
│   │   ├── spreadsheet_logger.go    # 交易日志（CSV）
│   │   └── dashboard_manager.go     # 控制台仪表板
│   ├── core/                    # 核心工具
│   │   └── market_math.go       # 市场时间计算
│   └── strategy/                # 交易策略
│       └── sniper_ladder.go     # SniperLadder策略
├── .env.example                 # 环境变量模板
├── go.mod                       # Go模块文件
└── README.md                    # 本文件
```

## 🔒 安全最佳实践

1. **永远不要提交`.env`文件** - 它已在`.gitignore`中
2. **使用环境变量** - 不要硬编码凭证
3. **从纸交易开始** - 在真实交易前彻底测试
4. **监控您的钱包** - 定期检查余额
5. **设置合理的持仓大小** - 不要冒超出承受能力的风险

## ⚠️ 免责声明

此机器人仅用于教育目的。交易加密货币和预测市场涉及重大风险。过去的表现不保证未来结果。风险自负。

## 📄 许可证

免费，开源

---

**祝交易愉快！🚀**
