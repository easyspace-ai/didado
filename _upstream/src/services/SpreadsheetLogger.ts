import * as fs from "fs";
import * as path from "path";

export interface TradeRecord {
  time: string;
  marketSlug: string;
  capitalInvested: number;
  profitPercent: number;
  capitalEnd: number;
  yesPositions: string; // e.g. "1@0.45, 1@0.40, 1@0.35"
  yesAvg: number;
  yesTotal: number;
  noPositions: string; // e.g. "1@0.57, 1@0.55"
  noAvg: number;
  noTotal: number;
  pnl: number;
  outcome: "WIN" | "LOSS" | "BREAKEVEN";
}

interface Stats {
  totalPnL: number;
  totalCapitalInvested: number;
  wins: number;
  losses: number;
  breakeven: number;
  trades: number;
  winRate: number;
  avgWin: number;
  avgLoss: number;
  roi: number;
  bestTrade: number;
  worstTrade: number;
  winStreak: number;
  lossStreak: number;
  currentStreak: number;
  currentStreakType: string;
}

export class SpreadsheetLogger {
  private filePath: string;
  private summaryPath: string;
  private strategy: string;
  private trades: TradeRecord[] = [];

  constructor(strategy: "SniperLadder" | "SimpleTrap") {
    this.strategy = strategy;
    this.filePath = path.join(process.cwd(), `trading_${strategy}.csv`);
    this.summaryPath = path.join(process.cwd(), `trading_summary.csv`);
    this.loadExistingTrades();
    this.ensureFileExists();
  }

  /**
   * Create file immediately if it doesn't exist
   */
  private ensureFileExists() {
    if (!fs.existsSync(this.filePath)) {
      // Create initial file with headers
      this.writeFullCSV();
      console.log(`📊 Created ${this.filePath}`);
    }
    if (!fs.existsSync(this.summaryPath)) {
      this.updateSummary();
      console.log(`📊 Created ${this.summaryPath}`);
    }
  }

  /**
   * Load existing trades from CSV file
   */
  private loadExistingTrades() {
    if (!fs.existsSync(this.filePath)) {
      this.trades = [];
      return;
    }

    try {
      const content = fs.readFileSync(this.filePath, "utf-8");
      const lines = content.trim().split("\n");

      // Find where trades data starts (after the summary section)
      let dataStartIndex = -1;
      for (let i = 0; i < lines.length; i++) {
        if (lines[i].startsWith("Time,Market,")) {
          dataStartIndex = i + 1;
          break;
        }
      }

      if (dataStartIndex === -1 || dataStartIndex >= lines.length) {
        this.trades = [];
        return;
      }

      // Parse trades
      this.trades = [];
      for (let i = dataStartIndex; i < lines.length; i++) {
        const line = lines[i];
        if (!line.trim()) continue;

        const fields = this.parseCSVLine(line);
        if (fields.length >= 13) {
          this.trades.push({
            time: fields[0],
            marketSlug: fields[1],
            capitalInvested: parseFloat(fields[2]) || 0,
            profitPercent: parseFloat(fields[3].replace("%", "")) / 100 || 0,
            capitalEnd: parseFloat(fields[4]) || 0,
            yesPositions: fields[5],
            yesAvg: parseFloat(fields[6]) || 0,
            yesTotal: parseInt(fields[7]) || 0,
            noPositions: fields[8],
            noAvg: parseFloat(fields[9]) || 0,
            noTotal: parseInt(fields[10]) || 0,
            pnl: parseFloat(fields[11]) || 0,
            outcome: fields[12] as "WIN" | "LOSS" | "BREAKEVEN",
          });
        }
      }
    } catch (e) {
      console.error("Failed to load existing trades:", e);
      this.trades = [];
    }
  }

  /**
   * Log a completed trade to the CSV file
   */
  logTrade(record: TradeRecord) {
    try {
      this.trades.push(record);
      this.writeFullCSV();
      this.updateSummary();
      console.log(`📊 Trade logged to ${this.filePath}`);
    } catch (e) {
      console.error("❌ Failed to log trade to CSV:", e);
    }
  }

