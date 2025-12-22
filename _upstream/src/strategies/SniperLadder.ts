import { PolymarketService } from "../services/PolymarketService";
import { WebSocketService } from "../services/WebSocketService";
import { MarketMath } from "../core/MarketMath";
import { IExecutor } from "../execution/IExecutor";
import { SpreadsheetLogger, TradeRecord } from "../services/SpreadsheetLogger";
import { DashboardManager, setBotStatus } from "../services/DashboardManager";
import { TokenClaimer } from "../services/TokenClaimer";
import { Wallet } from "@ethersproject/wallet";

export class SniperLadder {
  private api: PolymarketService;
  private ws: WebSocketService;
  private executor: IExecutor;
  private isRunning: boolean = false;
  private logger: SpreadsheetLogger;

  // --- CONFIG ---
  private INITIAL_TRAP_PRICE = 0.45;
  private INITIAL_TRAP_SIZE = 5;
  private LEG_THRESHOLD = 4.95; // 🟢 SAFETY: Need ~5 shares to trigger war mode

  private LADDER_LEVELS = [
    { price: 0.37, size: 5 },
    { price: 0.3, size: 5 },
  ];

  private INITIAL_PROFIT_TARGET_PERCENT = 0.025;
  private PIVOT_TIME_BEFORE_END_MS = 180000;
  private MAX_SPREAD = 0.4;
  private ONE_CYCLE_ONLY = true;

  // 🚨 PANIC MODE TIMING
  private PARTIAL_SELL_TIME_MS = 90000; // 1.5 minutes before market close
  private FULL_SELL_TIME_MS = 60000; // 1 minute before market close
  private PRICE_HISTORY_LENGTH = 100; // Track 60-100 ticks for price validation

  // --- STATE ---
  private phase:
    | "NEUTRAL"
    | "TRIGGERED"
    | "ACCUMULATION"
    | "PARTIAL_HEDGE"
    | "PIVOT"
    | "EXIT" = "NEUTRAL";

  private firstFillTime: number = 0;
  private currentMarketSlug: string = "";
  private marketEndTimeMs: number = 0;

  // 🟢 DEDUPLICATION LIST
  private processedMatchIds: Set<string> = new Set();

  private activeOrders: Map<
    string,
    { side: "YES" | "NO"; type: "TRAP" | "LADDER" | "HEDGE"; price: number }
  > = new Map();

  private orderFillHistory: Map<string, number> = new Map();

  private filledCountYes: number = 0;
  private filledCountNo: number = 0;
  private avgCostYes: number = 0;
  private avgCostNo: number = 0;
  private yesFills: { price: number; size: number }[] = [];
  private noFills: { price: number; size: number }[] = [];

  private tokenIdYes: string = "";
  private tokenIdNo: string = "";
  private priceYes: number = 0;
  private priceNo: number = 0;

  private lastLogTime: number = 0;
  private isProcessingTick: boolean = false;
  private lastSyncTime: number = 0;

  // 🟢 WATCHDOG STATE
  private lastTickTime: number = Date.now();
  private zeroDataStreak: number = 0;

  // 🟢 CRITICAL FIX: Prevent race condition in handleUserEvent
  private isHedging: boolean = false;

  // 🟢 ZERO-PROTECTION: Track last trade time to ignore API lag
  private lastTradeTime: number = 0;

  // 🚨 PANIC MODE: Price history for validation
  private priceHistoryYes: number[] = [];
  private priceHistoryNo: number[] = [];
  private partialSellExecuted: boolean = false;
  private fullSellExecuted: boolean = false;

  // 🟢 CRITICAL FIX: Store interval IDs for cleanup
  private healthCheckInterval: NodeJS.Timeout | null = null;
  private syncInterval: NodeJS.Timeout | null = null;
  private marketEndTimer: NodeJS.Timeout | null = null; // 🟢 CRITICAL: Store market end timer

  private apiCreds: { key: string; secret: string; passphrase: string } | null =
    null;

  // Token claiming service
  private tokenClaimer: TokenClaimer | null = null;

  constructor(
    api: PolymarketService,
    ws: WebSocketService,
    executor: IExecutor,
    apiCreds?: { key: string; secret: string; passphrase: string },
    signer?: Wallet,
    proxyAddress?: string
  ) {
    this.api = api;
    this.ws = ws;
    this.executor = executor;
    this.logger = new SpreadsheetLogger("SniperLadder");
    this.apiCreds = apiCreds || null;

    // Initialize TokenClaimer if signer and credentials are available
    if (signer && apiCreds?.key && apiCreds?.secret) {
      try {
        this.tokenClaimer = new TokenClaimer({
          signer,
          builderKey: apiCreds.key,
          builderSecret: apiCreds.secret,
          builderPassphrase: apiCreds.passphrase || "",
          proxyAddress,
          pollIntervalMs: 5000, // Poll every 5 seconds
        });
        console.log("[SNIPER] ✅ TokenClaimer initialized");
      } catch (e: any) {
        console.error(
          `[SNIPER] ⚠️ Failed to initialize TokenClaimer: ${e.message}`
        );
      }
    } else {
      console.log(
        "[SNIPER] ⚠️ TokenClaimer not initialized (missing signer or credentials)"
      );
    }
  }

  async start() {
    this.isRunning = true;
    console.log("🚀 SNIPER LADDER BOT STARTED (v5 - SIT & WAIT)");
    console.log(`   🛡️ Logic: Event-Driven. Recalculates ONLY on Fills.`);

    await this.hydrateState();

    const creds =
      process.env.POLYMARKET_API_KEY && process.env.POLYMARKET_API_SECRET
        ? {
            key: process.env.POLYMARKET_API_KEY,
            secret: process.env.POLYMARKET_API_SECRET,
            passphrase: process.env.POLYMARKET_PASSPHRASE || "",
          }
        : this.apiCreds || {
            key: "",
            secret: "",
            passphrase: "",
          };

    if (!creds.key || !creds.secret) {
      console.error("❌ CRITICAL: API Keys missing!");
    } else {
      // 🟢 1. USER STREAM: Only listens to YOUR trades. Triggers Logic.
      await this.ws.subscribeUser(creds, (data) => this.handleUserFill(data));
    }

    this.healthCheckInterval = setInterval(
      () => this.checkConnectionHealth(),
      15000
    );
    this.syncInterval = setInterval(() => this.syncPositions(), 30000);

    this.safeRunLifecycle();
  }

  // 🟢 CRITICAL FIX: Graceful shutdown method
  async stop() {
    console.log("🛑 Stopping bot gracefully...");
    this.isRunning = false;

    // Stop token claiming
    if (this.tokenClaimer) {
      this.tokenClaimer.stopPolling();
    }

    try {
      // 🟢 CRITICAL FIX: Clear intervals and timers to prevent memory leaks
      if (this.healthCheckInterval) {
        clearInterval(this.healthCheckInterval);
        this.healthCheckInterval = null;
      }
      if (this.syncInterval) {
        clearInterval(this.syncInterval);
        this.syncInterval = null;
      }
      if (this.marketEndTimer) {
        clearTimeout(this.marketEndTimer);
        this.marketEndTimer = null;
      }

      // Cancel all open orders
      await this.cancelAllOrders();
      DashboardManager.log("✅ All orders cancelled during shutdown.", "INFO");

      // Close WebSocket connections
      this.ws.close();
      DashboardManager.log("✅ WebSocket connections closed.", "INFO");
    } catch (e: any) {
      DashboardManager.log(`❌ Error during shutdown: ${e.message}`, "ERROR");
    }
  }

