package services

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type BotState struct {
	Name          string
	Phase         string
	PriceYes      float64
	PriceNo       float64
	YesShares     float64
	YesAvg        float64
	NoShares      float64
	NoAvg         float64
	ActiveOrders  int
	PnL           *float64
	HedgePrice    *float64
	HedgeSide     *string
	IsComplete    bool
	ExtraInfo     *string
	Status        *string
	UnrealizedPnL *float64
}

// Global status for when bots are between cycles
var (
	botStatusMu sync.RWMutex
	botStatus   = map[string]string{}
)

func SetBotStatus(name, status string) {
	botStatusMu.Lock()
	botStatus[name] = status
	botStatusMu.Unlock()
	DashboardManager.Log("STATUS CHANGE: "+status, LogInfo)
}

func GetBotStatus(name string) (string, bool) {
	botStatusMu.RLock()
	defer botStatusMu.RUnlock()
	s, ok := botStatus[name]
	return s, ok
}

type DashboardManagerClass struct {
	mu sync.Mutex

	lastStatus BotState

	// persistent history (keep last 50 lines)
	eventHistory []string

	fileLogger *FileLogger
}

func NewDashboardManager(fileLogger *FileLogger) *DashboardManagerClass {
	if fileLogger == nil {
		fileLogger = NewFileLogger("")
	}
	return &DashboardManagerClass{
		fileLogger:   fileLogger,
		eventHistory: make([]string, 0, 50),
	}
}

func (d *DashboardManagerClass) Log(message string, level LogLevel) {
	d.mu.Lock()
	defer d.mu.Unlock()

	ts := time.Now().Format("15:04:05")
	icon := "🔹"
	switch level {
	case LogTrade:
		icon = "💥"
	case LogWarn:
		icon = "⚠️"
	case LogError:
		icon = "❌"
	}

	line := fmt.Sprintf("[%s] %s %s", ts, icon, message)
	d.eventHistory = append(d.eventHistory, line)
	if len(d.eventHistory) > 50 {
		d.eventHistory = d.eventHistory[len(d.eventHistory)-50:]
	}

	d.fileLogger.Log(level, message)
	d.renderLocked()
}

func (d *DashboardManagerClass) LogEvent(source, message string) {
	d.Log("["+source+"] "+message, LogTrade)
}

func (d *DashboardManagerClass) LogError(source, message string) {
	d.Log(source+": "+message, LogError)
}

func (d *DashboardManagerClass) Update(status BotState) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastStatus = status
	d.renderLocked()
}

func (d *DashboardManagerClass) renderLocked() {
	// Clear screen (roughly like console.clear())
	fmt.Print("\033[H\033[2J")

	s := d.lastStatus
	now := time.Now().Format("15:04:05")

	fmt.Println("╔════════════════════════════════════════════════════════════════════════════╗")
	fmt.Printf("║ 🤖 POLYMARKET WAR ROOM (%s)             ║\n", padRight(now, 11))
	fmt.Println("╠════════════════════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  🔫 PHASE: %s ║\n", padRight(orDefault(s.Phase, "Waiting"), 67))
	fmt.Printf("║  📊 YES: $%s | NO: $%s                             ║\n",
		padRight(fmt.Sprintf("%.2f", s.PriceYes), 6),
		padRight(fmt.Sprintf("%.2f", s.PriceNo), 6),
	)

	pnl := "0.00"
	if s.PnL != nil {
		pnl = fmt.Sprintf("%.2f", *s.PnL)
	}
	hedgeSide := "-"
	if s.HedgeSide != nil && *s.HedgeSide != "" {
		hedgeSide = *s.HedgeSide
	}
	hedgePrice := "0.00"
	if s.HedgePrice != nil {
		hedgePrice = fmt.Sprintf("%.2f", *s.HedgePrice)
	}
	fmt.Printf("║  💰 PnL: $%s | 🛡️ Hedge: %s @ $%s            ║\n",
		padRight(pnl, 6),
		padRight(hedgeSide, 3),
		padRight(hedgePrice, 6),
	)

	stopLoss := "---"
	if s.UnrealizedPnL != nil {
		stopLoss = "$" + fmt.Sprintf("%.2f", *s.UnrealizedPnL)
	}
	fmt.Printf("║  📉 STOP LOSS CHECK: %s ║\n", padRight(stopLoss, 58))

	fmt.Println("╠════════════════════════════════════════════════════════════════════════════╣")
	fmt.Println("║ 📜 EVENT LOG (ERRORS & TRADES)                                             ║")
	fmt.Println("╠────────────────────────────────────────────────────────────────────────────╣")

	if len(d.eventHistory) == 0 {
		fmt.Println("║ (Waiting for events...)                                                    ║")
	} else {
		for _, line := range d.eventHistory {
			safe := line
			if len([]rune(safe)) > 74 {
				// truncate by runes
				r := []rune(safe)
				safe = string(r[:74])
			}
			fmt.Printf("║ %s ║\n", padRight(safe, 74))
		}
	}
	fmt.Println("╚════════════════════════════════════════════════════════════════════════════╝")
}

func padRight(s string, n int) string {
	r := []rune(s)
	if len(r) >= n {
		return string(r[:n])
	}
	return s + strings.Repeat(" ", n-len(r))
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// Singleton instance (mirrors TS)
var DashboardManager = NewDashboardManager(NewFileLogger(""))
