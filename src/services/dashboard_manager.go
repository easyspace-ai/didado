package services

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// BotState 机器人状态
type BotState struct {
	Name         string
	Phase        string
	PriceYes     float64
	PriceNo      float64
	YesShares    float64
	YesAvg       float64
	NoShares     float64
	NoAvg        float64
	ActiveOrders int
	PnL          float64
	HedgePrice   float64
	HedgeSide    string
	IsComplete   bool
	ExtraInfo    string
	Status       string
	UnrealizedPnL float64
}

// DashboardManagerType 仪表板管理器类型
type DashboardManagerType struct {
	lastStatus   *BotState
	eventHistory []string
	mu           sync.Mutex
}

var dashboardInstance *DashboardManagerType
var botStatusMap = make(map[string]string)

func init() {
	dashboardInstance = &DashboardManagerType{
		eventHistory: make([]string, 0),
	}
}

// SetBotStatus 设置机器人状态
func SetBotStatus(name, status string) {
	botStatusMap[name] = status
	dashboardInstance.Log("STATUS CHANGE: "+status, "INFO")
}

// GetBotStatus 获取机器人状态
func GetBotStatus(name string) string {
	return botStatusMap[name]
}

// Log 记录日志
func (d *DashboardManagerType) Log(message string, logType string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	timeStr := time.Now().Format("15:04:05")
	icon := "🔹"

	switch logType {
	case "TRADE":
		icon = "💥"
	case "WARN":
		icon = "⚠️"
	case "ERROR":
		icon = "❌"
	}

	logLine := fmt.Sprintf("[%s] %s %s", timeStr, icon, message)

	// 添加到历史记录
	d.eventHistory = append(d.eventHistory, logLine)
	if len(d.eventHistory) > 50 {
		d.eventHistory = d.eventHistory[1:]
	}

	// 保存到文件
	fileLoggerInstance.Log(logType, message)

	// 立即渲染
	d.Render()
}

// LogEvent 记录事件
func (d *DashboardManagerType) LogEvent(source, message string) {
	d.Log(fmt.Sprintf("[%s] %s", source, message), "TRADE")
}

// LogError 记录错误
func (d *DashboardManagerType) LogError(source, message string) {
	d.Log(fmt.Sprintf("%s: %s", source, message), "ERROR")
}

// LogSystem 记录系统信息
func (d *DashboardManagerType) LogSystem(message string) {
	d.Log(message, "INFO")
}

// Update 更新状态
func (d *DashboardManagerType) Update(status *BotState) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastStatus = status
	d.Render()
}

// Render 渲染仪表板
func (d *DashboardManagerType) Render() {
	os.Stdout.WriteString("\033[2J\033[H") // 清屏

	s := d.lastStatus
	if s == nil {
		s = &BotState{}
	}

	// 状态头部
	fmt.Println("╔════════════════════════════════════════════════════════════════════════════╗")
	fmt.Printf("║ 🤖 POLYMARKET WAR ROOM (%s)             ║\n", time.Now().Format("15:04:05"))
	fmt.Println("╠════════════════════════════════════════════════════════════════════════════╣")

	phase := s.Phase
	if phase == "" {
		phase = "Waiting"
	}
	fmt.Printf("║  🔫 PHASE: %-67s ║\n", phase)

	priceYesStr := fmt.Sprintf("%.2f", s.PriceYes)
	if s.PriceYes == 0 {
		priceYesStr = "0.00"
	}
	priceNoStr := fmt.Sprintf("%.2f", s.PriceNo)
	if s.PriceNo == 0 {
		priceNoStr = "0.00"
	}
	fmt.Printf("║  📊 YES: $%-6s | NO: $%-6s                             ║\n", priceYesStr, priceNoStr)

	pnlStr := fmt.Sprintf("%.2f", s.PnL)
	if s.PnL == 0 {
		pnlStr = "0.00"
	}
	hedgeSide := s.HedgeSide
	if hedgeSide == "" {
		hedgeSide = "-"
	}
	hedgePriceStr := fmt.Sprintf("%.2f", s.HedgePrice)
	if s.HedgePrice == 0 {
		hedgePriceStr = "0.00"
	}
	fmt.Printf("║  💰 PnL: $%-6s | 🛡️ Hedge: %-3s @ $%-6s            ║\n", pnlStr, hedgeSide, hedgePriceStr)

	unrealizedPnLStr := "---"
	if s.UnrealizedPnL != 0 {
		unrealizedPnLStr = fmt.Sprintf("$%.2f", s.UnrealizedPnL)
	}
	fmt.Printf("║  📉 STOP LOSS CHECK: %-58s ║\n", unrealizedPnLStr)
	fmt.Println("╠════════════════════════════════════════════════════════════════════════════╣")

	// 事件日志
	fmt.Println("║ 📜 EVENT LOG (ERRORS & TRADES)                                             ║")
	fmt.Println("╠────────────────────────────────────────────────────────────────────────────╣")

	if len(d.eventHistory) == 0 {
		fmt.Println("║ (Waiting for events...)                                                    ║")
	} else {
		for _, line := range d.eventHistory {
			safeMsg := line
			if len(safeMsg) > 74 {
				safeMsg = safeMsg[:74]
			}
			fmt.Printf("║ %-74s ║\n", safeMsg)
		}
	}

	fmt.Println("╚════════════════════════════════════════════════════════════════════════════╝")
}

// DashboardManager 导出单例实例
var DashboardManager = dashboardInstance