  // --------------------------------------------------------------------------
  // 🟢 1. THE WATCHER (Updates Price ONLY. SITS.)
  // --------------------------------------------------------------------------
  private async handleMarketTick(data: any) {
    if (this.isProcessingTick) return;
    this.isProcessingTick = true;
    this.lastTickTime = Date.now(); // 🟢 PET THE WATCHDOG

    try {
      // Update prices only
      if (data.event_type === "price_change" || data.event_type === "book") {
        if (data.asset_id === this.tokenIdYes) {
          const newPrice = parseFloat(data.price);
          if (newPrice > 0) {
            this.priceYes = newPrice;
            // 🚨 Track price history for panic mode validation
            this.priceHistoryYes.push(newPrice);
            if (this.priceHistoryYes.length > this.PRICE_HISTORY_LENGTH) {
              this.priceHistoryYes.shift();
            }
          }
        }
        if (data.asset_id === this.tokenIdNo) {
          const newPrice = parseFloat(data.price);
          if (newPrice > 0) {
            this.priceNo = newPrice;
            // 🚨 Track price history for panic mode validation
            this.priceHistoryNo.push(newPrice);
            if (this.priceHistoryNo.length > this.PRICE_HISTORY_LENGTH) {
              this.priceHistoryNo.shift();
            }
          }
        }
      }
      // Also handle the old format
      this.updatePrices(data);

      // 🟢 RULE: If data is 0 for 15 ticks, we assume socket is stuck.
      const isPriceZero = this.priceYes === 0 && this.priceNo === 0;
      const isAmnesia =
        this.phase !== "NEUTRAL" &&
        this.phase !== "EXIT" &&
        this.filledCountYes === 0 &&
        this.filledCountNo === 0;

      if (isPriceZero || isAmnesia) {
        this.zeroDataStreak++;
        if (this.zeroDataStreak > 15) {
          DashboardManager.log(
            "⚠️ ANOMALY: Data Streak Dead. Resetting Socket...",
            "WARN"
          );
          this.zeroDataStreak = 0;
          await this.reconnectWebSocket();
        }
      } else {
        this.zeroDataStreak = 0;
      }

      // Periodic order status check (backup to WS)
      await this.checkFills();

      // 🚨 PANIC MODE: Check conditions near market end
      if (
        this.phase !== "NEUTRAL" &&
        this.phase !== "EXIT" &&
        this.marketEndTimeMs > 0
      ) {
        const msUntilEnd = this.marketEndTimeMs - Date.now();

        // PARTIAL SELL: 1.5 minutes before close (unbalanced positions)
        if (
          msUntilEnd < this.PARTIAL_SELL_TIME_MS &&
          msUntilEnd >= this.FULL_SELL_TIME_MS &&
          !this.partialSellExecuted
        ) {
          await this.checkPartialSell();
        }

        // FULL SELL: 1 minute before close (naked position)
        if (msUntilEnd < this.FULL_SELL_TIME_MS && !this.fullSellExecuted) {
          await this.checkFullSell();
        }

        // Legacy pivot logic (if panic modes don't trigger)
        if (msUntilEnd < this.PIVOT_TIME_BEFORE_END_MS) {
          await this.executeLiquidityEscape();
        }
      }

      // Update dashboard periodically
      const now = Date.now();
      if (now - this.lastLogTime > 1000) {
        this.lastLogTime = now;
        this.updateDashboard();
      }

      // 🛑 NOTE: We do NOT call recalculateAndHedge here. Only on user fills.
    } catch (err: any) {
      DashboardManager.log(`❌ Tick error: ${err.message}`, "ERROR");
    } finally {
      this.isProcessingTick = false;
    }
  }

  // --------------------------------------------------------------------------
  // 🟢 2. THE OWNER (Updates Fills. MOVES.)
  // --------------------------------------------------------------------------
  private handleUserFill(data: any) {
    if (!data || data.event_type !== "trade") return;

    // Deduplicate events
    const uniqueId =
      data.match_id || data.id || `${data.timestamp}-${data.order_id}`;
    if (this.processedMatchIds.has(uniqueId)) return;
    this.processedMatchIds.add(uniqueId);
    if (this.processedMatchIds.size > 1000) this.processedMatchIds.clear();

    this.lastTradeTime = Date.now();

    const side = data.side;
    const size = parseFloat(data.size);
    const price = parseFloat(data.price);
    const assetId = data.asset_id;

    if (isNaN(size) || isNaN(price)) return;

    DashboardManager.log(
      `⚡ FILL CONFIRMED: ${side} ${size} @ ${price}`,
      "TRADE"
    );

    // Update Inventory
    if (assetId === this.tokenIdYes) {
      if (side === "BUY") {
        this.filledCountYes += size;
        this.avgCostYes += price * size;
        this.yesFills.push({ price, size });
      } else {
        this.filledCountYes -= size;
        if (this.filledCountYes > 0) {
          this.avgCostYes =
            (this.avgCostYes / (this.filledCountYes + size)) *
            this.filledCountYes;
        } else {
          this.avgCostYes = 0;
        }
      }
    } else if (assetId === this.tokenIdNo) {
      if (side === "BUY") {
        this.filledCountNo += size;
        this.avgCostNo += price * size;
        this.noFills.push({ price, size });
      } else {
        this.filledCountNo -= size;
        if (this.filledCountNo > 0) {
          this.avgCostNo =
            (this.avgCostNo / (this.filledCountNo + size)) * this.filledCountNo;
        } else {
          this.avgCostNo = 0;
        }
      }
    }

    // Trigger Logic
    this.checkExitOrHedge(side);
  }

  private checkExitOrHedge(lastFillSide: "BUY" | "SELL") {
    // 1. Victory Check
    if (
      this.filledCountYes > 0.1 &&
      Math.abs(this.filledCountYes - this.filledCountNo) < 0.1
    ) {
      DashboardManager.log("⚖️ PERFECT BALANCE REACHED. EXITING...", "TRADE");
      this.transitionToExit();
      return;
    }

    // 2. Initial Trigger
    if (this.phase === "NEUTRAL") {
      const currentSideCount =
        this.filledCountYes > this.filledCountNo
          ? this.filledCountYes
          : this.filledCountNo;
      if (currentSideCount >= this.LEG_THRESHOLD) {
        DashboardManager.log(`🦵 LEG COMPLETED. LADDER ACTIVATING!`, "TRADE");
        this.phase = "TRIGGERED";

        // Place Ladders ONCE
        const side = this.filledCountYes > this.filledCountNo ? "YES" : "NO";
        this.placeLadderOrders(side);

        // Place Hedge ONCE
        this.recalculateAndHedge();
      }
    }
    // 3. Dynamic Adjustment (User: "Recalculate only when our position changes")
    else {
      this.recalculateAndHedge();
    }
  }

  // 🟢 HELPER: DUST TOLERANCE
  private looseFloat(num: number): number {
    return Math.round(num * 100) / 100;
  }

  // --------------------------------------------------------------------------
  // 🛡️ WATCHDOG & CONNECTION RESILIENCE
  // --------------------------------------------------------------------------

  private async checkConnectionHealth() {
    if (!this.isRunning) return;
    const silenceDuration = Date.now() - this.lastTickTime;

    // 🟢 RULE: If dead, restart SOCKET ONLY. Keep Memory.
    if (silenceDuration > 45000) {
      DashboardManager.log(
        `💀 WATCHDOG: Connection Dead (${(silenceDuration / 1000).toFixed(
          0
        )}s). Resetting Socket...`,
        "ERROR"
      );
      await this.reconnectWebSocket();
    }
  }

  private async reconnectWebSocket() {
    // 🟢 CRITICAL FIX: Guard against empty tokens
    if (!this.tokenIdYes || !this.tokenIdNo) {
      DashboardManager.log("⚠️ Cannot reconnect: Tokens not set yet.", "WARN");
      return;
    }

    try {
      // This only refreshes the data pipe. It does NOT touch filledCountYes/No.
      await this.ws.subscribeMarket(
        [this.tokenIdYes, this.tokenIdNo],
        (data: any) => this.handleMarketTick(data)
      );
      this.lastTickTime = Date.now();
      DashboardManager.log("🔌 WebSocket Reset Successfully!", "INFO");
    } catch (e: any) {
      DashboardManager.log(`❌ Socket Reset Failed: ${e.message}`, "ERROR");
    }
  }

  // 1. ROBUST RESTART LOGIC
  private safeRunLifecycle() {
    this.runLifecycle().catch((err) => {
      DashboardManager.log(`CRITICAL CRASH: ${err.message || err}`, "ERROR");
      setTimeout(() => {
        if (this.isRunning) this.safeRunLifecycle();
      }, 5000);
    });
  }

  private async transitionToExit() {
    // 1. Cancel All Orders
    await this.cancelAllOrders();

    // 2. Report Profit
    const totalCost = this.avgCostYes + this.avgCostNo;
    const hedgedPairs = Math.min(this.filledCountYes, this.filledCountNo);
    const payout = hedgedPairs * 1.0; // $1 per matched pair
    const profit = payout - totalCost;
    const profitPercent = totalCost > 0 ? profit / totalCost : 0;

    DashboardManager.logEvent(
      "SNIPER",
      `🏆 WIN! +$${profit.toFixed(3)} (${this.filledCountYes}Y/${
        this.filledCountNo
      }N)`
    );

    this.phase = "EXIT";

    // 3. LOG TO SPREADSHEET
    const yesAvg = SpreadsheetLogger.calculateAvg(this.yesFills);
    const noAvg = SpreadsheetLogger.calculateAvg(this.noFills);

    const record: TradeRecord = {
      time: new Date().toISOString(),
      marketSlug: this.currentMarketSlug,
      capitalInvested: totalCost,
      profitPercent: profitPercent,
      capitalEnd: totalCost + profit,
      yesPositions: SpreadsheetLogger.formatPositions(this.yesFills),
      yesAvg: yesAvg,
      yesTotal: this.filledCountYes,
      noPositions: SpreadsheetLogger.formatPositions(this.noFills),
      noAvg: noAvg,
      noTotal: this.filledCountNo,
      pnl: profit,
      outcome: profit > 0 ? "WIN" : profit < 0 ? "LOSS" : "BREAKEVEN",
    };

    this.logger.logTrade(record);

    // 4. Force End Cycle
    setTimeout(() => this.endCycleAndRestart(), 1000);
  }

