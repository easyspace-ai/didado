package services

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Outcome string

const (
	OutcomeWin       Outcome = "WIN"
	OutcomeLoss      Outcome = "LOSS"
	OutcomeBreakeven Outcome = "BREAKEVEN"
)

type TradeRecord struct {
	Time            string
	MarketSlug      string
	CapitalInvested float64
	ProfitPercent   float64 // 0.025 = 2.5%
	CapitalEnd      float64
	YesPositions    string
	YesAvg          float64
	YesTotal        float64
	NoPositions     string
	NoAvg           float64
	NoTotal         float64
	PnL             float64
	Outcome         Outcome
}

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
	WinStreak            int
	LossStreak           int
	CurrentStreak        int
	CurrentStreakType    string
}

type SpreadsheetLogger struct {
	filePath    string
	summaryPath string
	strategy    string
	trades      []TradeRecord
}

func NewSpreadsheetLogger(strategy string) *SpreadsheetLogger {
	cwd := mustCwd()
	l := &SpreadsheetLogger{
		strategy:    strategy,
		filePath:    filepath.Join(cwd, fmt.Sprintf("trading_%s.csv", strategy)),
		summaryPath: filepath.Join(cwd, "trading_summary.csv"),
	}
	l.loadExistingTrades()
	l.ensureFileExists()
	return l
}

func (l *SpreadsheetLogger) LogTrade(record TradeRecord) {
	l.trades = append(l.trades, record)
	l.writeFullCSV()
	l.updateSummary()
}

func (l *SpreadsheetLogger) GetSummary() Stats {
	return l.calculateStats(l.trades)
}

func (l *SpreadsheetLogger) ensureFileExists() {
	if _, err := os.Stat(l.filePath); err != nil {
		l.writeFullCSV()
	}
	if _, err := os.Stat(l.summaryPath); err != nil {
		l.updateSummary()
	}
}

func (l *SpreadsheetLogger) loadExistingTrades() {
	f, err := os.Open(l.filePath)
	if err != nil {
		l.trades = nil
		return
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	// Find header
	dataStart := -1
	for i := range lines {
		if strings.HasPrefix(lines[i], "Time,Market,") {
			dataStart = i + 1
			break
		}
	}
	if dataStart == -1 || dataStart >= len(lines) {
		l.trades = nil
		return
	}

	var trades []TradeRecord
	for i := dataStart; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		fields := parseCSVLine(line)
		if len(fields) < 13 {
			continue
		}
		pp := 0.0
		if strings.HasSuffix(fields[3], "%") {
			v, _ := strconv.ParseFloat(strings.TrimSuffix(fields[3], "%"), 64)
			pp = v / 100
		}
		tr := TradeRecord{
			Time:            fields[0],
			MarketSlug:      fields[1],
			CapitalInvested: mustF(fields[2]),
			ProfitPercent:   pp,
			CapitalEnd:      mustF(fields[4]),
			YesPositions:    fields[5],
			YesAvg:          mustF(fields[6]),
			YesTotal:        mustF(fields[7]),
			NoPositions:     fields[8],
			NoAvg:           mustF(fields[9]),
			NoTotal:         mustF(fields[10]),
			PnL:             mustF(fields[11]),
			Outcome:         Outcome(fields[12]),
		}
		trades = append(trades, tr)
	}
	l.trades = trades
}

