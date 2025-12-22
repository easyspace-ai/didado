package services

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/polymarketbot-go/pkg/types"
)

// SpreadsheetLogger CSV交易日志记录器
type SpreadsheetLogger struct {
	filePath    string
	summaryPath string
	strategy    string
	trades      []types.TradeRecord
}

// Stats 统计数据
type Stats struct {
	TotalPnL             float64
	TotalCapitalInvested float64
	Wins                 int
	Losses               int
	Breakeven            int
	Trades               int
	WinRate              float64
	AvgWin               float64
	AvgLoss              float64
	ROI                  float64
	BestTrade            float64
	WorstTrade           float64
}

// NewSpreadsheetLogger 创建新的日志记录器
func NewSpreadsheetLogger(strategy string) *SpreadsheetLogger {
	cwd, _ := os.Getwd()
	filePath := filepath.Join(cwd, fmt.Sprintf("trading_%s.csv", strategy))
	summaryPath := filepath.Join(cwd, "trading_summary.csv")

	logger := &SpreadsheetLogger{
		filePath:    filePath,
		summaryPath: summaryPath,
		strategy:    strategy,
		trades:      make([]types.TradeRecord, 0),
	}

	logger.loadExistingTrades()
	logger.ensureFileExists()

	return logger
}

// LogTrade 记录交易
func (sl *SpreadsheetLogger) LogTrade(record types.TradeRecord) error {
	sl.trades = append(sl.trades, record)
	
	if err := sl.writeFullCSV(); err != nil {
		return fmt.Errorf("写入CSV失败: %w", err)
	}

	if err := sl.updateSummary(); err != nil {
		return fmt.Errorf("更新摘要失败: %w", err)
	}

	fmt.Printf("📊 交易已记录到 %s\n", sl.filePath)
	return nil
}

// loadExistingTrades 加载现有交易
func (sl *SpreadsheetLogger) loadExistingTrades() {
	// 简化实现 - 实际需要解析CSV文件
	sl.trades = make([]types.TradeRecord, 0)
}

// ensureFileExists 确保文件存在
func (sl *SpreadsheetLogger) ensureFileExists() {
	if _, err := os.Stat(sl.filePath); os.IsNotExist(err) {
		sl.writeFullCSV()
		fmt.Printf("📊 已创建 %s\n", sl.filePath)
	}

	if _, err := os.Stat(sl.summaryPath); os.IsNotExist(err) {
		sl.updateSummary()
		fmt.Printf("📊 已创建 %s\n", sl.summaryPath)
	}
}