  private async runLifecycle() {
    if (!this.isRunning) return;
    // 🟢 CRITICAL: We do NOT reset state here. State is only reset when cycle ends.

    // 1. Get Current and Next
    const currentTarget = MarketMath.getTargetMarketSlug();
    const nextTarget = MarketMath.getNextMarketSlug();

    DashboardManager.log(
      `🔍 System Start. Checking Market: ${currentTarget.slug.substring(
        0,
        10
      )}...`,
      "INFO"
    );

    // Default target is NEXT (Safety default)
    let target = nextTarget;

    // 1. SMART TARGETING & WALLET CHECK (Quick Peek)
    try {
      const tokens = await this.api.getMarketTokenIds(currentTarget.slug);
      if (tokens.length >= 2) {
        const positions = await this.executor.getPositions(
          tokens[0],
          tokens[1]
        );
        if (positions.yes > 0.1 || positions.no > 0.1) {
          DashboardManager.log(
            `🚨 ACTIVE TRADE FOUND! Forcing Current Market.`,
            "WARN"
          );
          target = currentTarget; // Update local target variable so logic follows this
        }
      }
    } catch (e: any) {
      DashboardManager.log(`⚠️ Startup Check Failed: ${e.message}`, "ERROR");
    }

    this.marketEndTimeMs = target.endTimeMs;
    this.currentMarketSlug = target.slug;

    // 2. WAIT LOGIC (Only waits if we are actually early for the Next market)
    const msUntilStart = MarketMath.getMsUntil(target.startTimeMs);
    const msUntilPrep = msUntilStart - 60000;
    if (msUntilPrep > 0) {
      const waitSecs = Math.round(msUntilPrep / 1000);
      setBotStatus("SniperLadder", `⏳ Waiting ${waitSecs}s`);
      await new Promise((resolve) => setTimeout(resolve, msUntilPrep));
    }

    // 3. GET TOKENS
    const tokenIds = await this.api.getMarketTokenIds(target.slug);
    if (!tokenIds || tokenIds.length < 2) {
      DashboardManager.log(
        `❌ Failed to get tokens for ${target.slug}. Retrying in 10s...`,
        "ERROR"
      );
      setTimeout(() => {
        if (this.isRunning) this.safeRunLifecycle();
      }, 10000);
      return;
    }
    this.tokenIdYes = tokenIds[0];
    this.tokenIdNo = tokenIds[1];
    DashboardManager.logEvent("SNIPER", `✅ Tokens Acquired`);

    // 4. HYDRATE MEMORY
    // 🟢 CRITICAL: Only scan if we are blank. If we have memory, TRUST IT.
    if (this.filledCountYes === 0 && this.filledCountNo === 0) {
      await this.hydrateState();
    } else {
      DashboardManager.log(
        `🧠 Memory Active: [${this.filledCountYes}Y / ${this.filledCountNo}N]. Skipping scan.`,
        "INFO"
      );
    }

    // 5. PLACE TRAPS
    const hasTraps = Array.from(this.activeOrders.values()).some(
      (o) => o.type === "TRAP"
    );
    if (!hasTraps && this.phase === "NEUTRAL") {
      await this.placeTrapOrders();
    } else {
      DashboardManager.log(
        "✅ State Restored. Skipping Initial Traps.",
        "INFO"
      );
    }

    // 6. CONNECT SOCKET (Uses the tokens found in step 3 or 4)
    // 🟢 CRITICAL FIX: Guard against empty tokens
    if (!this.tokenIdYes || !this.tokenIdNo) {
      DashboardManager.log(
        "❌ Cannot subscribe: Tokens not set. Retrying lifecycle...",
        "ERROR"
      );
      setTimeout(() => {
        if (this.isRunning) this.safeRunLifecycle();
      }, 5000);
      return;
    }

    // 🟢 CRITICAL CHANGE: Listen to Market for PRICE ONLY (Not Fills)
    await this.ws.subscribeMarket(
      [this.tokenIdYes, this.tokenIdNo],
      (data: any) => this.handleMarketTick(data)
    );

    // 7. SCHEDULE END
    // 🟢 CRITICAL FIX: Use 'this.marketEndTimeMs' which is updated by hydrateState
    // This ensures if we found shares in the current market, the timer ends in 2 mins, not 15.
    const msUntilEnd = MarketMath.getMsUntil(this.marketEndTimeMs);

    DashboardManager.log(
      `⏰ Timer set for ${(msUntilEnd / 60000).toFixed(1)} minutes`,
      "INFO"
    );

    // 🟢 CRITICAL FIX: Clear any existing timer before setting a new one
    if (this.marketEndTimer) {
      clearTimeout(this.marketEndTimer);
      this.marketEndTimer = null;
    }

    this.marketEndTimer = setTimeout(() => {
      this.marketEndTimer = null; // Clear reference when fired
      this.endCycleAndRestart().catch((err) => {
        DashboardManager.log(`❌ Cycle end error: ${err.message}`, "ERROR");
      });
    }, msUntilEnd + 2000); // 2 second buffer after market close
  }

  private async endCycleAndRestart() {
    setBotStatus("SniperLadder", "🔄 Switching cycle...");
    DashboardManager.logEvent("SNIPER", "🏁 Market ended");
    await this.logIncompletePosition();

    try {
      this.ws.close();
    } catch (e) {
      /* ignore */
    }

    // 🟢 Start token claiming after cycle ends (if not already polling)
    // This will keep polling continuously, checking every 5s for winning tokens
    if (this.tokenClaimer) {
      console.log(
        "[SNIPER] 🎁 Starting automatic token claiming (polling every 5s)..."
      );
      this.tokenClaimer.startPolling();
    }

    // 🟢 CHANGE 4: The only safe place to wipe.
    this.resetState();

    if (this.ONE_CYCLE_ONLY) {
      console.log(`[SNIPER] ✅ Single Cycle Complete.`);
      // Stop claiming before exit
      if (this.tokenClaimer) {
        this.tokenClaimer.stopPolling();
      }
      process.exit(0);
    } else {
      // Keep claiming running - it will continue polling in the background
      // while the next cycle runs
      this.safeRunLifecycle();
    }
  }

  private async logIncompletePosition() {
    const hasPosition = this.filledCountYes > 0 || this.filledCountNo > 0;
    const isBalanced =
      this.filledCountYes === this.filledCountNo && this.filledCountYes > 0;

    if (hasPosition && !isBalanced && this.phase !== "EXIT") {
      const totalCost = this.avgCostYes + this.avgCostNo;
      const hedgedPairs = Math.min(this.filledCountYes, this.filledCountNo);
      const hedgedPayout = hedgedPairs * 1.0;
      const profit = hedgedPayout - totalCost;
      const profitPercent = totalCost > 0 ? profit / totalCost : 0;

      DashboardManager.logEvent(
        "SNIPER",
        `❌ LOSS! PnL: $${profit.toFixed(3)}`
      );

      const record: TradeRecord = {
        time: new Date().toISOString(),
        marketSlug: this.currentMarketSlug,
        capitalInvested: totalCost,
        profitPercent: profitPercent,
        capitalEnd: hedgedPayout,
        yesPositions: SpreadsheetLogger.formatPositions(this.yesFills),
        yesAvg: SpreadsheetLogger.calculateAvg(this.yesFills),
        yesTotal: this.filledCountYes,
        noPositions: SpreadsheetLogger.formatPositions(this.noFills),
        noAvg: SpreadsheetLogger.calculateAvg(this.noFills),
        noTotal: this.filledCountNo,
        pnl: profit,
        outcome: "LOSS",
      };
      this.logger.logTrade(record);
    }
  }

