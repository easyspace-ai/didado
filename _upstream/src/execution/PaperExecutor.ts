/**
 * PAPER EXECUTOR
 * 
 * Simulates trading without using real funds.
 * 
 * Features:
 * - Fake $1,000 starting balance
 * - Simulated order placement
 * - Manual order fills (via executePaperFill)
 * - Position tracking
 * - Trade history logging
 * 
 * Use this for:
 * - Testing strategies
 * - Learning how the bot works
 * - Backtesting
 * - Development
 * 
 * ⚠️ Note: Orders don't automatically fill - you must call executePaperFill()
 * to simulate fills. In real trading, fills come from WebSocket.
 * 
 * @class PaperExecutor
 * @implements {IExecutor}
 */
import { IExecutor, TradeParams } from "./IExecutor";

/**
 * Position tracking structure
 * 
 * Tracks:
 * - Token ID
 * - Side (BUY/SELL)
 * - Quantity (shares)
 * - Average entry price
 * - Total invested (cost basis)
 */
interface Position {
  tokenId: string;
  side: "BUY" | "SELL";
  quantity: number;
  avgEntryPrice: number;
  totalInvested: number;
}

export class PaperExecutor implements IExecutor {
  // ============================================================================
  // PROPERTIES
  // ============================================================================
  
  /** Current fake USDC balance */
  private balance: number;
  
  /** Starting balance (for performance tracking) */
  private initialBalance: number;
  
  /** Current positions (tokenId -> Position) */
  private positions: Map<string, Position> = new Map();
  
  /** Trade history (for logging and analysis) */
  private tradeHistory: any[] = [];
  
  /** Pending orders (orders that haven't been filled yet) */
  private pendingOrders: Map<string, TradeParams> = new Map();
  
  /** Filled orders (orders that have been executed) */
  private filledOrders: Map<string, { matched: number; avgFillPrice: number }> =
    new Map();
  
  /**
   * Token ID to side mapping
   * Used for order recovery and position tracking
   */
  private tokenSideMap: Map<string, "YES" | "NO"> = new Map();

  // ============================================================================
  // CONSTRUCTOR
  // ============================================================================
  
  /**
   * Initialize PaperExecutor
   * 
   * Creates a simulated trading account with fake funds.
   * 
   * @param {number} initialBalance - Starting balance (default: $1,000)
   */
  constructor(initialBalance: number = 1000) {
    this.balance = initialBalance;
    this.initialBalance = initialBalance;
    console.log(`\n📝 PAPER TRADING PORTFOLIO INITIALIZED`);
    console.log(`   💰 Initial Cash: $${this.balance.toFixed(2)}`);
    console.log(`   ------------------------------------------\n`);
  }

  async getBalance(): Promise<number> {
    return this.balance;
  }

  async placeOrder(params: TradeParams): Promise<string> {
    // 1. GENERATE ID
    const orderId = `PAPER-${Date.now().toString().slice(-6)}-${Math.floor(
      Math.random() * 100
    )}`;

    // 2. STORE AS PENDING
    // In "Trap" strategy, we place orders that sit in the book.
    // They are NOT filled immediately.
    // Note: FOK/IOC types don't apply to paper trading, but we accept them for interface consistency
    this.pendingOrders.set(orderId, params);

    // 🟢 Track tokenId for order recovery (simplified: assume BUY orders are YES/NO based on context)
    // This is a placeholder - real implementation would need market context
    // For now, we'll infer from the order pattern (all orders are BUY in this strategy)

    const orderType = params.type ? `[${params.type}]` : "";
    console.log(
      `\n📝 [PAPER] Order Placed (Pending): ${params.side} ${
        params.size
      } @ $${params.price.toFixed(2)} ${orderType} (ID: ${orderId})`
    );
    return orderId;
  }

  async cancelAll(): Promise<void> {
    this.pendingOrders.clear();
    console.log("   [PAPER] All pending orders cleared.");
  }

  async cancelOrder(orderId: string): Promise<void> {
    if (this.pendingOrders.has(orderId)) {
      this.pendingOrders.delete(orderId);
      console.log(`   [PAPER] Cancelled Pending Order ${orderId}`);
    } else {
      console.log(
        `   [PAPER] Cancelled Order ${orderId} (Already filled or non-existent)`
      );
    }
  }

  async getOrderStatus(
    orderId: string
  ): Promise<{ matched: number; cancelled: boolean; avgFillPrice: number }> {
    // Check if it was filled
    if (this.filledOrders.has(orderId)) {
      const filled = this.filledOrders.get(orderId)!;
      return {
        matched: filled.matched,
        cancelled: false,
        avgFillPrice: filled.avgFillPrice,
      };
    }
    // Check if it's still in pending
    if (this.pendingOrders.has(orderId)) {
      return { matched: 0, cancelled: false, avgFillPrice: 0 };
    }
    // Not found - assume cancelled
    return { matched: 0, cancelled: true, avgFillPrice: 0 };
  }

  async getPositions(
    tokenIdYes: string,
    tokenIdNo: string
  ): Promise<{ yes: number; no: number }> {
    const posYes = this.positions.get(tokenIdYes);
    const posNo = this.positions.get(tokenIdNo);

    return {
      yes: posYes ? posYes.quantity : 0,
      no: posNo ? posNo.quantity : 0,
    };
    }