  /**
   * Write the full CSV with stats at top and trades below
   */
  private writeFullCSV() {
    const stats = this.calculateStats();
    const escapeField = (field: string | number) => {
      const str = String(field);
      if (str.includes(",") || str.includes('"') || str.includes("\n")) {
        return `"${str.replace(/"/g, '""')}"`;
      }
      return str;
    };

    // Build the file content
    const lines: string[] = [];

    // ===== HEADER =====
    lines.push(
      `═══════════════════════════════════════════════════════════════`
    );
    lines.push(`${this.strategy.toUpperCase()} TRADING REPORT`);
    lines.push(`Generated: ${new Date().toLocaleString()}`);
    lines.push(
      `═══════════════════════════════════════════════════════════════`
    );
    lines.push(``);

    // ===== PERFORMANCE SUMMARY =====
    lines.push(`📊 PERFORMANCE SUMMARY`);
    lines.push(
      `──────────────────────────────────────────────────────────────`
    );
    lines.push(`Total PnL:,$${stats.totalPnL.toFixed(4)}`);
    lines.push(
      `Total Capital Invested:,$${stats.totalCapitalInvested.toFixed(4)}`
    );
    lines.push(`ROI:,${stats.roi.toFixed(2)}%`);
    lines.push(``);

    // ===== WIN/LOSS BREAKDOWN =====
    lines.push(`🎯 WIN/LOSS BREAKDOWN`);
    lines.push(
      `──────────────────────────────────────────────────────────────`
    );
    lines.push(`Total Trades:,${stats.trades}`);
    lines.push(
      `Wins:,${stats.wins},${
        stats.trades > 0 ? ((stats.wins / stats.trades) * 100).toFixed(1) : 0
      }%`
    );
    lines.push(
      `Losses:,${stats.losses},${
        stats.trades > 0 ? ((stats.losses / stats.trades) * 100).toFixed(1) : 0
      }%`
    );
    lines.push(
      `Breakeven:,${stats.breakeven},${
        stats.trades > 0
          ? ((stats.breakeven / stats.trades) * 100).toFixed(1)
          : 0
      }%`
    );
    lines.push(`Win Rate:,${stats.winRate.toFixed(1)}%`);
    lines.push(``);

    // ===== TRADE STATISTICS =====
    lines.push(`📈 TRADE STATISTICS`);
    lines.push(
      `──────────────────────────────────────────────────────────────`
    );
    lines.push(`Average Win:,$${stats.avgWin.toFixed(4)}`);
    lines.push(`Average Loss:,$${stats.avgLoss.toFixed(4)}`);
    lines.push(`Best Trade:,$${stats.bestTrade.toFixed(4)}`);
    lines.push(`Worst Trade:,$${stats.worstTrade.toFixed(4)}`);
    lines.push(
      `Profit Factor:,${
        stats.avgLoss !== 0
          ? Math.abs(stats.avgWin / stats.avgLoss).toFixed(2)
          : "N/A"
      }`
    );
    lines.push(``);

    // ===== STREAK INFO =====
    lines.push(`🔥 STREAKS`);
    lines.push(
      `──────────────────────────────────────────────────────────────`
    );
    lines.push(`Best Win Streak:,${stats.winStreak}`);
    lines.push(`Worst Loss Streak:,${stats.lossStreak}`);
    lines.push(
      `Current Streak:,${stats.currentStreak} ${stats.currentStreakType}`
    );
    lines.push(``);

    // ===== TRADES TABLE =====
    lines.push(
      `══════════════════════════════════════════════════════════════`
    );
    lines.push(`📋 TRADE HISTORY`);
    lines.push(
      `══════════════════════════════════════════════════════════════`
    );
    lines.push(``);

    // Headers
    lines.push(
      [
        "Time",
        "Market",
        "Capital In",
        "Profit %",
        "Capital Out",
        "YES Positions",
        "YES Avg",
        "YES Total",
        "NO Positions",
        "NO Avg",
        "NO Total",
        "PnL",
        "Outcome",
      ].join(",")
    );

    // Trade rows
    for (const trade of this.trades) {
      lines.push(
        [
          trade.time,
          escapeField(trade.marketSlug),
          trade.capitalInvested.toFixed(4),
          (trade.profitPercent * 100).toFixed(2) + "%",
          trade.capitalEnd.toFixed(4),
          escapeField(trade.yesPositions),
          trade.yesAvg.toFixed(4),
          trade.yesTotal,
          escapeField(trade.noPositions),
          trade.noAvg.toFixed(4),
          trade.noTotal,
          trade.pnl.toFixed(4),
          trade.outcome,
        ].join(",")
      );
    }

    fs.writeFileSync(this.filePath, lines.join("\n"));
  }

