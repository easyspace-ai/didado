# Polymarket Trading Bot (Go 版本)

这是一个用 Go 语言实现的 Polymarket 交易机器人框架，完全复刻了原始 TypeScript 版本的所有功能。

## 🎯 策略无关架构

机器人将**基础设施**与**策略逻辑**分离：

- **基础设施层** (机器人提供):
  - 订单执行 (买入/卖出/取消)
  - WebSocket 连接 (实时价格和成交数据)
  - REST API 客户端 (市场数据、持仓)
  - 持仓同步 (从断线恢复)
  - 代币赎回 (自动领取)
  - 交易日志 (CSV 导出)
  - 市场工具 (时间计算、市场发现)

- **策略层** (您实现):
  - 您的交易逻辑
  - 何时买入/卖出
  - 使用什么价格
  - 风险管理规则
  - 退出条件

机器人附带预配置的 **SniperLadder** 策略作为示例，但您可以轻松创建自己的策略或修改现有策略。

## ✨ 机器人功能

这些是**真正的机器人功能** - 无论您使用哪种策略都能工作的基础设施：

### 订单执行

- **模拟交易**: 使用假 $1,000 USDC 模拟交易 (无风险)
- **实盘交易**: Polymarket 上的真实订单 (真实资金)
- **订单类型**: GTC (Good Till Cancel), FOK (Fill-Or-Kill), GTD (Good Till Date), FAK (Fill And Kill)
- **订单管理**: 下单、取消、检查状态、取消全部

### 实时数据

- **WebSocket 市场订阅**: 任何代币的实时价格更新
- **WebSocket 用户订阅**: 您的订单实时成交 (需要认证)
- **自动重连**: 优雅处理断线
- **连接健康监控**: 检测并恢复死连接

### 市场服务

- **市场发现**: 通过 slug 查找市场，获取代币 ID
- **市场数学工具**: 计算市场时间、间隔、等待时间
- **持仓查询**: 从区块链获取您的 YES/NO 持仓
- **余额检查**: 检查 USDC 余额 (实盘交易)

### 持仓管理

- **持仓同步**: 定期从链上同步持仓与机器人内存
- **状态恢复**: 如果机器人在周期中途重启，恢复持仓
- **零保护**: 忽略可能错误显示 0 持仓的 API 延迟

### 代币赎回

- **自动领取**: 每 5 秒轮询一次可赎回持仓
- **无 Gas 交易**: 使用 Polymarket 的中继器 (无需 gas 费用)
- **后台运行**: 在市场周期结束后持续运行

### 日志和监控

- **CSV 交易日志**: 所有交易导出到 `trading_*.csv`
- **性能指标**: PnL、胜率、ROI、最佳/最差交易
- **仪表板更新**: 控制台实时状态
- **文件日志**: 详细日志在 `logs/bot-YYYY-MM-DD.log`

### 开发者体验

- **Go**: 类型安全
- **模块化设计**: 易于扩展和修改
- **错误处理**: 优雅的错误恢复
- **优雅关闭**: 取消订单并干净地关闭连接

## 📋 前置要求

- **Go** 1.21+
- **Polymarket 账户** 已启用交易
- **钱包** 包含:
  - MATIC 用于 gas 费用 (推荐: 0.1+ MATIC)
  - USDC 用于交易 (金额取决于您的策略)

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
touch .env
```

## ⚙️ 配置

### 环境变量

创建 `.env` 文件，内容如下：

```env
# 必需: 您的钱包私钥 (不带 0x 前缀)
PRIVATE_KEY=your_private_key_here

# 可选: 如果您使用 Polymarket web UI (代理钱包)
POLYMARKET_PROXY_ADDRESS=0xYourProxyAddress

# 代币领取必需: API 凭证 (首次运行时会自动派生)
# 首次运行后，从控制台输出复制这些值并粘贴到这里以加快启动速度
POLYMARKET_API_KEY=your_api_key
POLYMARKET_API_SECRET=your_api_secret
POLYMARKET_PASSPHRASE=your_passphrase