func (l *SpreadsheetLogger) writeFullCSV() {
	stats := l.calculateStats(l.trades)
	escape := func(v any) string {
		s := fmt.Sprint(v)
		if strings.ContainsAny(s, ",\"\n") {
			return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
		}
		return s
	}

	var out []string
	out = append(out, "═══════════════════════════════════════════════════════════════")
	out = append(out, strings.ToUpper(l.strategy)+" TRADING REPORT")
	out = append(out, "Generated: "+time.Now().Format(time.RFC1123))
	out = append(out, "═══════════════════════════════════════════════════════════════")
	out = append(out, "")

	out = append(out, "📊 PERFORMANCE SUMMARY")
	out = append(out, "──────────────────────────────────────────────────────────────")
	out = append(out, fmt.Sprintf("Total PnL:,$%.4f", stats.TotalPnL))
	out = append(out, fmt.Sprintf("Total Capital Invested:,$%.4f", stats.TotalCapitalInvested))
	out = append(out, fmt.Sprintf("ROI:,%.2f%%", stats.ROI))
	out = append(out, "")

	out = append(out, "🎯 WIN/LOSS BREAKDOWN")
	out = append(out, "──────────────────────────────────────────────────────────────")
	out = append(out, fmt.Sprintf("Total Trades:,%d", stats.Trades))
	winPct := 0.0
	lossPct := 0.0
	bePct := 0.0
	if stats.Trades > 0 {
		winPct = (float64(stats.Wins) / float64(stats.Trades)) * 100
		lossPct = (float64(stats.Losses) / float64(stats.Trades)) * 100
		bePct = (float64(stats.Breakeven) / float64(stats.Trades)) * 100
	}
	out = append(out, fmt.Sprintf("Wins:,%d,%.1f%%", stats.Wins, winPct))
	out = append(out, fmt.Sprintf("Losses:,%d,%.1f%%", stats.Losses, lossPct))
	out = append(out, fmt.Sprintf("Breakeven:,%d,%.1f%%", stats.Breakeven, bePct))
	out = append(out, fmt.Sprintf("Win Rate:,%.1f%%", stats.WinRate))
	out = append(out, "")

	out = append(out, "📈 TRADE STATISTICS")
	out = append(out, "──────────────────────────────────────────────────────────────")
	out = append(out, fmt.Sprintf("Average Win:,$%.4f", stats.AvgWin))
	out = append(out, fmt.Sprintf("Average Loss:,$%.4f", stats.AvgLoss))
	out = append(out, fmt.Sprintf("Best Trade:,$%.4f", stats.BestTrade))
	out = append(out, fmt.Sprintf("Worst Trade:,$%.4f", stats.WorstTrade))
	pf := "N/A"
	if stats.AvgLoss != 0 {
		pf = fmt.Sprintf("%.2f", abs(stats.AvgWin/stats.AvgLoss))
	}
	out = append(out, fmt.Sprintf("Profit Factor:,%s", pf))
	out = append(out, "")

	out = append(out, "🔥 STREAKS")
	out = append(out, "──────────────────────────────────────────────────────────────")
	out = append(out, fmt.Sprintf("Best Win Streak:,%d", stats.WinStreak))
	out = append(out, fmt.Sprintf("Worst Loss Streak:,%d", stats.LossStreak))
	out = append(out, fmt.Sprintf("Current Streak:,%d %s", stats.CurrentStreak, stats.CurrentStreakType))
	out = append(out, "")

	out = append(out, "══════════════════════════════════════════════════════════════")
	out = append(out, "📋 TRADE HISTORY")
	out = append(out, "══════════════════════════════════════════════════════════════")
	out = append(out, "")

	out = append(out, strings.Join([]string{
		"Time", "Market", "Capital In", "Profit %", "Capital Out",
		"YES Positions", "YES Avg", "YES Total",
		"NO Positions", "NO Avg", "NO Total",
		"PnL", "Outcome",
	}, ","))

	for _, t := range l.trades {
		out = append(out, strings.Join([]string{
			escape(t.Time),
			escape(t.MarketSlug),
			fmt.Sprintf("%.4f", t.CapitalInvested),
			fmt.Sprintf("%.2f%%", t.ProfitPercent*100),
			fmt.Sprintf("%.4f", t.CapitalEnd),
			escape(t.YesPositions),
			fmt.Sprintf("%.4f", t.YesAvg),
			fmt.Sprintf("%.0f", t.YesTotal),
			escape(t.NoPositions),
			fmt.Sprintf("%.4f", t.NoAvg),
			fmt.Sprintf("%.0f", t.NoTotal),
			fmt.Sprintf("%.4f", t.PnL),
			string(t.Outcome),
		}, ","))
	}

	_ = os.WriteFile(l.filePath, []byte(strings.Join(out, "\n")), 0o644)
}