  // 🟢 NEW: Scans BOTH markets (Current & Next) to find where your money is
  private async hydrateState() {
    DashboardManager.log("🧠 Scanning Wallet (Hydration)...", "INFO");

    const currentTarget = MarketMath.getTargetMarketSlug();
    const nextTarget = MarketMath.getNextMarketSlug();
    let foundShares = false;

    // 1. CHECK CURRENT
    try {
      const tokens = await this.api.getMarketTokenIds(currentTarget.slug);
      if (tokens.length >= 2) {
        const pos = await this.executor.getPositions(tokens[0], tokens[1]);
        if (this.looseFloat(pos.yes) > 0.01 || this.looseFloat(pos.no) > 0.01) {
          DashboardManager.log(`🚨 FOUND SHARES IN CURRENT MARKET!`, "WARN");
          this.currentMarketSlug = currentTarget.slug;
          this.marketEndTimeMs = currentTarget.endTimeMs;
          this.tokenIdYes = tokens[0];
          this.tokenIdNo = tokens[1];
          this.filledCountYes = pos.yes;
          this.filledCountNo = pos.no;
          foundShares = true;
        }
      }
    } catch (e) {
      /* ignore */
    }

    // 2. CHECK NEXT
    if (!foundShares) {
      try {
        const tokens = await this.api.getMarketTokenIds(nextTarget.slug);
        if (tokens.length >= 2) {
          const pos = await this.executor.getPositions(tokens[0], tokens[1]);
          if (
            this.looseFloat(pos.yes) > 0.01 ||
            this.looseFloat(pos.no) > 0.01
          ) {
            DashboardManager.log(`✅ Found shares in NEXT market.`, "INFO");
            this.currentMarketSlug = nextTarget.slug;
            this.marketEndTimeMs = nextTarget.endTimeMs;
            this.tokenIdYes = tokens[0];
            this.tokenIdNo = tokens[1];
            this.filledCountYes = pos.yes;
            this.filledCountNo = pos.no;
            foundShares = true;
          }
        }
      } catch (e) {
        /* ignore */
      }
    }

    // 3. Fallback Cost Estimation
    if (this.filledCountYes > 0 && this.avgCostYes === 0)
      this.avgCostYes = this.filledCountYes * 0.5;
    if (this.filledCountNo > 0 && this.avgCostNo === 0)
      this.avgCostNo = this.filledCountNo * 0.5;

    // 4. RECOVER ORDERS (Adopt Ghost Orders)
    if (this.currentMarketSlug) {
      try {
        if (this.executor.getOpenOrders) {
          const openOrders = await this.executor.getOpenOrders(
            this.currentMarketSlug
          );
          this.activeOrders.clear();

          for (const order of openOrders) {
            let type: "TRAP" | "LADDER" | "HEDGE" = "TRAP";
            if (parseFloat(order.price) < 0.4) type = "LADDER";

            this.activeOrders.set(order.id, {
              side: order.side as "YES" | "NO",
              price: parseFloat(order.price),
              type: type,
            });
          }
        }
      } catch (e) {
        /* ignore */
      }
    }

    if (this.filledCountYes > 0 || this.filledCountNo > 0) {
      DashboardManager.log(
        `📝 MEMORY UPDATED: ${this.filledCountYes}Y / ${this.filledCountNo}N (from Scan)`,
        "WARN"
      );
      if (this.phase === "NEUTRAL") this.phase = "ACCUMULATION";
    } else {
      DashboardManager.log("🤷 Scan Complete. No active shares.", "INFO");
    }
  }

  // --------------------------------------------------------------------------
  // 🔥 CORE LOGIC: ORDER HANDLING & HEDGING
  // --------------------------------------------------------------------------

  private async checkFills() {
    const orderIds = Array.from(this.activeOrders.keys());

    for (const orderId of orderIds) {
      const orderInfo = this.activeOrders.get(orderId);
      if (!orderInfo) continue;

      try {
        const status = await this.executor.getOrderStatus(orderId);

        // 🟢 ROOT CAUSE FIX: DELTA TRACKING
        // 1. Get the TOTAL matched amount from API
        const totalMatched = status.matched || 0;

        // 2. Get the amount we ALREADY processed (default to 0)
        const previouslyProcessed = this.orderFillHistory.get(orderId) || 0;

        // 3. Calculate the NEW amount (The Delta)
        const newFillAmount = totalMatched - previouslyProcessed;

        // 4. Only process if there is ACTUALLY new stuff
        if (newFillAmount > 0.0001) {
          const fillPrice =
            status.avgFillPrice > 0 ? status.avgFillPrice : orderInfo.price;

          // 5. Update our history so we don't process this chunk again
          this.orderFillHistory.set(orderId, totalMatched);

          // 6. Pass ONLY the new amount to the handler
          await this.handleOrderFilled(
            orderId,
            orderInfo,
            fillPrice,
            newFillAmount
          );
        }
      } catch (e: any) {
        DashboardManager.log(
          `⚠️ Error checking order ${orderId}: ${e.message}`,
          "WARN"
        );
      }
    }
  }

  private async handleOrderFilled(
    orderId: string,
    orderInfo: any,
    fillPrice: number,
    filledSize: number
  ) {
    const rawMsg = `💥 ${orderInfo.side} ${
      orderInfo.type
    } @ $${fillPrice.toFixed(3)} (x${filledSize.toFixed(2)})`;
    DashboardManager.logEvent("SNIPER", rawMsg);

    // We update memory here too as a backup to WS
    if (orderInfo.side === "YES") {
      this.filledCountYes += filledSize;
      this.avgCostYes += fillPrice * filledSize;
      this.yesFills.push({ price: fillPrice, size: filledSize });
      this.lastTradeTime = Date.now(); // 🟢 Track trade time
    } else {
      this.filledCountNo += filledSize;
      this.avgCostNo += fillPrice * filledSize;
      this.noFills.push({ price: fillPrice, size: filledSize });
      this.lastTradeTime = Date.now(); // 🟢 Track trade time
    }

    // 2. CHECK PHASE TRANSITION
    if (this.phase === "NEUTRAL") {
      // 🟢 ACTION 1: IMMEDIATE DEFENSE (Kill the Opposite Trap)
      // Prevent double exposure immediately upon any fill.
      const loserSide = orderInfo.side === "YES" ? "NO" : "YES";
      await this.cancelOrdersByType(loserSide, "TRAP");

      // 🟢 DUST TOLERANCE check
      const currentSideCount =
        orderInfo.side === "YES" ? this.filledCountYes : this.filledCountNo;

      if (this.looseFloat(currentSideCount) < this.LEG_THRESHOLD) {
        DashboardManager.log(
          `🛡️ Trap Hit. Holding Dust (${currentSideCount.toFixed(
            2
          )}). Waiting...`,
          "INFO"
        );
        return;
      }

      DashboardManager.log(`🦵 LEG COMPLETED. LADDER ACTIVATING!`, "TRADE");

      this.firstFillTime = Date.now();
      this.phase = "TRIGGERED";
      await this.transitionToTriggered(orderInfo.side);
    } else {
      // Victory Check
      if (
        this.filledCountYes > 0.1 &&
        Math.abs(this.filledCountYes - this.filledCountNo) < 0.1
      ) {
        DashboardManager.log("⚖️ PERFECT BALANCE. CANCELLING ALL...", "TRADE");
        await this.transitionToExit();
        return;
      }

      // Logic for fills during active phases
      await this.recalculateAndHedge();
    }
  }