  // 🟢 CRITICAL FIX: Implement getOpenOrders for order recovery
  async getOpenOrders(marketSlug: string): Promise<Array<{
    id: string;
    side: "YES" | "NO";
    price: string;
    size: number;
  }>> {
    const orders: Array<{
      id: string;
      side: "YES" | "NO";
      price: string;
      size: number;
    }> = [];

    // Return all pending orders (they're "open" in paper trading)
    for (const [orderId, params] of this.pendingOrders.entries()) {
      // 🟢 Get side from token mapping, or infer from stored data
      // Since we don't have market context here, we'll use a heuristic:
      // In SniperLadder, all orders are BUY, and side is determined by tokenId
      // For now, return a placeholder - the bot will need to match by tokenId
      const side = this.tokenSideMap.get(params.tokenId) || "YES"; // Default to YES
      
      orders.push({
        id: orderId,
        side: side,
        price: params.price.toFixed(2),
        size: params.size,
      });
    }

    return orders;
  }

  /**
   * Manual Fill Trigger for Strategy
   */
  async executePaperFill(
    orderId: string,
    price: number,
    quantity: number
  ): Promise<void> {
    const params = this.pendingOrders.get(orderId);
    if (!params) {
      console.warn(`❌ [PAPER] Cannot fill unknown/cancelled order ${orderId}`);
      return;
    }

    // 1. SIMULATE LATENCY
    await new Promise((resolve) => setTimeout(resolve, 50));

    const cost = price * quantity;

    // 2. VALIDATE FUNDS
    if (params.side === "BUY" && this.balance < cost) {
      console.warn(`❌ [PAPER] REJECTED: Insufficient Funds`);
      this.pendingOrders.delete(orderId);
      return;
    }

    // 3. EXECUTE TRADE
    if (params.side === "BUY") {
      this.balance -= cost;
    } else {
      this.balance += cost; // Sell logic
    }

    // 4. UPDATE POSITIONS
    this.updatePosition({ ...params, price, size: quantity }); // Use fill price

    // 5. MARK AS FILLED (store actual fill price!)
    this.filledOrders.set(orderId, { matched: quantity, avgFillPrice: price });

    // 6. REMOVE FROM PENDING
    this.pendingOrders.delete(orderId);

    // 7. LOG
    this.printTradeLog(orderId, { ...params, price }, cost);
    this.printPortfolioSummary();
  }

  // --- INTERNAL TRACKING LOGIC ---

  private updatePosition(params: TradeParams) {
    const existing = this.positions.get(params.tokenId);

    if (existing) {
      // Calculate Weighted Average Price
      // New Avg = ((OldQty * OldAvg) + (NewQty * NewPrice)) / TotalQty
      const totalShares = existing.quantity + params.size;
      const totalCost = existing.totalInvested + params.price * params.size;

      existing.quantity = totalShares;
      existing.totalInvested = totalCost;
      existing.avgEntryPrice = totalCost / totalShares;
    } else {
      // New Position
      this.positions.set(params.tokenId, {
        tokenId: params.tokenId,
        side: params.side,
        quantity: params.size,
        avgEntryPrice: params.price,
        totalInvested: params.price * params.size,
      });
    }
  }

  private printTradeLog(id: string, params: TradeParams, cost: number) {
    console.log(`\n✅ [PAPER TRADE EXECUTED] #${id}`);
    console.log(`   🔹 Side:      ${params.side}`);
    console.log(`   🔹 Asset:     ...${params.tokenId.slice(-10)}`); // Show last 10 chars
    console.log(`   🔹 Price:     $${params.price.toFixed(2)}`);
    console.log(`   🔹 Size:      ${params.size} Shares`);
    console.log(`   🔹 Cost:      $${cost.toFixed(2)}`);
  }

  private printPortfolioSummary() {
    const totalInvested = Array.from(this.positions.values()).reduce(
      (sum, p) => sum + p.totalInvested,
      0
    );
    const totalEquity = this.balance + totalInvested;
    const profit = totalEquity - this.initialBalance;
    const profitColor = profit >= 0 ? "🟢" : "🔴";

    console.log(`\n📊 [PORTFOLIO SUMMARY]`);
    console.log(`   💵 Cash Balance:    $${this.balance.toFixed(2)}`);
    console.log(`   💼 Invested Amount: $${totalInvested.toFixed(2)}`);
    console.log(`   📈 Total Equity:    $${totalEquity.toFixed(2)}`);
    console.log(`   ${profitColor} PnL (Unrealized): $${profit.toFixed(2)}`);

    console.log(
      `   -----------------------------------------------------------`
    );
    console.log(`   POSITIONS:`);
    if (this.positions.size === 0) {
      console.log(`   (Empty)`);
    } else {
      this.positions.forEach((pos, id) => {
        console.log(
          `   • ID ...${id.slice(-8)} | Qty: ${
            pos.quantity
          } | Avg Entry: $${pos.avgEntryPrice.toFixed(
            3
          )} | Cost: $${pos.totalInvested.toFixed(2)}`
        );
      }      );
    }
    console.log(
      `   -----------------------------------------------------------\n`
    );
  }

  // 🟢 NEW: Get user address (stub for paper trading)
  async getAddress(): Promise<string> {
    return "0x0000000000000000000000000000000000000000"; // Dummy address for paper trading
  }

  // 🟢 NEW: Redeem winnings (stub for paper trading)
  async redeemPositions(conditionId: string): Promise<string> {
    console.log(`📝 [PAPER] Redemption simulated for condition ${conditionId.slice(0, 10)}...`);
    // In paper trading, redemption doesn't make sense - just return a dummy hash
    return `PAPER-REDEEM-${Date.now()}`;
  }
}