  /**
   * Calculate comprehensive stats
   */
  private calculateStats(): Stats {
    const stats: Stats = {
      totalPnL: 0,
      totalCapitalInvested: 0,
      wins: 0,
      losses: 0,
      breakeven: 0,
      trades: this.trades.length,
      winRate: 0,
      avgWin: 0,
      avgLoss: 0,
      roi: 0,
      bestTrade: 0,
      worstTrade: 0,
      winStreak: 0,
      lossStreak: 0,
      currentStreak: 0,
      currentStreakType: "",
    };

    if (this.trades.length === 0) return stats;

    let winPnLs: number[] = [];
    let lossPnLs: number[] = [];
    let currentWinStreak = 0;
    let currentLossStreak = 0;
    let maxWinStreak = 0;
    let maxLossStreak = 0;

    for (const trade of this.trades) {
      stats.totalPnL += trade.pnl;
      stats.totalCapitalInvested += trade.capitalInvested;

      if (trade.pnl > stats.bestTrade) stats.bestTrade = trade.pnl;
      if (trade.pnl < stats.worstTrade) stats.worstTrade = trade.pnl;

      if (trade.outcome === "WIN") {
        stats.wins++;
        winPnLs.push(trade.pnl);
        currentWinStreak++;
        currentLossStreak = 0;
        if (currentWinStreak > maxWinStreak) maxWinStreak = currentWinStreak;
      } else if (trade.outcome === "LOSS") {
        stats.losses++;
        lossPnLs.push(trade.pnl);
        currentLossStreak++;
        currentWinStreak = 0;
        if (currentLossStreak > maxLossStreak)
          maxLossStreak = currentLossStreak;
      } else {
        stats.breakeven++;
        currentWinStreak = 0;
        currentLossStreak = 0;
      }
    }

    // Calculate averages
    stats.avgWin =
      winPnLs.length > 0
        ? winPnLs.reduce((a, b) => a + b, 0) / winPnLs.length
        : 0;
    stats.avgLoss =
      lossPnLs.length > 0
        ? lossPnLs.reduce((a, b) => a + b, 0) / lossPnLs.length
        : 0;

    // Win rate (excluding breakeven)
    const decisiveTrades = stats.wins + stats.losses;
    stats.winRate =
      decisiveTrades > 0 ? (stats.wins / decisiveTrades) * 100 : 0;

    // ROI
    stats.roi =
      stats.totalCapitalInvested > 0
        ? (stats.totalPnL / stats.totalCapitalInvested) * 100
        : 0;

    // Streaks
    stats.winStreak = maxWinStreak;
    stats.lossStreak = maxLossStreak;
    stats.currentStreak =
      currentWinStreak > 0 ? currentWinStreak : currentLossStreak;
    stats.currentStreakType =
      currentWinStreak > 0 ? "WINS" : currentLossStreak > 0 ? "LOSSES" : "";

    return stats;
  }