  // --------------------------------------------------------------------------
  // 🟢 3. THE STRATEGIST (Calculates & Places ONE Order)
  // --------------------------------------------------------------------------
  private async recalculateAndHedge() {
    if (this.isHedging) return;
    this.isHedging = true;

    try {
      let aggressorSide: "YES" | "NO";
      let defenderSide: "YES" | "NO";
      let aggressorCount = 0;
      let defenderCount = 0;

      // Identify who is winning
      if (this.filledCountYes > this.filledCountNo) {
        aggressorSide = "YES";
        aggressorCount = this.filledCountYes;
        defenderSide = "NO";
        defenderCount = this.filledCountNo;
      } else if (this.filledCountNo > this.filledCountYes) {
        aggressorSide = "NO";
        aggressorCount = this.filledCountNo;
        defenderSide = "YES";
        defenderCount = this.filledCountYes;
      } else {
        return;
      } // Balanced

      const msUntilEnd = this.marketEndTimeMs - Date.now();
      if (msUntilEnd < this.PIVOT_TIME_BEFORE_END_MS) return;

      // Pre-Hedge Audit for huge gaps
      let sharesNeeded = aggressorCount - defenderCount;

      // 🚨 CRITICAL FIX: Always verify sharesNeeded matches actual position
      // If there's a mismatch, force a sync to get accurate counts
      const actualGap = Math.abs(this.filledCountYes - this.filledCountNo);
      if (Math.abs(sharesNeeded - actualGap) > 0.5) {
        DashboardManager.log(
          `⚠️ MISMATCH: Calculated gap ${sharesNeeded.toFixed(
            1
          )} but actual gap is ${actualGap.toFixed(1)}. Syncing positions...`,
          "WARN"
        );
        await this.syncPositions();
        // Recalculate after sync
        if (this.filledCountYes > this.filledCountNo) {
          aggressorCount = this.filledCountYes;
          defenderCount = this.filledCountNo;
        } else if (this.filledCountNo > this.filledCountYes) {
          aggressorCount = this.filledCountNo;
          defenderCount = this.filledCountYes;
        }
        sharesNeeded = aggressorCount - defenderCount;
        DashboardManager.log(
          `✅ After sync: ${this.filledCountYes}Y / ${
            this.filledCountNo
          }N → need ${sharesNeeded.toFixed(1)} shares`,
          "INFO"
        );
      }

      if (sharesNeeded > 10 && Date.now() - this.lastSyncTime > 5000) {
        await this.syncPositions();
        return; // Let the next tick handle it after sync
      }

      // 🟢 INITIAL HEDGE CHECK: Check BEFORE canceling orders
      const isInitialHedge = this.phase === "TRIGGERED";

      if (!isInitialHedge) {
        this.phase = "ACCUMULATION";
      }

      if (Math.round(sharesNeeded * 100) / 100 <= 0.1) return;

      // 🟢 CRITICAL FIX: Deadband to prevent infinite hedge loop
      // If gap is < 2 shares, ignore it (too small to trade efficiently)
      if (sharesNeeded < 2) {
        DashboardManager.log(
          `🛡️ Gap too small (${sharesNeeded.toFixed(
            1
          )}). Ignoring to prevent ping-pong.`,
          "INFO"
        );
        return;
      }

      // Budget Calc
      // Profit margin is on TOTAL TRANSACTION, not per share
      // Anchor target: 2.0% profit margin (adjusted from 2.5%)
      const anchorProfitMargin = 0.02; // 2.0% (anchor target)
      const totalRevenueTarget = aggressorCount * 1.0;
      const totalSunkCost = this.avgCostYes + this.avgCostNo;

      // Anchor: 2.0% profit margin on total transaction
      const totalCostAllowed = totalRevenueTarget / (1 + anchorProfitMargin);
      const remainingBudget = totalCostAllowed - totalSunkCost;

      // 🟢 MINIMUM ORDER SIZE FIX
      // Only round up to 5 if gap is legitimately 2-4 shares
      // (Gaps < 2 are already handled by deadband above)
      if (sharesNeeded < 5) {
        console.log(
          `[HEDGE] Gap is ${sharesNeeded.toFixed(
            2
          )}. Rounding to 5 to force execution.`
        );
        sharesNeeded = 5;
      }

      // 🟢 INITIAL HEDGE: Use fixed $0.52 for first hedge (after trap at $0.45)
      let anchorPrice: number;
      if (isInitialHedge) {
        // Initial hedge: use fixed $0.52
        anchorPrice = 0.52;
        DashboardManager.log(
          `🎯 INITIAL HEDGE: Using fixed price $0.52 (Trap was $${this.INITIAL_TRAP_PRICE})`,
          "INFO"
        );
      } else {
        // Subsequent hedges: calculate based on budget
        let rawPrice = remainingBudget / sharesNeeded;
        anchorPrice = Math.max(
          0.01,
          Math.min(0.99, Math.floor(rawPrice * 100) / 100)
        );
      }

      // 🟢 Apply break-even guard for subsequent hedges
      if (!isInitialHedge) {
        const breakEvenPrice = 1.0 - this.INITIAL_TRAP_PRICE;
        if (anchorPrice > breakEvenPrice) {
          anchorPrice = Math.min(anchorPrice, breakEvenPrice);
        }
      }

      const tokenId = defenderSide === "YES" ? this.tokenIdYes : this.tokenIdNo;

      // 🟢 TRACK: Count currently active (unfilled) hedge orders before canceling
      // This tells us how many orders to replace (if we placed 3 and 1 filled, we have 2 active = place 2 new)
      const activeHedgeOrders = Array.from(this.activeOrders.values()).filter(
        (o) => o.side === defenderSide && o.type === "HEDGE"
      ).length;

      // 🟢 LOGIC: CANCEL OLD -> PLACE NEW (Adjusting to new fill level)
      await this.cancelOrdersByType(defenderSide, "HEDGE");

      // 🟢 DYNAMIC ORDER DISTRIBUTION LOGIC
      // ALWAYS calculate based on NEW sharesNeeded, not just replace unfilled orders
      // The sharesNeeded might have changed (e.g., ladder filled, increasing gap)
      let ordersToPlace: number;
      const calculatedOrders = this.calculateOrdersNeeded(sharesNeeded);

      // 🚨 CRITICAL FIX: Always use calculatedOrders, never use Math.max with activeHedgeOrders
      // The activeHedgeOrders count might be stale if orders filled but state wasn't updated
      // We MUST place enough orders to cover the actual gap
      ordersToPlace = calculatedOrders;

      if (activeHedgeOrders > 0) {
        DashboardManager.log(
          `🔄 RECALCULATING: Had ${activeHedgeOrders} unfilled orders, need ${sharesNeeded.toFixed(
            1
          )} shares → placing ${ordersToPlace} orders (${
            ordersToPlace * 5
          } shares total)`,
          "INFO"
        );
      } else {
        DashboardManager.log(
          `🆕 INITIAL PLACEMENT: Calculating ${ordersToPlace} orders for ${sharesNeeded.toFixed(
            1
          )} shares (${ordersToPlace * 5} shares total)`,
          "INFO"
        );
      }

      // 🚨 SAFETY CHECK: Verify we're placing enough shares
      const totalSharesToPlace = ordersToPlace * 5;
      if (totalSharesToPlace < sharesNeeded - 0.5) {
        DashboardManager.log(
          `⚠️ WARNING: Placing ${totalSharesToPlace} shares but need ${sharesNeeded.toFixed(
            1
          )}. This may be insufficient!`,
          "WARN"
        );
      }

      // 🟢 DYNAMIC HEDGE LADDER: Calculate ONLY the price levels we need
      // We calculate prices based on how many orders we're placing
      let mostFarPrice: number = anchorPrice;
      let mostNearPrice: number = anchorPrice;

      // Only calculate additional price levels if we need more than 1 order
      if (ordersToPlace > 1) {
        if (isInitialHedge) {
          // For initial hedge, use fixed spread around $0.52
          const profitMarginSpread = 0.025; // 2.5%
          mostFarPrice = Math.max(0.01, anchorPrice * (1 - profitMarginSpread));
          mostNearPrice = Math.min(
            0.99,
            anchorPrice * (1 + profitMarginSpread)
          );
        } else {
          // For subsequent hedges, calculate based on TOTAL TRANSACTION profit margin
          // New spread: 1.5% (mostFar) → 2.0% (anchor) → 2.5% (mostNear)
          // Average if all fill: ~1.75% (between 1.5% and 2.5%)

          // Most Near: 2.5% profit margin (conservative, most expensive)
          // Always calculate mostNear (needed for 2 orders)
          const profitMarginNear = 0.025; // 2.5%
          const totalCostAllowedNear =
            totalRevenueTarget / (1 + profitMarginNear);
          const remainingBudgetNear = totalCostAllowedNear - totalSunkCost;
          mostNearPrice = Math.max(
            0.01,
            Math.min(0.99, remainingBudgetNear / sharesNeeded)
          );

          // Most Far: 1.5% profit margin (aggressive, cheapest)
          // Only needed for 3+ orders
          if (ordersToPlace >= 3) {
            const profitMarginFar = 0.015; // 1.5%
            const totalCostAllowedFar =
              totalRevenueTarget / (1 + profitMarginFar);
            const remainingBudgetFar = totalCostAllowedFar - totalSunkCost;
            mostFarPrice = Math.max(
              0.01,
              Math.min(0.99, remainingBudgetFar / sharesNeeded)
            );
          } else {
            // For 2 orders, set mostFar = anchor (won't be used, but needed for function signature)
            mostFarPrice = anchorPrice;
          }

          // Apply break-even guard
          const breakEvenPrice = 1.0 - this.INITIAL_TRAP_PRICE;
          mostFarPrice = Math.min(mostFarPrice, breakEvenPrice);
          mostNearPrice = Math.min(mostNearPrice, breakEvenPrice);
          anchorPrice = Math.min(anchorPrice, breakEvenPrice);
        }
      }

      const hedgeLadder = this.calculateHedgeLadder(
        sharesNeeded,
        anchorPrice,
        mostFarPrice,
        mostNearPrice,
        ordersToPlace
      );

      DashboardManager.log(
        `🛡️ HEDGE LADDER: Need ${sharesNeeded} ${defenderSide} | Placing ${hedgeLadder.length} order(s)`,
        "INFO"
      );

      // Place hedge ladder orders
      for (const level of hedgeLadder) {
        await this.placeHedgeOrder(
          defenderSide,
          tokenId,
          level.price,
          level.size,
          !isInitialHedge,
          isInitialHedge
        );
        await new Promise((r) => setTimeout(r, 150)); // Slight delay to prevent rate limit
      }
    } catch (e: any) {
      DashboardManager.log(`❌ Hedge Logic Error: ${e.message}`, "ERROR");
    } finally {
      this.isHedging = false;
    }
  }