# 交易模式: "true" 为实盘交易，"false" 或未设置则为模拟交易
LIVE_TRADING=false
```

### 获取您的私钥

**⚠️ 安全警告**: 永远不要分享您的私钥或将其提交到版本控制！

#### 选项 1: MetaMask

1. 打开 MetaMask
2. 点击账户菜单 (三个点)
3. 选择 "Account Details"
4. 点击 "Export Private Key"
5. 输入您的密码
6. 复制私钥 (如果存在 `0x` 前缀则移除)

#### 选项 2: 生成新钱包 (仅模拟交易)

- 将 `PRIVATE_KEY` 留空
- 机器人将生成随机钱包用于测试
- **注意**: 此钱包没有资金，仅用于模拟交易

### 获取您的代理地址 (如果使用 Web UI)

如果您主要使用 Polymarket 的 web 界面：

1. 登录 [polymarket.com](https://polymarket.com)
2. 转到您的账户设置
3. 找到您的 "Proxy Address" 或 "Deposit Address"
4. 复制到 `.env` 中的 `POLYMARKET_PROXY_ADDRESS`

**注意**: 如果您直接使用 MetaMask，则不需要此操作。

### API 凭证

**这些是自动代币领取所必需的。**

您有两个选项来获取 API 凭证：

#### 选项 1: 从 Polymarket Web 界面获取 (推荐)

1. **转到 Builder 设置**:
   - 访问 [polymarket.com/settings?tab=builder](https://polymarket.com/settings?tab=builder)
   - 或点击您的个人资料图片 → 从下拉菜单中选择 "Builders"

2. **找到您的 API 密钥**:
   - 查找 "Builder Keys" 部分
   - 您会看到现有的 API 密钥及其创建日期
   - 如果没有，点击 "+ Create New" 生成一个

3. **复制三个值**:
   - **API Key** (`apiKey`)
   - **Secret** (`secret`)
   - **Passphrase** (`passphrase`)

4. **添加到 .env**:
   ```env
   POLYMARKET_API_KEY=your_api_key_here
   POLYMARKET_API_SECRET=your_secret_here
   POLYMARKET_PASSPHRASE=your_passphrase_here
   ```

#### 选项 2: 让机器人自动派生

机器人可以从您的钱包签名自动派生 API 凭证：

1. **启动机器人而不设置 API 凭证**:
   ```bash
   go run main.go
   ```

2. **机器人派生凭证** (需要 10-30 秒):
   ```
   ⏳ Deriving API key (this may take 10-30 seconds)...
   ✅ API Key Derived Successfully.
   ```

3. **从控制台复制** (如果已记录) 并添加到 `.env` 以加快未来启动速度

**为什么需要它们**:

- **代币领取**: `TokenClaimer` 服务自动领取获胜代币所必需
- **用户 WebSocket**: 需要认证的 WebSocket 连接以接收您的订单成交
- **更快启动**: 如果预先设置，机器人不需要每次都派生凭证

**注意**: 如果不设置这些，机器人仍可用于交易，但：
- 代币领取将无法工作
- 您不会通过 WebSocket 收到实时订单成交通知
- 机器人需要在每次启动时派生凭证 (较慢)

## 🚀 运行机器人

### 启动机器人

```bash
go run main.go
```

### 模拟交易 (默认)

默认情况下，机器人以**模拟交易模式**运行：

- 使用假 $1,000 USDC 模拟交易
- 无真实资金风险
- 非常适合测试策略

### 实盘交易

要启用实盘交易：

1. 在 `.env` 中设置:
   ```env
   LIVE_TRADING=true
   ```

2. 确保您的钱包有:
   - **MATIC**: 用于 gas 费用 (推荐 0.1+)
   - **USDC**: 用于交易 (金额取决于策略)

3. 启动机器人:
   ```bash
   go run main.go
   ```

### 停止机器人

按 `Ctrl+C` 优雅停止。机器人将：
- 取消所有开放订单
- 关闭 WebSocket 连接
- 保存交易日志

## 📊 策略

这个机器人是**策略无关的** - 您可以通过创建新的策略类来实现任何交易策略。机器人提供所有基础设施 (订单执行、WebSocket 订阅、持仓管理)，您只需实现交易逻辑。

机器人附带预配置的 **SniperLadder** 策略作为示例。

## 📁 项目结构

```
polymarketbot-go/
├── main.go                    # 主入口点 (钱包设置、策略初始化)
├── src/
│   ├── strategies/           # 交易策略 (在此添加您自己的策略)
│   │   └── SniperLadder.go   # 预配置策略
│   ├── execution/            # 订单执行层
│   │   ├── executor.go       # 执行接口 (买入/卖出/取消)
│   │   ├── paper_executor.go # 模拟交易实现
│   │   └── live_executor.go  # 实盘交易实现
│   ├── services/             # 核心机器人服务
│   │   ├── PolymarketService.go  # REST API 客户端 (市场数据、代币)
│   │   ├── WebSocketService.go   # WebSocket 客户端 (价格订阅、成交)
│   │   ├── TokenClaimer.go       # 自动代币赎回
│   │   ├── SpreadsheetLogger.go  # 交易日志 (CSV 导出)
│   │   ├── DashboardManager.go  # 控制台仪表板
│   │   └── FileLogger.go         # 文件日志
│   └── core/                 # 核心工具
│       └── marketmath.go    # 市场时间计算
├── scripts/                  # 工具脚本
├── .env                      # 环境变量 (创建此文件)
├── go.mod                    # 依赖
└── README.md                 # 本文档
```

## 🔄 工作原理

### 机器人架构

机器人遵循模块化、策略无关的架构：

1. **入口点** (`main.go`):
   - 初始化钱包并认证 Polymarket
   - 从钱包签名派生 API 凭证
   - 创建执行器 (Paper 或 Live)
   - 初始化服务 (REST API、WebSocket)
   - 启动您的策略

2. **执行层** (`src/execution/`):
   - **IExecutor 接口**: 定义买入/卖出/取消操作
   - **PaperExecutor**: 模拟交易 (用于测试)
   - **LiveExecutor**: Polymarket 上的真实交易 (用于生产)
   - 策略使用 `IExecutor` - 它们不知道是模拟还是实盘

3. **服务层** (`src/services/`):
   - **PolymarketService**: REST API 调用 (获取代币、市场数据)
   - **WebSocketService**: 实时数据 (价格、您的成交)
   - **TokenClaimer**: 自动赎回 (每 5 秒轮询一次)
   - **SpreadsheetLogger**: 交易日志 (CSV 导出)
   - **DashboardManager**: 控制台仪表板更新

4. **核心工具** (`src/core/`):
   - **MarketMath**: 市场时间计算 (当前/下一个市场、等待时间)

5. **策略层** (`src/strategies/`):
   - 您的交易逻辑
   - 使用执行器下订单
   - 使用服务获取数据
   - 管理自己的状态

### 数据流

```
策略
  ↓