  /**
   * Update the combined summary CSV file
   */
  private updateSummary() {
    try {
      const sniperStats = this.getStatsFromStrategy("SniperLadder");

      const combinedTrades = sniperStats.trades;
      const combinedWins = sniperStats.wins;
      const combinedLosses = sniperStats.losses;
      const combinedBreakeven = sniperStats.breakeven;
      const combinedPnL = sniperStats.totalPnL;
      const combinedCapital = sniperStats.totalCapitalInvested;
      const combinedDecisive = combinedWins + combinedLosses;
      const combinedWinRate =
        combinedDecisive > 0 ? (combinedWins / combinedDecisive) * 100 : 0;
      const combinedROI =
        combinedCapital > 0 ? (combinedPnL / combinedCapital) * 100 : 0;

      const lines: string[] = [];

      lines.push(
        `═══════════════════════════════════════════════════════════════════════════════`
      );
      lines.push(`POLYMARKET BOT - COMBINED TRADING SUMMARY`);
      lines.push(`Last Updated: ${new Date().toLocaleString()}`);
      lines.push(
        `═══════════════════════════════════════════════════════════════════════════════`
      );
      lines.push(``);

      // Combined Summary
      lines.push(`🏆 OVERALL PERFORMANCE`);
      lines.push(
        `───────────────────────────────────────────────────────────────────────────────`
      );
      lines.push(`Metric,Value`);
      lines.push(`Total PnL,$${combinedPnL.toFixed(4)}`);
      lines.push(`Total Capital Invested,$${combinedCapital.toFixed(4)}`);
      lines.push(`ROI,${combinedROI.toFixed(2)}%`);
      lines.push(`Total Trades,${combinedTrades}`);
      lines.push(`Wins,${combinedWins}`);
      lines.push(`Losses,${combinedLosses}`);
      lines.push(`Breakeven,${combinedBreakeven}`);
      lines.push(`Win Rate,${combinedWinRate.toFixed(1)}%`);
      lines.push(``);

      // Strategy Comparison Table
      lines.push(`📊 STRATEGY COMPARISON`);
      lines.push(
        `───────────────────────────────────────────────────────────────────────────────`
      );
      lines.push(
        `Strategy,PnL,Trades,Wins,Losses,Win Rate,ROI,Avg Win,Avg Loss`
      );
      lines.push(
        `SniperLadder,$${sniperStats.totalPnL.toFixed(4)},${
          sniperStats.trades
        },${sniperStats.wins},${
          sniperStats.losses
        },${sniperStats.winRate.toFixed(1)}%,${sniperStats.roi.toFixed(
          2
        )}%,$${sniperStats.avgWin.toFixed(4)},$${sniperStats.avgLoss.toFixed(
          4
        )}`
      );
      lines.push(
        `───────────────────────────────────────────────────────────────────────────────`
      );
      lines.push(
        `COMBINED,$${combinedPnL.toFixed(
          4
        )},${combinedTrades},${combinedWins},${combinedLosses},${combinedWinRate.toFixed(
          1
        )}%,${combinedROI.toFixed(2)}%,-,-`
      );
      lines.push(``);

      // Visual Performance Bar
      lines.push(`📈 PERFORMANCE INDICATOR`);
      lines.push(
        `───────────────────────────────────────────────────────────────────────────────`
      );
      const pnlIndicator = combinedPnL >= 0 ? "🟢 PROFITABLE" : "🔴 LOSING";
      const winRateIndicator =
        combinedWinRate >= 50 ? "🟢 POSITIVE" : "🔴 NEEDS IMPROVEMENT";
      lines.push(`Overall Status:,${pnlIndicator}`);
      lines.push(`Win Rate Status:,${winRateIndicator}`);
      lines.push(``);

      // Best/Worst
      lines.push(`🎯 BEST & WORST`);
      lines.push(
        `───────────────────────────────────────────────────────────────────────────────`
      );
      lines.push(
        `SniperLadder Best Trade,$${sniperStats.bestTrade.toFixed(4)}`
      );
      lines.push(
        `SniperLadder Worst Trade,$${sniperStats.worstTrade.toFixed(4)}`
      );

      fs.writeFileSync(this.summaryPath, lines.join("\n"));
    } catch (e) {
      console.error("Failed to update summary:", e);
    }
  }

  /**
   * Get stats for a specific strategy
   */
  private getStatsFromStrategy(strategy: string): Stats {
    const filePath = path.join(process.cwd(), `trading_${strategy}.csv`);

    const defaultStats: Stats = {
      totalPnL: 0,
      totalCapitalInvested: 0,
      wins: 0,
      losses: 0,
      breakeven: 0,
      trades: 0,
      winRate: 0,
      avgWin: 0,
      avgLoss: 0,
      roi: 0,
      bestTrade: 0,
      worstTrade: 0,
      winStreak: 0,
      lossStreak: 0,
      currentStreak: 0,
      currentStreakType: "",
    };

    if (!fs.existsSync(filePath)) return defaultStats;

    try {
      const content = fs.readFileSync(filePath, "utf-8");
      const lines = content.trim().split("\n");

      // Find trades data
      let dataStartIndex = -1;
      for (let i = 0; i < lines.length; i++) {
        if (lines[i].startsWith("Time,Market,")) {
          dataStartIndex = i + 1;
          break;
        }
      }

      if (dataStartIndex === -1) return defaultStats;

      const trades: TradeRecord[] = [];
      for (let i = dataStartIndex; i < lines.length; i++) {
        const line = lines[i];
        if (!line.trim()) continue;

        const fields = this.parseCSVLine(line);
        if (fields.length >= 13) {
          trades.push({
            time: fields[0],
            marketSlug: fields[1],
            capitalInvested: parseFloat(fields[2]) || 0,
            profitPercent: parseFloat(fields[3].replace("%", "")) / 100 || 0,
            capitalEnd: parseFloat(fields[4]) || 0,
            yesPositions: fields[5],
            yesAvg: parseFloat(fields[6]) || 0,
            yesTotal: parseInt(fields[7]) || 0,
            noPositions: fields[8],
            noAvg: parseFloat(fields[9]) || 0,
            noTotal: parseInt(fields[10]) || 0,
            pnl: parseFloat(fields[11]) || 0,
            outcome: fields[12] as "WIN" | "LOSS" | "BREAKEVEN",
          });
        }
      }

      // Calculate stats from trades
      return this.calculateStatsFromTrades(trades);
    } catch (e) {
      return defaultStats;
    }
  }