  // 🟢 HELPER: Calculate how many orders needed based on shares
  private calculateOrdersNeeded(sharesNeeded: number): number {
    if (sharesNeeded < 10) {
      // Rule C: Below 10 shares - ONE order
      return 1;
    } else if (sharesNeeded % 5 === 0) {
      // Rule A: Divisible by 5 - distribute evenly (e.g., 15 → 3 orders of 5)
      return sharesNeeded / 5;
    } else {
      // Rule B: Not divisible by 5 AND > 10 - (5 + remainder) = 2 orders
      return 2;
    }
  }

  // 🟢 DYNAMIC HEDGE LADDER CALCULATOR
  private calculateHedgeLadder(
    sharesNeeded: number,
    anchorPrice: number,
    mostFarPrice: number,
    mostNearPrice: number,
    ordersToPlace: number
  ): Array<{ price: number; size: number }> {
    const ladder: Array<{ price: number; size: number }> = [];

    // Calculate sizes for each order
    if (ordersToPlace === 1) {
      // Single order: place at anchor price
      ladder.push({ price: anchorPrice, size: sharesNeeded });
    } else if (ordersToPlace === 2) {
      // Two orders: Use anchor (2.5%) and mostNear (0% break-even)
      // This is less aggressive than mostFar (5%) and more likely to fill
      // Average will be closer to 2.5% if both fill
      const remainder = sharesNeeded % 5;
      const baseSize = 5;
      if (remainder > 0) {
        // Put bigger size on anchor (2.5%), smaller on mostNear (0%)
        ladder.push({ price: anchorPrice, size: remainder + baseSize }); // Bigger on anchor
        ladder.push({ price: mostNearPrice, size: baseSize }); // Smaller on mostNear
      } else {
        // Exactly divisible by 5, but only 2 orders (e.g., 10 shares)
        ladder.push({ price: anchorPrice, size: baseSize }); // Anchor first
        ladder.push({ price: mostNearPrice, size: baseSize }); // MostNear second
      }
    } else {
      // Three or more orders: distribute evenly (5-5-5 pattern)
      const sizePerOrder = Math.floor(sharesNeeded / ordersToPlace);
      const remainder = sharesNeeded % ordersToPlace;

      // Distribute evenly across 3 price levels
      const prices = [mostFarPrice, anchorPrice, mostNearPrice];
      for (let i = 0; i < ordersToPlace; i++) {
        let size = sizePerOrder;
        // Add remainder to first order if needed
        if (i === 0 && remainder > 0) {
          size += remainder;
        }
        const priceIndex = i % 3; // Cycle through price levels
        ladder.push({ price: prices[priceIndex], size });
      }
    }

    return ladder;
  }

  // 🟢 UPDATED: PROFIT GUARD + SAFETY VALVE
  private async placeHedgeOrder(
    side: "YES" | "NO",
    tokenId: string,
    price: number,
    size: number,
    useAggressiveBuffer: boolean,
    isInitialHedge: boolean = false
  ) {
    let finalPrice = price;

    if (useAggressiveBuffer) {
      // 🟢 FIX 1: Max 1 cent aggression
      finalPrice = Math.min(0.99, price + 0.01);
    }

    // 🟢 FIX 2: PROFIT GUARD (Break Even Cap) - ONLY for subsequent hedges
    // Initial hedge stays at $0.52, no cap applied
    if (!isInitialHedge) {
      // Never pay more than (1.00 - Entry). E.g. If Entry 0.45, Max Hedge 0.55.
      const breakEvenPrice = 1.0 - this.INITIAL_TRAP_PRICE;
      if (finalPrice > breakEvenPrice) {
        // Allow a tiny 0.01 loss only if we are desperate (dynamic), otherwise CAP IT.
        if (finalPrice > breakEvenPrice + 0.01) {
          DashboardManager.log(
            `⚠️ CAPPING PRICE: $${finalPrice.toFixed(
              2
            )} -> $${breakEvenPrice.toFixed(2)} to preserve capital.`,
            "WARN"
          );
          finalPrice = breakEvenPrice;
        }
      }
    }

    DashboardManager.log(
      `📤 SENDING: ${side} HEDGE ${size} @ $${finalPrice.toFixed(2)} (GTC)`,
      "INFO"
    );

    try {
      const id = await this.executor.placeOrder({
        tokenId,
        side: "BUY",
        price: finalPrice,
        size,
        type: "GTC",
      });

      this.activeOrders.set(id, { side, type: "HEDGE", price: finalPrice });
    } catch (e: any) {
      DashboardManager.log(`❌ HEDGE FAILED: ${e.message}`, "ERROR");
      // 🟢 FIX 3: Trigger Safety Valve on critical errors
      await this.handleExecutionError(e, side);
    }
  }

  // 🟢 NEW: SAFETY VALVE (If Blockchain says we are broke, Wipe Memory)
  private async handleExecutionError(error: any, side: "YES" | "NO") {
    const msg = error.message || JSON.stringify(error);
    const isBalanceError =
      msg.includes("balance") ||
      msg.includes("insufficient") ||
      msg.includes("allowance");

    if (isBalanceError) {
      DashboardManager.log(
        `🚨 CRITICAL: Blockchain rejected order (${msg}). Memory likely wrong. FORCING SYNC.`,
        "ERROR"
      );
      try {
        const positions = await this.executor.getPositions(
          this.tokenIdYes,
          this.tokenIdNo
        );
        this.filledCountYes = positions.yes;
        this.filledCountNo = positions.no;
        DashboardManager.log(
          `🔄 HARD RESET: Synced to Reality (Y:${this.filledCountYes.toFixed(
            1
          )}/N:${this.filledCountNo.toFixed(1)})`,
          "WARN"
        );
      } catch (e) {
        /* ignore */
      }
    }
  }

  private async transitionToTriggered(winnerSide: "YES" | "NO") {
    DashboardManager.log(`⚡ PHASE 2: TRIGGERED by ${winnerSide}`, "TRADE");
    await this.placeLadderOrders(winnerSide);
    await this.recalculateAndHedge();
  }

  private async placeLadderOrders(side: "YES" | "NO") {
    // 🟢 CRITICAL FIX: Guard against empty tokens
    if (!this.tokenIdYes || !this.tokenIdNo) {
      DashboardManager.log(
        "❌ Cannot place ladders: Tokens not set yet.",
        "ERROR"
      );
      return;
    }

    const currentSpread = this.priceYes + this.priceNo - 1.0;

    // 🟢 SPREAD GUARD: LOG IT IF IT BLOCKS
    if (currentSpread > this.MAX_SPREAD) {
      DashboardManager.log(
        `⚠️ SKIPPING LADDERS: Spread ${currentSpread.toFixed(3)} > ${
          this.MAX_SPREAD
        }`,
        "WARN"
      );
      return;
    }

    DashboardManager.log(`🪜 Placing LADDER for ${side}...`, "INFO");
    const tokenId = side === "YES" ? this.tokenIdYes : this.tokenIdNo;

    for (const level of this.LADDER_LEVELS) {
      try {
        DashboardManager.log(
          `📤 SENDING: LADDER ${side} ${level.size} @ $${level.price} (GTC)`,
          "INFO"
        );
        const id = await this.executor.placeOrder({
          tokenId,
          side: "BUY",
          price: level.price,
          size: level.size,
          type: "GTC",
        });
        this.activeOrders.set(id, { side, price: level.price, type: "LADDER" });
      } catch (e: any) {
        const msg = e.message || JSON.stringify(e);
        // 🟢 REALITY CHECK
        if (msg.includes("balance") || msg.includes("allowance")) {
          DashboardManager.log(
            `💸 WALLET EMPTY during Ladder. Syncing...`,
            "WARN"
          );
          await this.syncPositions();
          continue;
        }
        DashboardManager.log(`❌ LADDER FAILED: ${msg}`, "ERROR");
        await this.handleExecutionError(e, side);
      }
    }
    setBotStatus("SniperLadder", `🪜 Ladder Active (${side})`);
  }

  // --- BOILERPLATE HELPERS ---

  private async cancelOrdersByType(
    side: "YES" | "NO",
    type: "TRAP" | "LADDER" | "HEDGE"
  ) {
    const toCancel: string[] = [];
    this.activeOrders.forEach((val, key) => {
      if (val.side === side && val.type === type) toCancel.push(key);
    });
    for (const id of toCancel) {
      try {
        await this.executor.cancelOrder(id);
        this.activeOrders.delete(id);
      } catch (e) {
        /* ignore */
      }
    }
  }

  private async cancelAllOrders() {
    const ids = Array.from(this.activeOrders.keys());
    for (const id of ids) {
      try {
        await this.executor.cancelOrder(id);
        this.activeOrders.delete(id);
      } catch (e) {
        /* ignore */
      }
    }
  }