func (l *SpreadsheetLogger) updateSummary() {
	// TS版本的 summary 目前只读取 SniperLadder（其余策略不参与合并）
	sniper := l.getStatsFromStrategy("SniperLadder")

	combinedTrades := sniper.Trades
	combinedWins := sniper.Wins
	combinedLosses := sniper.Losses
	combinedBreakeven := sniper.Breakeven
	combinedPnL := sniper.TotalPnL
	combinedCapital := sniper.TotalCapitalInvested
	combinedDecisive := combinedWins + combinedLosses
	combinedWinRate := 0.0
	if combinedDecisive > 0 {
		combinedWinRate = (float64(combinedWins) / float64(combinedDecisive)) * 100
	}
	combinedROI := 0.0
	if combinedCapital > 0 {
		combinedROI = (combinedPnL / combinedCapital) * 100
	}

	var out []string
	out = append(out, "═══════════════════════════════════════════════════════════════════════════════")
	out = append(out, "POLYMARKET BOT - COMBINED TRADING SUMMARY")
	out = append(out, "Last Updated: "+time.Now().Format(time.RFC1123))
	out = append(out, "═══════════════════════════════════════════════════════════════════════════════")
	out = append(out, "")

	out = append(out, "🏆 OVERALL PERFORMANCE")
	out = append(out, "───────────────────────────────────────────────────────────────────────────────")
	out = append(out, "Metric,Value")
	out = append(out, fmt.Sprintf("Total PnL,$%.4f", combinedPnL))
	out = append(out, fmt.Sprintf("Total Capital Invested,$%.4f", combinedCapital))
	out = append(out, fmt.Sprintf("ROI,%.2f%%", combinedROI))
	out = append(out, fmt.Sprintf("Total Trades,%d", combinedTrades))
	out = append(out, fmt.Sprintf("Wins,%d", combinedWins))
	out = append(out, fmt.Sprintf("Losses,%d", combinedLosses))
	out = append(out, fmt.Sprintf("Breakeven,%d", combinedBreakeven))
	out = append(out, fmt.Sprintf("Win Rate,%.1f%%", combinedWinRate))
	out = append(out, "")

	out = append(out, "📊 STRATEGY COMPARISON")
	out = append(out, "───────────────────────────────────────────────────────────────────────────────")
	out = append(out, "Strategy,PnL,Trades,Wins,Losses,Win Rate,ROI,Avg Win,Avg Loss")
	out = append(out, fmt.Sprintf(
		"SniperLadder,$%.4f,%d,%d,%d,%.1f%%,%.2f%%,$%.4f,$%.4f",
		sniper.TotalPnL, sniper.Trades, sniper.Wins, sniper.Losses, sniper.WinRate, sniper.ROI, sniper.AvgWin, sniper.AvgLoss,
	))
	out = append(out, "───────────────────────────────────────────────────────────────────────────────")
	out = append(out, fmt.Sprintf("COMBINED,$%.4f,%d,%d,%d,%.1f%%,%.2f%%,-,-", combinedPnL, combinedTrades, combinedWins, combinedLosses, combinedWinRate, combinedROI))
	out = append(out, "")

	out = append(out, "📈 PERFORMANCE INDICATOR")
	out = append(out, "───────────────────────────────────────────────────────────────────────────────")
	pnlIndicator := "🟢 PROFITABLE"
	if combinedPnL < 0 {
		pnlIndicator = "🔴 LOSING"
	}
	winRateIndicator := "🟢 POSITIVE"
	if combinedWinRate < 50 {
		winRateIndicator = "🔴 NEEDS IMPROVEMENT"
	}
	out = append(out, fmt.Sprintf("Overall Status:,%s", pnlIndicator))
	out = append(out, fmt.Sprintf("Win Rate Status:,%s", winRateIndicator))
	out = append(out, "")

	out = append(out, "🎯 BEST & WORST")
	out = append(out, "───────────────────────────────────────────────────────────────────────────────")
	out = append(out, fmt.Sprintf("SniperLadder Best Trade,$%.4f", sniper.BestTrade))
	out = append(out, fmt.Sprintf("SniperLadder Worst Trade,$%.4f", sniper.WorstTrade))

	_ = os.WriteFile(l.summaryPath, []byte(strings.Join(out, "\n")), 0o644)
}

