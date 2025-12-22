package services

import (
	"fmt"
	"sync"
	"time"
)

// DashboardManager 仪表板管理器
// 管理控制台输出和状态显示
type DashboardManager struct {
	mu sync.RWMutex
}

// BotStatus 机器人状态
type BotStatus struct {
	Name          string
	Phase         string
	PriceYes      float64
	PriceNo       float64
	YesShares     float64
	YesAvg        float64
	NoShares      float64
	NoAvg         float64
	ActiveOrders  int
	PnL           float64
	UnrealizedPnL float64
	IsComplete    bool
}

var globalDashboard = &DashboardManager{}

// GetDashboard 获取全局仪表板实例
func GetDashboard() *DashboardManager {
	return globalDashboard
}

// Update 更新仪表板显示
func (dm *DashboardManager) Update(status BotStatus) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	fmt.Printf("\n")
	fmt.Printf("═══════════════════════════════════════════════════════════════\n")
	fmt.Printf("  %s - %s\n", status.Name, status.Phase)
	fmt.Printf("═══════════════════════════════════════════════════════════════\n")
	fmt.Printf("  价格: YES $%.3f | NO $%.3f\n", status.PriceYes, status.PriceNo)
	fmt.Printf("  持仓: YES %.2f@$%.3f | NO %.2f@$%.3f\n",
		status.YesShares, status.YesAvg, status.NoShares, status.NoAvg)
	fmt.Printf("  活跃订单: %d\n", status.ActiveOrders)
	
	if status.UnrealizedPnL != 0 {
		pnlColor := "🟢"
		if status.UnrealizedPnL < 0 {
			pnlColor = "🔴"
		}
		fmt.Printf("  %s 未实现盈亏: $%.4f\n", pnlColor, status.UnrealizedPnL)
	}
	
	if status.IsComplete && status.PnL != 0 {
		pnlColor := "🟢"
		if status.PnL < 0 {
			pnlColor = "🔴"
		}
		fmt.Printf("  %s 实现盈亏: $%.4f\n", pnlColor, status.PnL)
	}
	fmt.Printf("═══════════════════════════════════════════════════════════════\n\n")
}

// Log 记录消息
func (dm *DashboardManager) Log(message, level string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	timestamp := time.Now().Format("15:04:05")
	icon := "ℹ️"
	
	switch level {
	case "INFO":
		icon = "ℹ️"
	case "WARN":
		icon = "⚠️"
	case "ERROR":
		icon = "❌"
	case "TRADE":
		icon = "💰"
	}

	fmt.Printf("[%s] %s %s\n", timestamp, icon, message)
}

// LogEvent 记录事件
func (dm *DashboardManager) LogEvent(strategy, message string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	timestamp := time.Now().Format("15:04:05")
	fmt.Printf("[%s] [%s] %s\n", timestamp, strategy, message)
}

// SetBotStatus 设置机器人状态
func SetBotStatus(name, status string) {
	fmt.Printf("🤖 [%s] %s\n", name, status)
}