  private async placeTrapOrders() {
    // 🟢 CRITICAL FIX: Guard against empty tokens
    if (!this.tokenIdYes || !this.tokenIdNo) {
      DashboardManager.log(
        "❌ Cannot place traps: Tokens not set yet.",
        "ERROR"
      );
      return;
    }

    try {
      DashboardManager.log(
        `🪤 Placing TRAP orders @ $${this.INITIAL_TRAP_PRICE}...`,
        "INFO"
      );
      const size = this.INITIAL_TRAP_SIZE;

      DashboardManager.log(
        `📤 SENDING: TRAP YES/NO ${size} @ $${this.INITIAL_TRAP_PRICE} (GTC)`,
        "INFO"
      );

      const idYes = await this.executor.placeOrder({
        tokenId: this.tokenIdYes,
        side: "BUY",
        price: this.INITIAL_TRAP_PRICE,
        size: size,
        type: "GTC",
      });
      const idNo = await this.executor.placeOrder({
        tokenId: this.tokenIdNo,
        side: "BUY",
        price: this.INITIAL_TRAP_PRICE,
        size: size,
        type: "GTC",
      });

      this.activeOrders.set(idYes, {
        side: "YES",
        price: this.INITIAL_TRAP_PRICE,
        type: "TRAP",
      });
      this.activeOrders.set(idNo, {
        side: "NO",
        price: this.INITIAL_TRAP_PRICE,
        type: "TRAP",
      });
      setBotStatus("SniperLadder", `🪤 Trap Set ($${this.INITIAL_TRAP_PRICE})`);
    } catch (e: any) {
      DashboardManager.log(`❌ TRAP FAILED: ${e.message}`, "ERROR");
    }
  }

  private updatePrices(data: any) {
    const updates =
      data.price_changes || data.updates || (Array.isArray(data) ? data : []);
    if (data.asks) {
      if (data.asset_id === this.tokenIdYes) {
        const newPrice = parseFloat(data.asks[0]?.price || "0");
        if (newPrice > 0) {
          this.priceYes = newPrice;
          // 🚨 Track price history for panic mode validation
          this.priceHistoryYes.push(newPrice);
          if (this.priceHistoryYes.length > this.PRICE_HISTORY_LENGTH) {
            this.priceHistoryYes.shift();
          }
        }
      }
      if (data.asset_id === this.tokenIdNo) {
        const newPrice = parseFloat(data.asks[0]?.price || "0");
        if (newPrice > 0) {
          this.priceNo = newPrice;
          // 🚨 Track price history for panic mode validation
          this.priceHistoryNo.push(newPrice);
          if (this.priceHistoryNo.length > this.PRICE_HISTORY_LENGTH) {
            this.priceHistoryNo.shift();
          }
        }
      }
    }
    for (const update of updates) {
      if (!update.price) continue;
      const assetId = update.asset_id || data.asset_id;
      const price = parseFloat(update.price);
      if (update.side === "SELL" && price > 0) {
        if (assetId === this.tokenIdYes) {
          this.priceYes = price;
          // 🚨 Track price history
          this.priceHistoryYes.push(price);
          if (this.priceHistoryYes.length > this.PRICE_HISTORY_LENGTH) {
            this.priceHistoryYes.shift();
          }
        }
        if (assetId === this.tokenIdNo) {
          this.priceNo = price;
          // 🚨 Track price history
          this.priceHistoryNo.push(price);
          if (this.priceHistoryNo.length > this.PRICE_HISTORY_LENGTH) {
            this.priceHistoryNo.shift();
          }
        }
      }
    }
  }

  // 🚨 PANIC MODE 1: PARTIAL SELL (Unbalanced positions)
  private async checkPartialSell() {
    if (this.phase === "EXIT" || this.partialSellExecuted) return;

    // Guard against empty tokens
    if (!this.tokenIdYes || !this.tokenIdNo) {
      return;
    }

    // Condition: Both sides have positions, but unbalanced (NOT SUPER STRICT!)
    // Account for 12.999 vs 13 (use looseFloat for comparison)
    const hasYes = this.looseFloat(this.filledCountYes) > 0.1;
    const hasNo = this.looseFloat(this.filledCountNo) > 0.1;
    const imbalance = Math.abs(
      this.looseFloat(this.filledCountYes) - this.looseFloat(this.filledCountNo)
    );

    if (hasYes && hasNo && imbalance > 0.5) {
      // Use 0.5 threshold (not super strict - accounts for 12.999 vs 13)
      DashboardManager.log(
        `🚨 PANIC MODE 1: PARTIAL SELL | Unbalanced: ${this.filledCountYes.toFixed(
          1
        )}Y / ${this.filledCountNo.toFixed(1)}N (imbalance: ${imbalance.toFixed(
          2
        )})`,
        "WARN"
      );

      // Cancel ALL orders
      await this.cancelAllOrders();

      // Sell excess to balance
      const sideToSell =
        this.filledCountYes > this.filledCountNo ? "YES" : "NO";
      const tokenId = sideToSell === "YES" ? this.tokenIdYes : this.tokenIdNo;
      const excessAmount = imbalance;

      DashboardManager.log(
        `📤 SELLING ${excessAmount.toFixed(
          2
        )} ${sideToSell} to balance positions`,
        "WARN"
      );

      try {
        await this.executor.placeOrder({
          tokenId: tokenId,
          side: "SELL",
          price: 0.01, // Market sell
          size: excessAmount,
          type: "GTC",
        });
        // Update memory (will be synced on next fill)
        if (sideToSell === "YES") {
          this.filledCountYes -= excessAmount;
        } else {
          this.filledCountNo -= excessAmount;
        }
        this.partialSellExecuted = true;
      } catch (e: any) {
        DashboardManager.log(`❌ PARTIAL SELL FAILED: ${e.message}`, "ERROR");
      }
    }
  }

  // 🚨 PANIC MODE 2: FULL SELL (Naked position)
  private async checkFullSell() {
    if (this.phase === "EXIT" || this.fullSellExecuted) return;

    // Guard against empty tokens
    if (!this.tokenIdYes || !this.tokenIdNo) {
      return;
    }

    // Condition: Naked position (one side has position, other is zero)
    const hasYes = this.looseFloat(this.filledCountYes) > 0.1;
    const hasNo = this.looseFloat(this.filledCountNo) > 0.1;
    const isNaked = (hasYes && !hasNo) || (!hasYes && hasNo);

    if (isNaked) {
      // Validate prices over 60-100 tick history to avoid WebSocket errors
      const validHistoryYes = this.priceHistoryYes.filter((p) => p > 0);
      const validHistoryNo = this.priceHistoryNo.filter((p) => p > 0);

      if (validHistoryYes.length < 60 || validHistoryNo.length < 60) {
        DashboardManager.log(
          `⚠️ PANIC MODE: Insufficient price history (${validHistoryYes.length}Y / ${validHistoryNo.length}N). Waiting...`,
          "WARN"
        );
        return; // Wait for more price data
      }

      // Calculate average prices over history
      const avgPriceYes =
        validHistoryYes.reduce((a, b) => a + b, 0) / validHistoryYes.length;
      const avgPriceNo =
        validHistoryNo.reduce((a, b) => a + b, 0) / validHistoryNo.length;

      DashboardManager.log(
        `🚨 PANIC MODE 2: FULL SELL | Naked Position: ${this.filledCountYes.toFixed(
          1
        )}Y / ${this.filledCountNo.toFixed(1)}N`,
        "WARN"
      );
      DashboardManager.log(
        `   Avg Prices: YES $${avgPriceYes.toFixed(
          3
        )} / NO $${avgPriceNo.toFixed(3)}`,
        "WARN"
      );

      // Determine which side we're holding
      let holdingSide: "YES" | "NO" | null = null;
      let holdingAmount = 0;
      let isWinningSide = false;

      if (hasYes && !hasNo) {
        holdingSide = "YES";
        holdingAmount = this.filledCountYes;
        isWinningSide = avgPriceYes > avgPriceNo; // Higher price = winning
      } else if (hasNo && !hasYes) {
        holdingSide = "NO";
        holdingAmount = this.filledCountNo;
        isWinningSide = avgPriceNo > avgPriceYes; // Higher price = winning
      }

      if (holdingSide && holdingAmount > 0) {
        if (isWinningSide) {
          // HOLD: We're holding the winning side
          const holdingPrice = holdingSide === "YES" ? avgPriceYes : avgPriceNo;
          const otherPrice = holdingSide === "YES" ? avgPriceNo : avgPriceYes;
          DashboardManager.log(
            `✅ HOLDING: ${holdingSide} is WINNING ($${holdingPrice.toFixed(
              3
            )} > $${otherPrice.toFixed(3)}). Keeping position.`,
            "INFO"
          );
          this.fullSellExecuted = true; // Mark as executed to prevent re-triggering
        } else {
          // SELL ALL: We're holding the losing side
          const holdingPrice = holdingSide === "YES" ? avgPriceYes : avgPriceNo;
          const otherPrice = holdingSide === "YES" ? avgPriceNo : avgPriceYes;
          DashboardManager.log(
            `📤 SELLING ALL: ${holdingSide} is LOSING ($${holdingPrice.toFixed(
              3
            )} < $${otherPrice.toFixed(3)}). Dumping ${holdingAmount.toFixed(
              2
            )} shares.`,
            "WARN"
          );

          // Cancel ALL orders
          await this.cancelAllOrders();

          const tokenId =
            holdingSide === "YES" ? this.tokenIdYes : this.tokenIdNo;

          try {
            await this.executor.placeOrder({
              tokenId: tokenId,
              side: "SELL",
              price: 0.01, // Market sell
              size: holdingAmount,
              type: "GTC",
            });
            // Update memory
            if (holdingSide === "YES") {
              this.filledCountYes = 0;
            } else {
              this.filledCountNo = 0;
            }
            this.fullSellExecuted = true;
          } catch (e: any) {
            DashboardManager.log(`❌ FULL SELL FAILED: ${e.message}`, "ERROR");
          }
        }
      }
    }
  }