  /**
   * Calculate stats from an array of trades
   */
  private calculateStatsFromTrades(trades: TradeRecord[]): Stats {
    const stats: Stats = {
      totalPnL: 0,
      totalCapitalInvested: 0,
      wins: 0,
      losses: 0,
      breakeven: 0,
      trades: trades.length,
      winRate: 0,
      avgWin: 0,
      avgLoss: 0,
      roi: 0,
      bestTrade: 0,
      worstTrade: 0,
      winStreak: 0,
      lossStreak: 0,
      currentStreak: 0,
      currentStreakType: "",
    };

    if (trades.length === 0) return stats;

    let winPnLs: number[] = [];
    let lossPnLs: number[] = [];
    let currentWinStreak = 0;
    let currentLossStreak = 0;
    let maxWinStreak = 0;
    let maxLossStreak = 0;

    for (const trade of trades) {
      stats.totalPnL += trade.pnl;
      stats.totalCapitalInvested += trade.capitalInvested;

      if (trade.pnl > stats.bestTrade) stats.bestTrade = trade.pnl;
      if (trade.pnl < stats.worstTrade) stats.worstTrade = trade.pnl;

      if (trade.outcome === "WIN") {
        stats.wins++;
        winPnLs.push(trade.pnl);
        currentWinStreak++;
        currentLossStreak = 0;
        if (currentWinStreak > maxWinStreak) maxWinStreak = currentWinStreak;
      } else if (trade.outcome === "LOSS") {
        stats.losses++;
        lossPnLs.push(trade.pnl);
        currentLossStreak++;
        currentWinStreak = 0;
        if (currentLossStreak > maxLossStreak)
          maxLossStreak = currentLossStreak;
      } else {
        stats.breakeven++;
        currentWinStreak = 0;
        currentLossStreak = 0;
      }
    }

    stats.avgWin =
      winPnLs.length > 0
        ? winPnLs.reduce((a, b) => a + b, 0) / winPnLs.length
        : 0;
    stats.avgLoss =
      lossPnLs.length > 0
        ? lossPnLs.reduce((a, b) => a + b, 0) / lossPnLs.length
        : 0;

    const decisiveTrades = stats.wins + stats.losses;
    stats.winRate =
      decisiveTrades > 0 ? (stats.wins / decisiveTrades) * 100 : 0;
    stats.roi =
      stats.totalCapitalInvested > 0
        ? (stats.totalPnL / stats.totalCapitalInvested) * 100
        : 0;

    stats.winStreak = maxWinStreak;
    stats.lossStreak = maxLossStreak;
    stats.currentStreak =
      currentWinStreak > 0 ? currentWinStreak : currentLossStreak;
    stats.currentStreakType =
      currentWinStreak > 0 ? "WINS" : currentLossStreak > 0 ? "LOSSES" : "";

    return stats;
  }

  /**
   * Parse a CSV line handling quoted fields
   */
  private parseCSVLine(line: string): string[] {
    const result: string[] = [];
    let current = "";
    let inQuotes = false;

    for (let i = 0; i < line.length; i++) {
      const char = line[i];

      if (char === '"') {
        if (inQuotes && line[i + 1] === '"') {
          current += '"';
          i++;
        } else {
          inQuotes = !inQuotes;
        }
      } else if (char === "," && !inQuotes) {
        result.push(current);
        current = "";
      } else {
        current += char;
      }
    }
    result.push(current);

    return result;
  }

  /**
   * Helper to format positions array into readable string
   */
  static formatPositions(fills: { price: number; size: number }[]): string {
    return fills.map((f) => `${f.size}@$${f.price.toFixed(2)}`).join("; ");
  }

  /**
   * Calculate average price from fills
   */
  static calculateAvg(fills: { price: number; size: number }[]): number {
    if (fills.length === 0) return 0;
    const totalCost = fills.reduce((sum, f) => sum + f.price * f.size, 0);
    const totalSize = fills.reduce((sum, f) => sum + f.size, 0);
    return totalSize > 0 ? totalCost / totalSize : 0;
  }

  /**
   * Get summary stats for a strategy
   */
  getSummary(): Stats {
    return this.calculateStats();
  }
}