func (l *SpreadsheetLogger) getStatsFromStrategy(strategy string) Stats {
	filePath := filepath.Join(mustCwd(), fmt.Sprintf("trading_%s.csv", strategy))
	f, err := os.Open(filePath)
	if err != nil {
		return Stats{}
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	dataStart := -1
	for i := range lines {
		if strings.HasPrefix(lines[i], "Time,Market,") {
			dataStart = i + 1
			break
		}
	}
	if dataStart == -1 {
		return Stats{}
	}

	var trades []TradeRecord
	for i := dataStart; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		fields := parseCSVLine(line)
		if len(fields) < 13 {
			continue
		}
		pp := 0.0
		if strings.HasSuffix(fields[3], "%") {
			v, _ := strconv.ParseFloat(strings.TrimSuffix(fields[3], "%"), 64)
			pp = v / 100
		}
		trades = append(trades, TradeRecord{
			Time:            fields[0],
			MarketSlug:      fields[1],
			CapitalInvested: mustF(fields[2]),
			ProfitPercent:   pp,
			CapitalEnd:      mustF(fields[4]),
			YesPositions:    fields[5],
			YesAvg:          mustF(fields[6]),
			YesTotal:        mustF(fields[7]),
			NoPositions:     fields[8],
			NoAvg:           mustF(fields[9]),
			NoTotal:         mustF(fields[10]),
			PnL:             mustF(fields[11]),
			Outcome:         Outcome(fields[12]),
		})
	}
	return l.calculateStats(trades)
}

func (l *SpreadsheetLogger) calculateStats(trades []TradeRecord) Stats {
	st := Stats{
		Trades: len(trades),
	}
	if len(trades) == 0 {
		return st
	}

	winPnL := make([]float64, 0, len(trades))
	lossPnL := make([]float64, 0, len(trades))
	curWin := 0
	curLoss := 0
	maxWin := 0
	maxLoss := 0

	for _, t := range trades {
		st.TotalPnL += t.PnL
		st.TotalCapitalInvested += t.CapitalInvested
		if t.PnL > st.BestTrade {
			st.BestTrade = t.PnL
		}
		if t.PnL < st.WorstTrade {
			st.WorstTrade = t.PnL
		}

		switch t.Outcome {
		case OutcomeWin:
			st.Wins++
			winPnL = append(winPnL, t.PnL)
			curWin++
			curLoss = 0
			if curWin > maxWin {
				maxWin = curWin
			}
		case OutcomeLoss:
			st.Losses++
			lossPnL = append(lossPnL, t.PnL)
			curLoss++
			curWin = 0
			if curLoss > maxLoss {
				maxLoss = curLoss
			}
		default:
			st.Breakeven++
			curWin = 0
			curLoss = 0
		}
	}

	st.AvgWin = avg(winPnL)
	st.AvgLoss = avg(lossPnL)

	decisive := st.Wins + st.Losses
	if decisive > 0 {
		st.WinRate = (float64(st.Wins) / float64(decisive)) * 100
	}
	if st.TotalCapitalInvested > 0 {
		st.ROI = (st.TotalPnL / st.TotalCapitalInvested) * 100
	}

	st.WinStreak = maxWin
	st.LossStreak = maxLoss
	if curWin > 0 {
		st.CurrentStreak = curWin
		st.CurrentStreakType = "WINS"
	} else if curLoss > 0 {
		st.CurrentStreak = curLoss
		st.CurrentStreakType = "LOSSES"
	}

	return st
}

func parseCSVLine(line string) []string {
	var res []string
	var cur strings.Builder
	inQuotes := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch ch {
		case '"':
			if inQuotes && i+1 < len(line) && line[i+1] == '"' {
				cur.WriteByte('"')
				i++
				continue
			}
			inQuotes = !inQuotes
		case ',':
			if !inQuotes {
				res = append(res, cur.String())
				cur.Reset()
			} else {
				cur.WriteByte(ch)
			}
		default:
			cur.WriteByte(ch)
		}
	}
	res = append(res, cur.String())
	return res
}

func FormatPositions(fills []Fill) string {
	parts := make([]string, 0, len(fills))
	for _, f := range fills {
		parts = append(parts, fmt.Sprintf("%.0f@$%.2f", f.Size, f.Price))
	}
	return strings.Join(parts, "; ")
}

type Fill struct {
	Price float64
	Size  float64
}

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
	if totalSize == 0 {
		return 0
	}
	return totalCost / totalSize
}

func mustF(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

func avg(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range xs {
		sum += v
	}
	return sum / float64(len(xs))
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