  private async executeLiquidityEscape() {
    if (this.phase === "EXIT") return;

    // 🟢 CRITICAL FIX: Guard against empty tokens
    if (!this.tokenIdYes || !this.tokenIdNo) {
      DashboardManager.log(
        "⚠️ Cannot execute escape: Tokens not set yet.",
        "WARN"
      );
      return;
    }

    const msUntilEnd = this.marketEndTimeMs - Date.now();

    if (msUntilEnd > 60000) return;

    // 🟢 DIAMOND HANDS (No Stop Loss).

    const netExposure = Math.abs(this.filledCountYes - this.filledCountNo);
    if (this.looseFloat(netExposure) > 5) {
      const sideToSell =
        this.filledCountYes > this.filledCountNo ? "YES" : "NO";
      const tokenId = sideToSell === "YES" ? this.tokenIdYes : this.tokenIdNo;
      const currentPrice = sideToSell === "YES" ? this.priceYes : this.priceNo;

      if (currentPrice > 0.05) {
        DashboardManager.log(
          `📤 SENDING: DUMP ${netExposure} ${sideToSell} (Risk Reduce)`,
          "WARN"
        );
        try {
          await this.executor.placeOrder({
            tokenId: tokenId,
            side: "SELL",
            price: 0.01,
            size: netExposure,
            type: "GTC",
          });
          if (sideToSell === "YES") this.filledCountYes -= netExposure;
          else this.filledCountNo -= netExposure;
        } catch (e: any) {
          DashboardManager.log(`❌ DUMP FAILED: ${e.message}`, "ERROR");
        }
      }
    }
  }

  // 🟢 CHANGE 3: THIS IS THE ONLY FUNCTION THAT WIPES MEMORY
  // It is ONLY called by endCycleAndRestart()
  private resetState() {
    this.phase = "NEUTRAL";
    this.firstFillTime = 0;
    this.activeOrders.clear();
    this.orderFillHistory.clear();
    this.filledCountYes = 0;
    this.filledCountNo = 0;
    this.avgCostYes = 0;
    this.avgCostNo = 0;
    this.priceYes = 0;
    this.priceNo = 0;
    this.marketEndTimeMs = 0;
    this.currentMarketSlug = "";
    this.yesFills = [];
    this.noFills = [];
    this.lastLogTime = 0;
    this.zeroDataStreak = 0;
    this.processedMatchIds.clear();
    // 🟢 CRITICAL FIX: Reset hedging state
    this.isHedging = false;
    // 🚨 Reset panic mode state
    this.partialSellExecuted = false;
    this.fullSellExecuted = false;
    this.priceHistoryYes = [];
    this.priceHistoryNo = [];
    // 🟢 Reset trade time tracking
    this.lastTradeTime = 0;
    // Clear intervals and timers (shouldn't be needed here, but safety)
    if (this.healthCheckInterval) {
      clearInterval(this.healthCheckInterval);
      this.healthCheckInterval = null;
    }
    if (this.syncInterval) {
      clearInterval(this.syncInterval);
      this.syncInterval = null;
    }
    if (this.marketEndTimer) {
      clearTimeout(this.marketEndTimer);
      this.marketEndTimer = null;
    }
  }

  private updateDashboard() {
    if (this.priceYes === 0 && this.priceNo === 0) return;
    const totalCost = this.avgCostYes + this.avgCostNo;
    const yesAvg =
      this.filledCountYes > 0 ? this.avgCostYes / this.filledCountYes : 0;
    const noAvg =
      this.filledCountNo > 0 ? this.avgCostNo / this.filledCountNo : 0;
    let pnl: number | undefined;
    let isComplete = false;

    if (this.phase === "EXIT") {
      pnl = Math.min(this.filledCountYes, this.filledCountNo) * 1.0 - totalCost;
      isComplete = true;
    } else if (this.filledCountYes > 0 && this.filledCountNo > 0) {
      pnl = Math.min(this.filledCountYes, this.filledCountNo) * 1.0 - totalCost;
    }

    // Calc Real PnL for display
    const yesVal =
      this.filledCountYes * (this.priceYes > 0 ? this.priceYes : 0);
    const noVal = this.filledCountNo * (this.priceNo > 0 ? this.priceNo : 0);
    const unrealizedPnL = yesVal + noVal - totalCost;

    let hedgePrice: number | undefined;
    let hedgeSide: string | undefined;
    this.activeOrders.forEach((v, k) => {
      if (v.type === "HEDGE") {
        hedgePrice = v.price;
        hedgeSide = v.side;
      }
    });

    DashboardManager.update({
      name: "SniperLadder",
      phase: this.phase,
      priceYes: this.priceYes,
      priceNo: this.priceNo,
      yesShares: this.filledCountYes,
      yesAvg,
      noShares: this.filledCountNo,
      noAvg,
      activeOrders: this.activeOrders.size,
      pnl,
      unrealizedPnL, // Pass real PnL to dash
      hedgePrice,
      hedgeSide,
      isComplete,
    });
  }

  // 🟢 UPDATED: ZERO-PROTECTION PROTOCOL
  private async syncPositions() {
    // 🛡️ Guard: Don't sync if tokens aren't set yet
    if (!this.tokenIdYes || !this.tokenIdNo) return;

    // 🟢 1. IGNORE RECENT TRADES (Anti-Lag)
    // If we just traded within the last 60 seconds, skip sync to prevent API lag issues
    if (this.lastTradeTime > 0 && Date.now() - this.lastTradeTime < 60000) {
      return;
    }

    try {
      const positions = await this.executor.getPositions(
        this.tokenIdYes,
        this.tokenIdNo
      );

      // 🟢 2. THE FIX: If API says 0, but Memory > 0, TRUST MEMORY.
      // This stops the bot from deleting your shares and trying to buy them again.
      if (positions.yes === 0 && this.filledCountYes > 0) {
        DashboardManager.log(
          `🛡️ IGNORING API LAG: API says 0 YES, Memory says ${this.filledCountYes.toFixed(
            1
          )}. Keeping Memory.`,
          "WARN"
        );
        return;
      }
      if (positions.no === 0 && this.filledCountNo > 0) {
        DashboardManager.log(
          `🛡️ IGNORING API LAG: API says 0 NO, Memory says ${this.filledCountNo.toFixed(
            1
          )}. Keeping Memory.`,
          "WARN"
        );
        return;
      }

      const driftYes = positions.yes - this.filledCountYes;
      const driftNo = positions.no - this.filledCountNo;

      // If drift is significant (>0.1 shares), overwrite memory
      if (Math.abs(driftYes) > 0.1 || Math.abs(driftNo) > 0.1) {
        DashboardManager.log(
          `⚖️ SYNC FIX: YES ${this.filledCountYes.toFixed(
            1
          )}->${positions.yes.toFixed(1)} | NO ${this.filledCountNo.toFixed(
            1
          )}->${positions.no.toFixed(1)}`,
          "WARN"
        );

        this.filledCountYes = positions.yes;
        this.filledCountNo = positions.no;

        // Fix cost basis if missing
        if (this.filledCountYes > 0 && this.avgCostYes === 0)
          this.avgCostYes = this.filledCountYes * 0.5;
        if (this.filledCountNo > 0 && this.avgCostNo === 0)
          this.avgCostNo = this.filledCountNo * 0.5;
      }
    } catch (e: any) {
      // Silent fail on sync error, rely on WS
    }
  }
}