执行器 (Paper/Live) → 在 Polymarket 上下订单
  ↓
WebSocketService → 实时接收成交
  ↓
策略 → 响应成交，下新订单
  ↓
SpreadsheetLogger → 记录交易到 CSV
```

## 🛠️ 故障排除

### 机器人无法启动

**错误**: "Authentication Failed"

- **解决方案**: 确保 `PRIVATE_KEY` 正确且钱包已在 Polymarket 上启用交易

**错误**: "Cannot get tokens"

- **解决方案**: 市场可能尚不可用。机器人会自动重试。

### 订单未成交

- 检查价格是否合理 (不要太远离市场)
- 验证您是否有足够的 USDC 余额
- 检查日志中的 WebSocket 连接状态

### WebSocket 断开连接

机器人自动：
- 检测死连接
- 重连 WebSocket
- 从 API 同步持仓
- 继续交易

### 日志中缺少交易

- 检查根目录中的 `trading_*.csv` 文件
- 验证 `SpreadsheetLogger` 是否工作
- 检查控制台是否有错误

### 赎回不工作

- 确保设置了 `POLYMARKET_API_KEY` 和 `POLYMARKET_API_SECRET` (或让机器人派生它们)
- 检查持仓是否实际可赎回 (市场必须已结算)
- 如果使用 web UI，验证代理地址

## 📝 日志

- **控制台**: 实时交易活动
- **CSV 文件**: `trading_<Strategy>.csv` - 交易历史
- **日志文件**: `logs/bot-YYYY-MM-DD.log` - 详细日志

## 🔒 安全最佳实践

1. **永远不要提交 `.env` 文件** - 它已在 `.gitignore` 中
2. **使用环境变量** - 不要硬编码凭证
3. **从模拟交易开始** - 在实盘交易前彻底测试
4. **监控您的钱包** - 定期检查余额
5. **设置合理的持仓大小** - 不要冒超过您能承受损失的风险

## 📚 其他资源

- [Polymarket API 文档](https://docs.polymarket.com/)
- [Polymarket Discord](https://discord.gg/polymarket) - 社区支持
- [Polygon Network](https://polygon.technology/) - 网络信息

## ⚠️ 免责声明

此机器人仅用于教育目的。交易加密货币和预测市场涉及重大风险。过往表现不能保证未来结果。使用风险自负。

## 📄 许可证

FREE, OPEN SOURCE, ISC

---

**Happy Trading! 🚀**
