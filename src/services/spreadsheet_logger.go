package services

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// TradeRecord 交易记录
type TradeRecord struct {
	Time            string
	MarketSlug      string
	CapitalInvested float64
	ProfitPercent   float64
	CapitalEnd      float64
	YesPositions    string
	YesAvg          float64
	YesTotal        float64
	NoPositions     string
	NoAvg           float64
	NoTotal         float64
	PnL             float64
	Outcome         string // "WIN", "LOSS", "BREAKEVEN"
}

// Stats 统计数据
type Stats struct {
	TotalPnL            float64
	TotalCapitalInvested float64
	Wins                int
	Losses              int
	Breakeven           int
	Trades              int
	WinRate             float64
	AvgWin              float64
	AvgLoss             float64
	ROI                 float64
	BestTrade           float64
	WorstTrade          float64
	WinStreak           int
	LossStreak          int
	CurrentStreak       int
	CurrentStreakType   string
}

// SpreadsheetLogger CSV 日志记录器
type SpreadsheetLogger struct {
	filePath    string
	summaryPath string
	strategy    string
	trades      []TradeRecord
}

// NewSpreadsheetLogger 创建新的 CSV 日志记录器
func NewSpreadsheetLogger(strategy string) *SpreadsheetLogger {
	filePath := filepath.Join(".", fmt.Sprintf("trading_%s.csv", strategy))
	summaryPath := filepath.Join(".", "trading_summary.csv")

	logger := &SpreadsheetLogger{
		filePath:    filePath,
		summaryPath: summaryPath,
		strategy:    strategy,
		trades:      make([]TradeRecord, 0),
	}

	logger.loadExistingTrades()
	logger.ensureFileExists()

	return logger
}

// ensureFileExists 确保文件存在
func (s *SpreadsheetLogger) ensureFileExists() {
	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		s.writeFullCSV()
		fmt.Printf("📊 Created %s\n", s.filePath)
	}
	if _, err := os.Stat(s.summaryPath); os.IsNotExist(err) {
		s.updateSummary()
		fmt.Printf("📊 Created %s\n", s.summaryPath)
	}
}

// loadExistingTrades 从 CSV 文件加载现有交易
func (s *SpreadsheetLogger) loadExistingTrades() {
	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		s.trades = []TradeRecord{}
		return
	}

	file, err := os.Open(s.filePath)
	if err != nil {
		s.trades = []TradeRecord{}
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	lines, err := reader.ReadAll()
	if err != nil {
		s.trades = []TradeRecord{}
		return
	}

	// 找到数据开始位置
	dataStartIndex := -1
	for i, line := range lines {
		if len(line) > 0 && strings.HasPrefix(line[0], "Time") {
			dataStartIndex = i + 1
			break
		}
	}

	if dataStartIndex == -1 || dataStartIndex >= len(lines) {
		s.trades = []TradeRecord{}
		return
	}

	// 解析交易
	s.trades = []TradeRecord{}
	for i := dataStartIndex; i < len(lines); i++ {
		line := lines[i]
		if len(line) < 13 {
			continue
		}

		capitalInvested, _ := strconv.ParseFloat(line[2], 64)
		profitPercentStr := strings.TrimSuffix(line[3], "%")
		profitPercent, _ := strconv.ParseFloat(profitPercentStr, 64)
		profitPercent /= 100
		capitalEnd, _ := strconv.ParseFloat(line[4], 64)
		yesAvg, _ := strconv.ParseFloat(line[6], 64)
		yesTotal, _ := strconv.ParseFloat(line[7], 64)
		noAvg, _ := strconv.ParseFloat(line[9], 64)
		noTotal, _ := strconv.ParseFloat(line[10], 64)
		pnl, _ := strconv.ParseFloat(line[11], 64)

		s.trades = append(s.trades, TradeRecord{
			Time:            line[0],
			MarketSlug:      line[1],
			CapitalInvested: capitalInvested,
			ProfitPercent:   profitPercent,
			CapitalEnd:      capitalEnd,
			YesPositions:    line[5],
			YesAvg:          yesAvg,
			YesTotal:        yesTotal,
			NoPositions:     line[8],
			NoAvg:           noAvg,
			NoTotal:         noTotal,
			PnL:             pnl,
			Outcome:         line[12],
		})
	}
}

// LogTrade 记录交易到 CSV 文件
func (s *SpreadsheetLogger) LogTrade(record TradeRecord) {
	s.trades = append(s.trades, record)
	s.writeFullCSV()
	s.updateSummary()
	fmt.Printf("📊 Trade logged to %s\n", s.filePath)
}

