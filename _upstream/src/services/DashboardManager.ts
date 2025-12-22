/**
 * Centralized Dashboard Manager
 * "Black Box" Logger - Persistent Error History
 * Nothing gets deleted, all errors are logged permanently
 */

import { FileLogger } from "./FileLogger";

export interface BotState {
  name: string;
  phase: string;
  priceYes: number;
  priceNo: number;
  yesShares: number;
  yesAvg: number;
  noShares: number;
  noAvg: number;
  activeOrders: number;
  pnl?: number;
  hedgePrice?: number;
  hedgeSide?: string;
  isComplete: boolean;
  extraInfo?: string;
  status?: string;
  unrealizedPnL?: number; // For stop loss display
}

// Global status for when bots are between cycles
const botStatus: Map<string, string> = new Map();

export function setBotStatus(name: string, status: string) {
  botStatus.set(name, status);
  DashboardManager.log(`STATUS CHANGE: ${status}`, "INFO");
}

export function getBotStatus(name: string): string | undefined {
  return botStatus.get(name);
}

class DashboardManagerClass {
  private lastStatus: any = {};

  // 🟢 PERSISTENT HISTORY (The "Black Box")
  // We keep the last 50 lines so you can scroll back and see exactly what happened.
  private eventHistory: string[] = [];

  // 🟢 UNIFIED LOGGING (Info, Trade, Warn, Error)
  public log(
    message: string,
    type: "INFO" | "TRADE" | "WARN" | "ERROR" = "INFO"
  ) {
    const time = new Date().toLocaleTimeString();
    let icon = "🔹";

    if (type === "TRADE") icon = "💥";
    if (type === "WARN") icon = "⚠️";
    if (type === "ERROR") icon = "❌";

    const logLine = `[${time}] ${icon} ${message}`;

    // Add to history
    this.eventHistory.push(logLine);
    // Keep last 50 lines (increased from 20 for deep debugging)
    if (this.eventHistory.length > 50) this.eventHistory.shift();

    // 🟢 BLACK BOX: Save to File
    FileLogger.log(type, message);

    // Force render immediately so you see it
    this.render();
  }

  // --- WRAPPERS FOR COMPATIBILITY ---
  public logEvent(source: string, message: string) {
    // Hook this one too - it will call log() which saves to file
    this.log(`[${source}] ${message}`, "TRADE");
  }

  public logError(source: string, message: string) {
    this.log(`${source}: ${message}`, "ERROR");
  }

  public logSystem(message: string) {
    this.log(message, "INFO");
  }

  public logSystemEvent(message: string) {
    this.log(message, "INFO");
  }

  public update(status: any) {
    this.lastStatus = status;
    this.render();
  }

  private render() {
    console.clear();
    const s = this.lastStatus;

    // 1. STATUS HEADER
    console.log(
      `╔════════════════════════════════════════════════════════════════════════════╗`
    );
    console.log(
      `║ 🤖 POLYMARKET WAR ROOM (${new Date().toLocaleTimeString()})             ║`
    );
    console.log(
      `╠════════════════════════════════════════════════════════════════════════════╣`
    );
    console.log(`║  🔫 PHASE: ${(s.phase || "Waiting").padEnd(67)} ║`);
    console.log(
      `║  📊 YES: $${(s.priceYes?.toFixed(2) || "0.00").padEnd(6)} | NO: $${(
        s.priceNo?.toFixed(2) || "0.00"
      ).padEnd(6)}                             ║`
    );
    console.log(
      `║  💰 PnL: $${(s.pnl?.toFixed(2) || "0.00").padEnd(6)} | 🛡️ Hedge: ${(
        s.hedgeSide || "-"
      ).padEnd(3)} @ $${(s.hedgePrice?.toFixed(2) || "0.00").padEnd(
        6
      )}            ║`
    );
    console.log(
      `║  📉 STOP LOSS CHECK: ${(s.unrealizedPnL
        ? "$" + s.unrealizedPnL.toFixed(2)
        : "---"
      ).padEnd(58)} ║`
    );
    console.log(
      `╠════════════════════════════════════════════════════════════════════════════╣`
    );

    // 2. THE BLACK BOX (HISTORY)
    console.log(
      `║ 📜 EVENT LOG (ERRORS & TRADES)                                             ║`
    );
    console.log(
      `╠────────────────────────────────────────────────────────────────────────────╣`
    );

    if (this.eventHistory.length === 0) {
      console.log(
        `║ (Waiting for events...)                                                    ║`
      );
    } else {
      this.eventHistory.forEach((line) => {
        // Cut string to fit box width
        const safeMsg = line.substring(0, 74).padEnd(74);
        console.log(`║ ${safeMsg} ║`);
      });
    }
    console.log(
      `╚════════════════════════════════════════════════════════════════════════════╝`
    );
  }
}

// Singleton instance
export const DashboardManager = new DashboardManagerClass();
