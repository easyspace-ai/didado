/**
 * SIMPLE TRAP TRADING STRATEGY
 * 
 * A simple, low-risk trading strategy for Polymarket Bitcoin 15-minute markets.
 * 
 * Strategy Flow:
 * 1. Place initial traps at $0.46 on both YES and NO (5 shares each)
 * 2. Wait 1 minute before market starts
 * 3. When one trap fills:
 *    - Immediately cancel the opposite trap
 *    - Place hedge order at $0.52 on the opposite side
 * 4. Exit conditions:
 *    - WIN: Hedge fills → Perfect balance → Exit with profit
 *    - TIMEOUT: Hedge doesn't fill within 3 minutes:
 *      - Cancel all orders
 *      - If current price ≥ entry + $0.05 → Sell at market
 *      - Otherwise → Place breakeven limit order
 * 
 * Risk Management:
 * - Maximum exposure: 5 shares (one side only)
 * - Automatic hedging prevents large losses
 * - Timeout exit prevents holding unhedged positions
 * 
 * @class SimpleTrap
 */
import { PolymarketService } from "../services/PolymarketService";
import { WebSocketService } from "../services/WebSocketService";
import { MarketMath } from "../core/MarketMath";
import { IExecutor } from "../execution/IExecutor";
import { SpreadsheetLogger, TradeRecord } from "../services/SpreadsheetLogger";
import { DashboardManager, setBotStatus } from "../services/DashboardManager";
import { TokenClaimer } from "../services/TokenClaimer";
import { Wallet } from "@ethersproject/wallet";

export class SimpleTrap {
  // ============================================================================
  // DEPENDENCIES
  // ============================================================================
  
  /** REST API service for fetching market data */
  private api: PolymarketService;
  
  /** WebSocket service for real-time price and fill updates */
  private ws: WebSocketService;
  
  /** Order executor (Paper or Live trading) */
  private executor: IExecutor;
  
  /** Bot running state flag */
  private isRunning: boolean = false;
  
  /** CSV logger for trade history */
  private logger: SpreadsheetLogger;

  // ============================================================================
  // STRATEGY CONFIGURATION
  // ============================================================================
  
  /**
   * Initial trap price
   * Both YES and NO traps are placed at this price
   */
  private INITIAL_TRAP_PRICE = 0.46;
  
  /**
   * Number of shares per trap order
   * Total exposure: 5 shares (if one trap fills)
   */
  private INITIAL_TRAP_SIZE = 5; // 5 shares per side
  
  /**
   * Hedge order price
   * When a trap fills, we place a hedge at this price on the opposite side
   */
  private HEDGE_PRICE = 0.52;
  
  /**
   * Hedge timeout in milliseconds
   * If hedge doesn't fill within this time, we exit the position
   */
  private HEDGE_TIMEOUT_MS = 180000; // 3 minutes
  
  /**
   * Market sell price gap threshold
   * If current price is this much higher than entry, sell at market instead of breakeven
   */
  private MARKET_SELL_PRICE_GAP = 0.05; // Sell at market if price is 5 cents higher than entry
  
  /**
   * Single cycle mode
   * If true, bot exits after one market cycle
   * If false, bot continues to next market
   */
  private ONE_CYCLE_ONLY = true;

  // ============================================================================
  // TRADING STATE
  // ============================================================================
  
  /**
   * Current trading phase
   * - NEUTRAL: Waiting for trap to fill
   * - HEDGE_PENDING: Trap filled, waiting for hedge to fill
   * - EXIT: Position closed, exiting
   */
  private phase: "NEUTRAL" | "HEDGE_PENDING" | "EXIT" = "NEUTRAL";

  /**
   * Current market information
   */
  private currentMarketSlug: string = "";
  private marketEndTimeMs: number = 0;

  /**
   * Active orders tracking
   * Maps order ID to order information (side, type, price)
   * Used for:
   * - Canceling orders
   * - Tracking order status
   * - Preventing duplicate orders
   */
  private activeOrders: Map<
    string,
    { side: "YES" | "NO"; type: "TRAP" | "HEDGE"; price: number }
  > = new Map();

  /**
   * Order fill history
   * Tracks how much of each order has been filled
   * Used to detect partial fills and new fills
   */
  private orderFillHistory: Map<string, number> = new Map();

