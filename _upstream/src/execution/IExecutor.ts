/**
 * EXECUTOR INTERFACE
 *
 * Defines the interface for order execution in both paper and live trading modes.
 *
 * The executor pattern allows strategies to work with either:
 * - PaperExecutor: Simulated trading (for testing)
 * - LiveExecutor: Real trading (for production)
 *
 * Both implement the same interface, so strategies don't need to know which one they're using.
 *
 * @module execution
 */

// ============================================================================
// TRADE PARAMETERS INTERFACE
// ============================================================================

/**
 * Parameters for placing an order
 *
 * @interface TradeParams
 */
export interface TradeParams {
  /** Token ID (YES or NO token address) */
  tokenId: string;

  /** Order side: BUY or SELL */
  side: "BUY" | "SELL";

  /** Limit price (e.g., 0.46 for $0.46) */
  price: number;

  /** Order size in shares */
  size: number;

  /**
   * Order type (optional, defaults to GTC)
   * - GTC: Good Till Cancel (order stays until filled or cancelled)
   * - FOK: Fill-Or-Kill (must fill completely or cancel)
   * - GTD: Good Till Date (order expires at specific date)
   * - FAK: Fill And Kill (fill what you can, cancel the rest)
   */
  type?: "GTC" | "FOK" | "GTD" | "FAK";
}

// ============================================================================
// EXECUTOR INTERFACE
// ============================================================================

/**
 * Executor interface for order placement and management
 *
 * Implemented by:
 * - PaperExecutor: Simulated trading
 * - LiveExecutor: Real trading
 *
 * @interface IExecutor
 */
export interface IExecutor {
  /**
   * Get current available balance
   *
   * - PaperExecutor: Returns fake balance (starts at $1,000)
   * - LiveExecutor: Returns real USDC balance from blockchain
   *
   * @returns {Promise<number>} Balance in USDC
   */
  getBalance(): Promise<number>;

  /**
   * Place a limit order
   *
   * Creates an order on Polymarket's order book.
   * Order will remain open until filled or cancelled.
   *
   * @param {TradeParams} params - Order parameters
   * @returns {Promise<string>} Order ID (for tracking and cancellation)
   */
  placeOrder(params: TradeParams): Promise<string>;

  /**
   * Cancel a specific order by ID
   *
   * Removes the order from the order book.
   * If order is already filled, this is a no-op.
   *
   * @param {string} orderId - Order ID to cancel
   * @returns {Promise<void>}
   */
  cancelOrder(orderId: string): Promise<void>;

  /**
   * Cancel all open orders
   *
   * Safety function to quickly exit all positions.
   * Used during:
   * - Graceful shutdown
   * - Emergency exits
   * - Strategy resets
   *
   * @returns {Promise<void>}
   */
  cancelAll(): Promise<void>;

  /**
   * Get order status
   *
   * Checks if an order has been filled, cancelled, or is still pending.
   *
   * @param {string} orderId - Order ID to check
   * @returns {Promise<Object>} Order status
   * @returns {number} matched - Number of shares filled
   * @returns {boolean} cancelled - Whether order was cancelled
   * @returns {number} avgFillPrice - Average fill price (if filled)
   */
  getOrderStatus(
    orderId: string
  ): Promise<{ matched: number; cancelled: boolean; avgFillPrice: number }>;

  /**
   * Manually fill an order (Paper Trading only)
   *
   * Used by PaperExecutor to simulate order fills.
   * Not used in LiveExecutor (real fills come from WebSocket).
   *
   * @param {string} orderId - Order ID to fill
   * @param {number} price - Fill price
   * @param {number} quantity - Fill quantity
   * @returns {Promise<void>}
   */
  executePaperFill?(
    orderId: string,
    price: number,
    quantity: number
  ): Promise<void>;

  /**
   * Get current positions for YES and NO tokens
   *
   * Returns the number of shares held for each outcome.
   *
   * @param {string} tokenIdYes - YES token ID
   * @param {string} tokenIdNo - NO token ID
   * @returns {Promise<Object>} Positions
   * @returns {number} yes - Number of YES shares
   * @returns {number} no - Number of NO shares
   */
  getPositions(
    tokenIdYes: string,
    tokenIdNo: string
  ): Promise<{ yes: number; no: number }>;

  /**
   * Redeem winning positions from resolved market
   *
   * Claims USDC from winning tokens after market resolves.
   * Only works in LiveExecutor (requires blockchain interaction).
   *
   * @param {string} conditionId - Market condition ID
   * @returns {Promise<string>} Transaction hash
   */
  redeemPositions(conditionId: string): Promise<string>;

  /**
   * Get user address
   *
   * Returns the wallet address being used for trading.
   * Used for:
   * - Position queries
   * - Balance checks
   * - Order recovery
   *
   * @returns {Promise<string>} Wallet address
   */
  getAddress(): Promise<string>;

  /**
   * Get open orders for a market (optional)
   *
   * Used for order recovery after disconnects.
   * Not all executors implement this (it's optional).
   *
   * @param {string} marketSlug - Market slug
   * @returns {Promise<Array>} Array of open orders
   */
  getOpenOrders?(marketSlug: string): Promise<
    Array<{
      id: string;
      side: "YES" | "NO";
      price: string;
      size: number;
    }>
  >;
}

// ============================================================================
// POLYGON CONTRACT ADDRESSES
// ============================================================================

/**
 * Conditional Token Framework (CTF) Contract Address
 *
 * This is Polymarket's smart contract that:
 * - Holds all market positions
 * - Manages token minting/burning
 * - Handles redemptions
 */
export const CTF_CONTRACT_ADDRESS =
  "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045";

/**
 * USDC Token Address on Polygon
 *
 * This is the collateral token used for all Polymarket markets.
 * All trades and redemptions are in USDC.
 */
export const USDC_ADDRESS = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174";