// writeFullCSV 写入完整 CSV
func (s *SpreadsheetLogger) writeFullCSV() {
	stats := s.calculateStats()

	file, err := os.Create(s.filePath)
	if err != nil {
		fmt.Printf("❌ Failed to create CSV file: %v\n", err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// 头部
	writer.Write([]string{"═══════════════════════════════════════════════════════════════"})
	writer.Write([]string{strings.ToUpper(s.strategy) + " TRADING REPORT"})
	writer.Write([]string{"Generated: " + time.Now().Format("2006-01-02 15:04:05")})
	writer.Write([]string{"═══════════════════════════════════════════════════════════════"})
	writer.Write([]string{""})

	// 性能摘要
	writer.Write([]string{"📊 PERFORMANCE SUMMARY"})
	writer.Write([]string{"──────────────────────────────────────────────────────────────"})
	writer.Write([]string{"Total PnL", fmt.Sprintf("$%.4f", stats.TotalPnL)})
	writer.Write([]string{"Total Capital Invested", fmt.Sprintf("$%.4f", stats.TotalCapitalInvested)})
	writer.Write([]string{"ROI", fmt.Sprintf("%.2f%%", stats.ROI)})
	writer.Write([]string{""})

	// 胜负统计
	writer.Write([]string{"🎯 WIN/LOSS BREAKDOWN"})
	writer.Write([]string{"──────────────────────────────────────────────────────────────"})
	writer.Write([]string{"Total Trades", strconv.Itoa(stats.Trades)})
	if stats.Trades > 0 {
		writer.Write([]string{"Wins", strconv.Itoa(stats.Wins), fmt.Sprintf("%.1f%%", float64(stats.Wins)/float64(stats.Trades)*100)})
		writer.Write([]string{"Losses", strconv.Itoa(stats.Losses), fmt.Sprintf("%.1f%%", float64(stats.Losses)/float64(stats.Trades)*100)})
		writer.Write([]string{"Breakeven", strconv.Itoa(stats.Breakeven), fmt.Sprintf("%.1f%%", float64(stats.Breakeven)/float64(stats.Trades)*100)})
	}
	writer.Write([]string{"Win Rate", fmt.Sprintf("%.1f%%", stats.WinRate)})
	writer.Write([]string{""})

	// 交易统计
	writer.Write([]string{"📈 TRADE STATISTICS"})
	writer.Write([]string{"──────────────────────────────────────────────────────────────"})
	writer.Write([]string{"Average Win", fmt.Sprintf("$%.4f", stats.AvgWin)})
	writer.Write([]string{"Average Loss", fmt.Sprintf("$%.4f", stats.AvgLoss)})
	writer.Write([]string{"Best Trade", fmt.Sprintf("$%.4f", stats.BestTrade)})
	writer.Write([]string{"Worst Trade", fmt.Sprintf("$%.4f", stats.WorstTrade)})
	if stats.AvgLoss != 0 {
		writer.Write([]string{"Profit Factor", fmt.Sprintf("%.2f", stats.AvgWin/stats.AvgLoss)})
	} else {
		writer.Write([]string{"Profit Factor", "N/A"})
	}
	writer.Write([]string{""})

	// 连胜信息
	writer.Write([]string{"🔥 STREAKS"})
	writer.Write([]string{"──────────────────────────────────────────────────────────────"})
	writer.Write([]string{"Best Win Streak", strconv.Itoa(stats.WinStreak)})
	writer.Write([]string{"Worst Loss Streak", strconv.Itoa(stats.LossStreak)})
	writer.Write([]string{"Current Streak", strconv.Itoa(stats.CurrentStreak) + " " + stats.CurrentStreakType})
	writer.Write([]string{""})

	// 交易表格
	writer.Write([]string{"══════════════════════════════════════════════════════════════"})
	writer.Write([]string{"📋 TRADE HISTORY"})
	writer.Write([]string{"══════════════════════════════════════════════════════════════"})
	writer.Write([]string{""})

	// 表头
	writer.Write([]string{
		"Time", "Market", "Capital In", "Profit %", "Capital Out",
		"YES Positions", "YES Avg", "YES Total", "NO Positions", "NO Avg", "NO Total",
		"PnL", "Outcome",
	})

	// 交易行
	for _, trade := range s.trades {
		writer.Write([]string{
			trade.Time,
			trade.MarketSlug,
			fmt.Sprintf("%.4f", trade.CapitalInvested),
			fmt.Sprintf("%.2f%%", trade.ProfitPercent*100),
			fmt.Sprintf("%.4f", trade.CapitalEnd),
			trade.YesPositions,
			fmt.Sprintf("%.4f", trade.YesAvg),
			strconv.FormatFloat(trade.YesTotal, 'f', 0, 64),
			trade.NoPositions,
			fmt.Sprintf("%.4f", trade.NoAvg),
			strconv.FormatFloat(trade.NoTotal, 'f', 0, 64),
			fmt.Sprintf("%.4f", trade.PnL),
			trade.Outcome,
		})
	}
}

// calculateStats 计算统计数据
func (s *SpreadsheetLogger) calculateStats() Stats {
	stats := Stats{
		WorstTrade: 0,
	}

	if len(s.trades) == 0 {
		return stats
	}

	stats.Trades = len(s.trades)
	var winPnLs, lossPnLs []float64
	currentWinStreak := 0
	currentLossStreak := 0
	maxWinStreak := 0
	maxLossStreak := 0

	for _, trade := range s.trades {
		stats.TotalPnL += trade.PnL
		stats.TotalCapitalInvested += trade.CapitalInvested

		if trade.PnL > stats.BestTrade {
			stats.BestTrade = trade.PnL
		}
		if trade.PnL < stats.WorstTrade {
			stats.WorstTrade = trade.PnL
		}

		if trade.Outcome == "WIN" {
			stats.Wins++
			winPnLs = append(winPnLs, trade.PnL)
			currentWinStreak++
			currentLossStreak = 0
			if currentWinStreak > maxWinStreak {
				maxWinStreak = currentWinStreak
			}
		} else if trade.Outcome == "LOSS" {
			stats.Losses++
			lossPnLs = append(lossPnLs, trade.PnL)
			currentLossStreak++
			currentWinStreak = 0
			if currentLossStreak > maxLossStreak {
				maxLossStreak = currentLossStreak
			}
		} else {
			stats.Breakeven++
			currentWinStreak = 0
			currentLossStreak = 0
		}
	}

	// 计算平均值
	if len(winPnLs) > 0 {
		sum := 0.0
		for _, pnl := range winPnLs {
			sum += pnl
		}
		stats.AvgWin = sum / float64(len(winPnLs))
	}

	if len(lossPnLs) > 0 {
		sum := 0.0
		for _, pnl := range lossPnLs {
			sum += pnl
		}
		stats.AvgLoss = sum / float64(len(lossPnLs))
	}

	// 胜率
	decisiveTrades := stats.Wins + stats.Losses
	if decisiveTrades > 0 {
		stats.WinRate = float64(stats.Wins) / float64(decisiveTrades) * 100
	}

	// ROI
	if stats.TotalCapitalInvested > 0 {
		stats.ROI = stats.TotalPnL / stats.TotalCapitalInvested * 100
	}

	// 连胜
	stats.WinStreak = maxWinStreak
	stats.LossStreak = maxLossStreak
	if currentWinStreak > 0 {
		stats.CurrentStreak = currentWinStreak
		stats.CurrentStreakType = "WINS"
	} else if currentLossStreak > 0 {
		stats.CurrentStreak = currentLossStreak
		stats.CurrentStreakType = "LOSSES"
	}

	return stats
}

// updateSummary 更新汇总文件
func (s *SpreadsheetLogger) updateSummary() {
	stats := s.calculateStats()

	file, err := os.Create(s.summaryPath)
	if err != nil {
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	writer.Write([]string{"═══════════════════════════════════════════════════════════════════════════════"})
	writer.Write([]string{"POLYMARKET BOT - COMBINED TRADING SUMMARY"})
	writer.Write([]string{"Last Updated: " + time.Now().Format("2006-01-02 15:04:05")})
	writer.Write([]string{"═══════════════════════════════════════════════════════════════════════════════"})
	writer.Write([]string{""})

	writer.Write([]string{"🏆 OVERALL PERFORMANCE"})
	writer.Write([]string{"───────────────────────────────────────────────────────────────────────────────"})
	writer.Write([]string{"Metric", "Value"})
	writer.Write([]string{"Total PnL", fmt.Sprintf("$%.4f", stats.TotalPnL)})
	writer.Write([]string{"Total Capital Invested", fmt.Sprintf("$%.4f", stats.TotalCapitalInvested)})
	writer.Write([]string{"ROI", fmt.Sprintf("%.2f%%", stats.ROI)})
	writer.Write([]string{"Total Trades", strconv.Itoa(stats.Trades)})
	writer.Write([]string{"Wins", strconv.Itoa(stats.Wins)})
	writer.Write([]string{"Losses", strconv.Itoa(stats.Losses)})
	writer.Write([]string{"Breakeven", strconv.Itoa(stats.Breakeven)})
	writer.Write([]string{"Win Rate", fmt.Sprintf("%.1f%%", stats.WinRate)})
	writer.Write([]string{""})
}

// FormatPositions 格式化持仓数组为可读字符串
func FormatPositions(fills []Fill) string {
	parts := make([]string, len(fills))
	for i, f := range fills {
		parts[i] = fmt.Sprintf("%.2f@$%.2f", f.Size, f.Price)
	}
	return strings.Join(parts, "; ")
}

// CalculateAvg 从成交记录计算平均价格
func CalculateAvg(fills []Fill) float64 {
	if len(fills) == 0 {
		return 0
	}
	totalCost := 0.0
	totalSize := 0.0
	for _, f := range fills {
		totalCost += f.Price * f.Size
		totalSize += f.Size
	}
	if totalSize > 0 {
		return totalCost / totalSize
	}
	return 0
}

// Fill 成交记录
type Fill struct {
	Price float64
	Size  float64
}