  /**
   * Position tracking
   * - filledCountYes/No: Total shares filled on each side
   * - avgCostYes/No: Total cost basis for each side
   * - yesFills/noFills: Array of individual fills (for logging)
   */
  private filledCountYes: number = 0;
  private filledCountNo: number = 0;
  private avgCostYes: number = 0;
  private avgCostNo: number = 0;
  private yesFills: { price: number; size: number }[] = [];
  private noFills: { price: number; size: number }[] = [];

  /**
   * Market token IDs
   * These are the actual token contract addresses for YES and NO outcomes
   */
  private tokenIdYes: string = "";
  private tokenIdNo: string = "";
  
  /**
   * Current market prices
   * Updated from WebSocket market feed
   */
  private priceYes: number = 0;
  private priceNo: number = 0;

  /**
   * Timing and processing state
   */
  private lastLogTime: number = 0; // Last dashboard update time
  private isProcessingTick: boolean = false; // Prevents concurrent tick processing
  private lastSyncTime: number = 0; // Last position sync time

  /**
   * Connection health monitoring
   * - lastTickTime: Last time we received WebSocket data
   * - zeroDataStreak: Count of consecutive zero/empty data messages
   */
  private lastTickTime: number = Date.now();
  private zeroDataStreak: number = 0;

  /**
   * Deduplication
   * Prevents processing the same fill event multiple times
   */
  private processedMatchIds: Set<string> = new Set();

  /**
   * Hedge timeout tracking
   * - hedgeTimeoutTimer: Timer that triggers if hedge doesn't fill
   * - trapFillTime: When the trap was filled (for timeout calculation)
   * - hedgeOrderId: ID of the hedge order (for tracking)
   * - filledSide: Which side filled (YES or NO)
   */
  private hedgeTimeoutTimer: NodeJS.Timeout | null = null;
  private trapFillTime: number = 0;
  private hedgeOrderId: string | null = null;
  private filledSide: "YES" | "NO" | null = null;

  /**
   * Interval timers (for cleanup)
   * - healthCheckInterval: Checks WebSocket connection health
   * - syncInterval: Periodically syncs positions from API
   * - marketEndTimer: Triggers when market ends
   */
  private healthCheckInterval: NodeJS.Timeout | null = null;
  private syncInterval: NodeJS.Timeout | null = null;
  private marketEndTimer: NodeJS.Timeout | null = null;

  /**
   * API credentials
   * Used for authenticated WebSocket connections
   */
  private apiCreds: { key: string; secret: string; passphrase: string } | null =
    null;