// writeFullCSV 写入完整的CSV文件
func (sl *SpreadsheetLogger) writeFullCSV() error {
	file, err := os.Create(sl.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	stats := sl.calculateStats()

	// 写入头部
	file.WriteString("═══════════════════════════════════════════════════════════════\n")
	file.WriteString(fmt.Sprintf("%s 交易报告\n", sl.strategy))
	file.WriteString(fmt.Sprintf("生成时间: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	file.WriteString("═══════════════════════════════════════════════════════════════\n\n")

	// 写入统计摘要
	file.WriteString("📊 性能摘要\n")
	file.WriteString("──────────────────────────────────────────────────────────────\n")
	file.WriteString(fmt.Sprintf("总盈亏,$%.4f\n", stats.TotalPnL))
	file.WriteString(fmt.Sprintf("总投资资金,$%.4f\n", stats.TotalCapitalInvested))
	file.WriteString(fmt.Sprintf("ROI,%.2f%%\n\n", stats.ROI))

	// 写入交易历史表头
	file.WriteString("══════════════════════════════════════════════════════════════\n")
	file.WriteString("📋 交易历史\n")
	file.WriteString("══════════════════════════════════════════════════════════════\n\n")

	// CSV表头
	writer.Write([]string{
		"时间", "市场", "投入资金", "利润%", "最终资金",
		"YES持仓", "YES平均", "YES总数",
		"NO持仓", "NO平均", "NO总数",
		"盈亏", "结果",
	})

	// 写入交易数据
	for _, trade := range sl.trades {
		writer.Write([]string{
			trade.Time.Format("2006-01-02 15:04:05"),
			trade.MarketSlug,
			fmt.Sprintf("%.4f", trade.CapitalInvested),
			fmt.Sprintf("%.2f%%", trade.ProfitPercent*100),
			fmt.Sprintf("%.4f", trade.CapitalEnd),
			trade.YesPositions,
			fmt.Sprintf("%.4f", trade.YesAvg),
			fmt.Sprintf("%.0f", trade.YesTotal),
			trade.NoPositions,
			fmt.Sprintf("%.4f", trade.NoAvg),
			fmt.Sprintf("%.0f", trade.NoTotal),
			fmt.Sprintf("%.4f", trade.PnL),
			trade.Outcome,
		})
	}

	return nil
}

// updateSummary 更新摘要文件
func (sl *SpreadsheetLogger) updateSummary() error {
	stats := sl.calculateStats()

	file, err := os.Create(sl.summaryPath)
	if err != nil {
		return err
	}
	defer file.Close()

	file.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
	file.WriteString("POLYMARKET BOT - 综合交易摘要\n")
	file.WriteString(fmt.Sprintf("最后更新: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	file.WriteString("═══════════════════════════════════════════════════════════════════════════════\n\n")

	file.WriteString("🏆 整体表现\n")
	file.WriteString("───────────────────────────────────────────────────────────────────────────────\n")
	file.WriteString(fmt.Sprintf("总盈亏,$%.4f\n", stats.TotalPnL))
	file.WriteString(fmt.Sprintf("总投资资金,$%.4f\n", stats.TotalCapitalInvested))
	file.WriteString(fmt.Sprintf("ROI,%.2f%%\n", stats.ROI))
	file.WriteString(fmt.Sprintf("总交易数,%d\n", stats.Trades))
	file.WriteString(fmt.Sprintf("胜率,%.1f%%\n", stats.WinRate))

	return nil
}

// calculateStats 计算统计数据
func (sl *SpreadsheetLogger) calculateStats() Stats {
	stats := Stats{}

	if len(sl.trades) == 0 {
		return stats
	}

	stats.Trades = len(sl.trades)

	for _, trade := range sl.trades {
		stats.TotalPnL += trade.PnL
		stats.TotalCapitalInvested += trade.CapitalInvested

		if trade.PnL > stats.BestTrade {
			stats.BestTrade = trade.PnL
		}
		if trade.PnL < stats.WorstTrade {
			stats.WorstTrade = trade.PnL
		}

		switch trade.Outcome {
		case "WIN":
			stats.Wins++
			stats.AvgWin += trade.PnL
		case "LOSS":
			stats.Losses++
			stats.AvgLoss += trade.PnL
		case "BREAKEVEN":
			stats.Breakeven++
		}
	}

	if stats.Wins > 0 {
		stats.AvgWin /= float64(stats.Wins)
	}
	if stats.Losses > 0 {
		stats.AvgLoss /= float64(stats.Losses)
	}

	decisiveTrades := stats.Wins + stats.Losses
	if decisiveTrades > 0 {
		stats.WinRate = float64(stats.Wins) / float64(decisiveTrades) * 100
	}

	if stats.TotalCapitalInvested > 0 {
		stats.ROI = stats.TotalPnL / stats.TotalCapitalInvested * 100
	}

	return stats
}

// FormatPositions 格式化持仓字符串
func FormatPositions(fills []struct{ Price, Size float64 }) string {
	result := ""
	for i, f := range fills {
		if i > 0 {
			result += "; "
		}
		result += fmt.Sprintf("%.1f@$%.2f", f.Size, f.Price)
	}
	return result
}

// CalculateAvg 计算平均价格
func CalculateAvg(fills []struct{ Price, Size float64 }) float64 {
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