  /**
   * Token claiming service
   * Automatically claims winning tokens after market cycles
   */
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
    this.logger = new SpreadsheetLogger("SimpleTrap");
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
          pollIntervalMs: 5000,
        });
        console.log("[SIMPLETRAP] ✅ TokenClaimer initialized");
      } catch (e: any) {
        console.error(
          `[SIMPLETRAP] ⚠️ Failed to initialize TokenClaimer: ${e.message}`
        );
      }
    }
  }

  async start() {
    this.isRunning = true;
    console.log("🚀 SIMPLE TRAP BOT STARTED");
    console.log(`   🎯 Strategy: Trap @ $0.46 → Hedge @ $0.52 → Exit`);

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
      await this.ws.subscribeUser(creds, (data) => this.handleUserFill(data));
    }

    this.healthCheckInterval = setInterval(
      () => this.checkConnectionHealth(),
      15000
    );
    this.syncInterval = setInterval(() => this.syncPositions(), 30000);

    this.safeRunLifecycle();
  }

  async stop() {
    console.log("🛑 Stopping bot gracefully...");
    this.isRunning = false;

    // Stop token claiming
    if (this.tokenClaimer) {
      this.tokenClaimer.stopPolling();
    }

    try {
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
      if (this.hedgeTimeoutTimer) {
        clearTimeout(this.hedgeTimeoutTimer);
        this.hedgeTimeoutTimer = null;
      }

      await this.cancelAllOrders();
      this.ws.close();
    } catch (e: any) {
      DashboardManager.log(`❌ Error during shutdown: ${e.message}`, "ERROR");
    }
  }

  private safeRunLifecycle() {
    this.runLifecycle().catch((err) => {
      DashboardManager.log(`❌ Lifecycle error: ${err.message}`, "ERROR");
      if (this.isRunning) {
        setTimeout(() => this.safeRunLifecycle(), 5000);
      }
    });
  }

  private async runLifecycle() {
    if (!this.isRunning) return;

    // 1. Get Current and Next markets
    const currentTarget = MarketMath.getTargetMarketSlug();
    const nextTarget = MarketMath.getNextMarketSlug();

    DashboardManager.log(
      `🔍 System Start. Checking Market: ${currentTarget.slug.substring(
        0,
        10
      )}...`,
      "INFO"
    );

    // Default target is NEXT (Safety default - wait for current to end)
    let target = nextTarget;

    // 2. SMART TARGETING: Check if we have active positions in current market
    try {
      const tokens = await this.api.getMarketTokenIds(currentTarget.slug);
      if (tokens.length >= 2) {
        const positions = await this.executor.getPositions(
          tokens[0],
          tokens[1]
        );
        if (positions.yes > 0.1 || positions.no > 0.1) {
          DashboardManager.log(
            `🚨 ACTIVE TRADE FOUND! Trading Current Market.`,
            "WARN"
          );
          target = currentTarget; // If we have positions, trade current market
        }
      }
    } catch (e: any) {
      DashboardManager.log(`⚠️ Startup Check Failed: ${e.message}`, "ERROR");
    }

    this.marketEndTimeMs = target.endTimeMs;
    this.currentMarketSlug = target.slug;

    // 3. WAIT LOGIC: If targeting NEXT market, wait until 1 minute before it starts
    const msUntilStart = MarketMath.getMsUntil(target.startTimeMs);
    const msUntilPrep = msUntilStart - 60000; // Place orders 1 minute before market starts

    if (msUntilPrep > 0 && target === nextTarget) {
      const waitSecs = Math.round(msUntilPrep / 1000);
      const waitMins = Math.round(waitSecs / 60);
      setBotStatus(
        "SimpleTrap",
        `⏳ Waiting ${waitMins}m ${waitSecs % 60}s for next market`
      );
      DashboardManager.log(
        `⏳ Current market still active. Waiting ${waitMins}m ${
          waitSecs % 60
        }s until 1 min before next market...`,
        "INFO"
      );
      await new Promise((resolve) => setTimeout(resolve, msUntilPrep));
    }

    // 4. GET TOKENS
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
    DashboardManager.logEvent("SIMPLETRAP", `✅ Tokens Acquired`);

    // 5. HYDRATE STATE
    if (this.filledCountYes === 0 && this.filledCountNo === 0) {
      await this.hydrateState();
    } else {
      DashboardManager.log(
        `🧠 Memory Active: [${this.filledCountYes}Y / ${this.filledCountNo}N]. Skipping scan.`,
        "INFO"
      );
    }

    // 6. PLACE TRAPS (only if we don't have active traps)
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

    // 7. CONNECT WS
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

    await this.ws.subscribeMarket(
      [this.tokenIdYes, this.tokenIdNo],
      (data: any) => this.handleMarketTick(data)
    );

    // 8. SCHEDULE END
    const msUntilEnd = MarketMath.getMsUntil(this.marketEndTimeMs);
    DashboardManager.log(
      `⏰ Timer set for ${(msUntilEnd / 60000).toFixed(1)} minutes`,
      "INFO"
    );

    if (this.marketEndTimer) {
      clearTimeout(this.marketEndTimer);
      this.marketEndTimer = null;
    }

    this.marketEndTimer = setTimeout(() => {
      this.endCycleAndRestart().catch((err) => {
        DashboardManager.log(`❌ Cycle end error: ${err.message}`, "ERROR");
        if (this.isRunning) this.safeRunLifecycle();
      });
    }, msUntilEnd + 2000);
  }

  private async endCycleAndRestart() {
    setBotStatus("SimpleTrap", "🔄 Switching cycle...");
    DashboardManager.logEvent("SIMPLETRAP", "🏁 Market ended");
    await this.logIncompletePosition();

    try {
      this.ws.close();
    } catch (e) {
      /* ignore */
    }

    // Start token claiming after cycle ends
    if (this.tokenClaimer) {
      console.log(
        "[SIMPLETRAP] 🎁 Starting automatic token claiming (polling every 5s)..."
      );
      this.tokenClaimer.startPolling();
    }

    this.resetState();

    if (this.ONE_CYCLE_ONLY) {
      console.log(`[SIMPLETRAP] ✅ Single Cycle Complete.`);
      if (this.tokenClaimer) {
        this.tokenClaimer.stopPolling();
      }
      process.exit(0);
    } else {
      if (this.tokenClaimer) {
        this.tokenClaimer.stopPolling();
      }
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
        "SIMPLETRAP",
        `💔 LOSS: Unbalanced position (${this.filledCountYes}Y/${
          this.filledCountNo
        }N) -$${Math.abs(profit).toFixed(3)}`
      );

      const yesAvg =
        this.filledCountYes > 0 ? this.avgCostYes / this.filledCountYes : 0;
      const noAvg =
        this.filledCountNo > 0 ? this.avgCostNo / this.filledCountNo : 0;

      const record: TradeRecord = {
        time: new Date().toISOString(),
        marketSlug: this.currentMarketSlug,
        capitalInvested: totalCost,
        profitPercent,
        capitalEnd: hedgedPayout,
        yesPositions: SpreadsheetLogger.formatPositions(this.yesFills),
        yesAvg: yesAvg,
        yesTotal: this.filledCountYes,
        noPositions: SpreadsheetLogger.formatPositions(this.noFills),
        noAvg: noAvg,
        noTotal: this.filledCountNo,
        pnl: profit,
        outcome: "LOSS",
      };
      this.logger.logTrade(record);
    }
  }

  private resetState() {
    this.phase = "NEUTRAL";
    this.filledCountYes = 0;
    this.filledCountNo = 0;
    this.avgCostYes = 0;
    this.avgCostNo = 0;
    this.yesFills = [];
    this.noFills = [];
    this.activeOrders.clear();
    this.orderFillHistory.clear();
    this.processedMatchIds.clear();
    this.hedgeOrderId = null;
    this.filledSide = null;
    this.trapFillTime = 0;
    if (this.hedgeTimeoutTimer) {
      clearTimeout(this.hedgeTimeoutTimer);
      this.hedgeTimeoutTimer = null;
    }
  }

  private async hydrateState() {
    if (!this.currentMarketSlug) return;

    try {
      const positions = await this.executor.getPositions(
        this.tokenIdYes,
        this.tokenIdNo
      );
      if (positions.yes > 0 || positions.no > 0) {
        this.filledCountYes = positions.yes;
        this.filledCountNo = positions.no;
      }
    } catch (e) {
      /* ignore */
    }

    if (this.filledCountYes > 0 && this.avgCostYes === 0)
      this.avgCostYes = this.filledCountYes * 0.5;
    if (this.filledCountNo > 0 && this.avgCostNo === 0)
      this.avgCostNo = this.filledCountNo * 0.5;

    if (this.filledCountYes > 0 || this.filledCountNo > 0) {
      DashboardManager.log(
        `📝 MEMORY UPDATED: ${this.filledCountYes}Y / ${this.filledCountNo}N (from Scan)`,
        "WARN"
      );
      if (this.phase === "NEUTRAL") {
        // If we have positions, we're in hedge pending state
        this.phase = "HEDGE_PENDING";
      }
    }
  }

  private async handleMarketTick(data: any) {
    if (this.isProcessingTick) return;
    this.isProcessingTick = true;

    try {
      this.lastTickTime = Date.now();
      this.updatePrices(data);

      // Check for zero data
      const isPriceZero = this.priceYes === 0 && this.priceNo === 0;
      const isAmnesia =
        this.priceYes === 0 &&
        this.priceNo === 0 &&
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

      await this.checkFills();

      // Update dashboard periodically
      const now = Date.now();
      if (now - this.lastLogTime > 1000) {
        this.lastLogTime = now;
        this.updateDashboard();
      }
    } catch (err: any) {
      DashboardManager.log(`❌ Tick error: ${err.message}`, "ERROR");
    } finally {
      this.isProcessingTick = false;
    }
  }

  private async handleUserFill(data: any) {
    if (!data || data.event_type !== "trade") return;

    const matchId = data.match_id || data.id;
    if (!matchId || this.processedMatchIds.has(matchId)) return;
    this.processedMatchIds.add(matchId);

    const assetId = data.asset_id || data.token_id;
    const side = data.side === "BUY" ? "BUY" : "SELL";
    const price = parseFloat(data.price || "0");
    const size = parseFloat(data.size || data.amount || "0");

    if (!assetId || price <= 0 || size <= 0) return;

    if (assetId === this.tokenIdYes || assetId === this.tokenIdNo) {
      await this.handleOrderFilled(
        matchId,
        { side: assetId === this.tokenIdYes ? "YES" : "NO", type: "TRAP" },
        price,
        size
      );
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
    DashboardManager.logEvent("SIMPLETRAP", rawMsg);

    // Update state
    if (orderInfo.side === "YES") {
      this.filledCountYes += filledSize;
      this.avgCostYes += fillPrice * filledSize;
      this.yesFills.push({ price: fillPrice, size: filledSize });
    } else {
      this.filledCountNo += filledSize;
      this.avgCostNo += fillPrice * filledSize;
      this.noFills.push({ price: fillPrice, size: filledSize });
    }

    // If trap filled, IMMEDIATELY cancel opposite trap and place hedge
    if (this.phase === "NEUTRAL" && orderInfo.type === "TRAP") {
      const loserSide = orderInfo.side === "YES" ? "NO" : "YES";

      // 🚨 IMMEDIATE ACTION: Cancel opposite trap first
      DashboardManager.log(
        `🚨 TRAP FILLED! Cancelling opposite ${loserSide} trap immediately...`,
        "TRADE"
      );
      await this.cancelOrdersByType(loserSide, "TRAP");

      this.filledSide = orderInfo.side;
      this.trapFillTime = Date.now();
      this.phase = "HEDGE_PENDING";

      // 🚨 IMMEDIATE ACTION: Place hedge order at $0.52
      DashboardManager.log(
        `📤 Placing hedge order immediately: ${loserSide} @ $${this.HEDGE_PRICE}`,
        "TRADE"
      );
      await this.placeHedgeOrder(orderInfo.side);

      // Set timeout for 3 minutes
      if (this.hedgeTimeoutTimer) {
        clearTimeout(this.hedgeTimeoutTimer);
      }
      this.hedgeTimeoutTimer = setTimeout(() => {
        this.handleHedgeTimeout();
      }, this.HEDGE_TIMEOUT_MS);
    } else if (this.phase === "HEDGE_PENDING" && orderInfo.type === "HEDGE") {
      // Hedge filled - exit!
      await this.transitionToExit();
    }
  }

  private async placeHedgeOrder(filledSide: "YES" | "NO") {
    const defenderSide = filledSide === "YES" ? "NO" : "YES";
    const tokenId = defenderSide === "YES" ? this.tokenIdYes : this.tokenIdNo;

    try {
      DashboardManager.log(
        `📤 Placing HEDGE: ${defenderSide} ${this.INITIAL_TRAP_SIZE} @ $${this.HEDGE_PRICE}`,
        "INFO"
      );

      const id = await this.executor.placeOrder({
        tokenId,
        side: "BUY",
        price: this.HEDGE_PRICE,
        size: this.INITIAL_TRAP_SIZE,
        type: "GTC",
      });

      this.hedgeOrderId = id;
      this.activeOrders.set(id, {
        side: defenderSide,
        type: "HEDGE",
        price: this.HEDGE_PRICE,
      });

      setBotStatus(
        "SimpleTrap",
        `🛡️ Hedge Pending (${defenderSide} @ $${this.HEDGE_PRICE})`
      );
    } catch (e: any) {
      DashboardManager.log(`❌ HEDGE FAILED: ${e.message}`, "ERROR");
    }
  }

  private async handleHedgeTimeout() {
    if (this.phase !== "HEDGE_PENDING") return;

    DashboardManager.log(
      `⏰ HEDGE TIMEOUT: 3 minutes elapsed. Exiting position...`,
      "WARN"
    );

    // Cancel all orders
    await this.cancelAllOrders();

    // Check if we should sell at market or place breakeven order
    if (this.filledSide === "YES") {
      const entryPrice = this.avgCostYes / this.filledCountYes;
      const currentPrice = this.priceYes;

      // If current price is higher by gap threshold, sell at market
      if (
        currentPrice > 0 &&
        currentPrice >= entryPrice + this.MARKET_SELL_PRICE_GAP
      ) {
        DashboardManager.log(
          `💰 Market sell: Current price $${currentPrice.toFixed(2)} is $${(
            currentPrice - entryPrice
          ).toFixed(2)} above entry. Selling at market...`,
          "INFO"
        );
        await this.sellAtMarket("YES");
      } else {
        // Place breakeven limit order
        const breakevenPrice = 1.0 - entryPrice;
        DashboardManager.log(
          `📊 Placing breakeven order: ${
            this.filledCountYes
          } YES @ $${breakevenPrice.toFixed(2)}`,
          "INFO"
        );
        await this.placeBreakevenOrder("YES", breakevenPrice);
      }
    } else if (this.filledSide === "NO") {
      const entryPrice = this.avgCostNo / this.filledCountNo;
      const currentPrice = this.priceNo;

      if (
        currentPrice > 0 &&
        currentPrice >= entryPrice + this.MARKET_SELL_PRICE_GAP
      ) {
        DashboardManager.log(
          `💰 Market sell: Current price $${currentPrice.toFixed(2)} is $${(
            currentPrice - entryPrice
          ).toFixed(2)} above entry. Selling at market...`,
          "INFO"
        );
        await this.sellAtMarket("NO");
      } else {
        const breakevenPrice = 1.0 - entryPrice;
        DashboardManager.log(
          `📊 Placing breakeven order: ${
            this.filledCountNo
          } NO @ $${breakevenPrice.toFixed(2)}`,
          "INFO"
        );
        await this.placeBreakevenOrder("NO", breakevenPrice);
      }
    }

    // Transition to exit
    this.phase = "EXIT";
  }

  private async sellAtMarket(side: "YES" | "NO") {
    const tokenId = side === "YES" ? this.tokenIdYes : this.tokenIdNo;
    const shares = side === "YES" ? this.filledCountYes : this.filledCountNo;

    try {
      // Use very low price (market sell) with FOK type
      const marketPrice = 0.01; // Very aggressive price for market sell
      DashboardManager.log(
        `📤 MARKET SELL: ${side} ${shares} @ $${marketPrice} (FOK)`,
        "INFO"
      );

      await this.executor.placeOrder({
        tokenId,
        side: "SELL",
        price: marketPrice,
        size: shares,
        type: "FOK", // Fill-Or-Kill (market order)
      });
    } catch (e: any) {
      DashboardManager.log(`❌ Market sell failed: ${e.message}`, "ERROR");
    }
  }

  private async placeBreakevenOrder(side: "YES" | "NO", price: number) {
    const tokenId = side === "YES" ? this.tokenIdYes : this.tokenIdNo;
    const shares = side === "YES" ? this.filledCountYes : this.filledCountNo;

    try {
      const id = await this.executor.placeOrder({
        tokenId,
        side: "SELL",
        price: price,
        size: shares,
        type: "GTC",
      });

      this.activeOrders.set(id, { side, type: "HEDGE", price });
      DashboardManager.log(
        `✅ Breakeven order placed: ${side} ${shares} @ $${price.toFixed(2)}`,
        "INFO"
      );
    } catch (e: any) {
      DashboardManager.log(`❌ Breakeven order failed: ${e.message}`, "ERROR");
    }
  }

  private async transitionToExit() {
    if (this.phase === "EXIT") return;

    DashboardManager.log("⚖️ PERFECT BALANCE REACHED. EXITING...", "TRADE");
    this.phase = "EXIT";

    // Cancel all orders
    await this.cancelAllOrders();

    // Log trade
    const totalCost = this.avgCostYes + this.avgCostNo;
    const hedgedPairs = Math.min(this.filledCountYes, this.filledCountNo);
    const payout = hedgedPairs * 1.0;
    const profit = payout - totalCost;
    const profitPercent = totalCost > 0 ? profit / totalCost : 0;

    DashboardManager.logEvent(
      "SIMPLETRAP",
      `🏆 WIN! +$${profit.toFixed(3)} (${this.filledCountYes}Y/${
        this.filledCountNo
      }N)`
    );

    const yesAvg =
      this.filledCountYes > 0 ? this.avgCostYes / this.filledCountYes : 0;
    const noAvg =
      this.filledCountNo > 0 ? this.avgCostNo / this.filledCountNo : 0;

    const record: TradeRecord = {
      time: new Date().toISOString(),
      marketSlug: this.currentMarketSlug,
      capitalInvested: totalCost,
      profitPercent,
      capitalEnd: payout,
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
  }

  private async placeTrapOrders() {
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

      const idYes = await this.executor.placeOrder({
        tokenId: this.tokenIdYes,
        side: "BUY",
        price: this.INITIAL_TRAP_PRICE,
        size: this.INITIAL_TRAP_SIZE,
        type: "GTC",
      });

      const idNo = await this.executor.placeOrder({
        tokenId: this.tokenIdNo,
        side: "BUY",
        price: this.INITIAL_TRAP_PRICE,
        size: this.INITIAL_TRAP_SIZE,
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

      setBotStatus("SimpleTrap", `🪤 Traps Set ($${this.INITIAL_TRAP_PRICE})`);
    } catch (e: any) {
      DashboardManager.log(`❌ TRAP FAILED: ${e.message}`, "ERROR");
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

  private async cancelOrdersByType(side: "YES" | "NO", type: "TRAP" | "HEDGE") {
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

  private updatePrices(data: any) {
    if (data.asks) {
      if (data.asset_id === this.tokenIdYes) {
        const newPrice = parseFloat(data.asks[0]?.price || "0");
        if (newPrice > 0) this.priceYes = newPrice;
      }
      if (data.asset_id === this.tokenIdNo) {
        const newPrice = parseFloat(data.asks[0]?.price || "0");
        if (newPrice > 0) this.priceNo = newPrice;
      }
    }
  }

  private async checkFills() {
    for (const [orderId, orderInfo] of this.activeOrders.entries()) {
      try {
        const status = await this.executor.getOrderStatus(orderId);
        const totalMatched = status.matched || 0;
        const prevMatched = this.orderFillHistory.get(orderId) || 0;

        if (totalMatched > prevMatched) {
          const newFillAmount = totalMatched - prevMatched;
          const fillPrice = status.avgFillPrice || orderInfo.price;

          this.orderFillHistory.set(orderId, totalMatched);

          await this.handleOrderFilled(
            orderId,
            orderInfo,
            fillPrice,
            newFillAmount
          );
        }
      } catch (e: any) {
        /* ignore */
      }
    }
  }

  private async syncPositions() {
    if (Date.now() - this.lastSyncTime < 5000) return;
    this.lastSyncTime = Date.now();

    try {
      const positions = await this.executor.getPositions(
        this.tokenIdYes,
        this.tokenIdNo
      );
      // Only update if we have no memory (don't overwrite if we have fills)
      if (
        this.filledCountYes === 0 &&
        this.filledCountNo === 0 &&
        (positions.yes > 0 || positions.no > 0)
      ) {
        this.filledCountYes = positions.yes;
        this.filledCountNo = positions.no;
      }
    } catch (e) {
      /* ignore */
    }
  }

  private async reconnectWebSocket() {
    try {
      this.ws.close();
      await new Promise((r) => setTimeout(r, 1000));
      await this.ws.subscribeMarket(
        [this.tokenIdYes, this.tokenIdNo],
        (data: any) => this.handleMarketTick(data)
      );
    } catch (e) {
      /* ignore */
    }
  }

  private async checkConnectionHealth() {
    if (!this.isRunning) return;
    const silenceDuration = Date.now() - this.lastTickTime;

    if (silenceDuration > 45000) {
      DashboardManager.log(
        `💀 WATCHDOG: Connection Dead (${(silenceDuration / 1000).toFixed(
          0
        )}s). Resetting Socket...`,
        "WARN"
      );
      await this.reconnectWebSocket();
    }
  }

  private updateDashboard() {
    const status = `[${this.filledCountYes.toFixed(
      1
    )}Y / ${this.filledCountNo.toFixed(1)}N]`;
    setBotStatus("SimpleTrap", status);
  }
}
