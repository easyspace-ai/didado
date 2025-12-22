/**
 * POLYMARKET TRADING BOT - MAIN ENTRY POINT
 *
 * This is the main entry point for the Polymarket trading bot. It handles:
 * - Wallet initialization and authentication
 * - API credential derivation
 * - Strategy initialization
 * - Graceful shutdown handling
 *
 * The bot supports multiple trading strategies and can run in either:
 * - Paper Trading Mode: Simulated trading with fake funds (default)
 * - Live Trading Mode: Real trading with actual funds (set LIVE_TRADING=true)
 *
 */

import { ClobClient } from "@polymarket/clob-client";
import { Wallet } from "@ethersproject/wallet";
import { JsonRpcProvider } from "@ethersproject/providers";
import dotenv from "dotenv";

// Import our custom modules
import { PolymarketService } from "./services/PolymarketService";
import { WebSocketService } from "./services/WebSocketService";
import { SniperLadder } from "./strategies/SniperLadder";
import { SimpleTrap } from "./strategies/SimpleTrap";
import { PaperExecutor } from "./execution/PaperExecutor";
import { LiveExecutor } from "./execution/LiveExecutor";

// Load environment variables from .env file
dotenv.config();

// ============================================================================
// CONFIGURATION CONSTANTS
// ============================================================================

/**
 * Polygon Mainnet Chain ID
 * Polymarket runs on Polygon, so we use chain ID 137
 */
const CHAIN_ID = 137;

/**
 * Public Polygon RPC endpoint
 * Used for reading blockchain data (balances, etc.)
 * Note: For production, consider using a private RPC provider for better reliability
 */
const RPC_URL = "https://polygon-rpc.com";

/**
 * MAIN FUNCTION
 *
 * Initializes the bot with the following steps:
 * 1. Setup wallet and blockchain provider
 * 2. Authenticate with Polymarket CLOB API
 * 3. Initialize services (REST API, WebSocket)
 * 4. Select executor (Paper or Live trading)
 * 5. Initialize and start trading strategy
 * 6. Setup graceful shutdown handlers
 */
async function main() {
  console.log("\n🚀 POLYMARKET SNIPER BOT INITIALIZING...");

  // ============================================================================
  // STEP 1: SETUP WALLET & BLOCKCHAIN PROVIDER
  // ============================================================================

  /**
   * Create a JSON-RPC provider to interact with Polygon blockchain
   * This is used for:
   * - Reading wallet balances (MATIC, USDC)
   * - Checking transaction status
   * - Reading on-chain position data
   */
  const provider = new JsonRpcProvider(RPC_URL);

  /**
   * Load private key from environment variables
   *
   * If no private key is provided:
   * - Bot will generate a random wallet
   * - This wallet has no funds
   * - Only works for paper trading mode
   *
   * Security Note: Never commit your private key to version control!
   */
  const privateKey = process.env.PRIVATE_KEY;
  if (!privateKey) {
    console.warn(
      "⚠️ NO PRIVATE KEY FOUND IN .env! Using random wallet (Paper Trading only)."
    );
  }

  /**
   * Create wallet signer
   * - If private key exists: Use it to create wallet
   * - Otherwise: Generate random wallet for testing
   */
  const signer = privateKey
    ? new Wallet(privateKey, provider)
    : Wallet.createRandom().connect(provider);

  console.log(`🔑 Wallet Address: ${signer.address}`);

  /**
   * Check and log wallet balance (MATIC for gas fees)
   *
   * This is important because:
   * - Every transaction on Polygon requires MATIC for gas
   * - Without MATIC, orders cannot be placed
   * - Recommended: At least 0.1 MATIC for smooth operation
   *
   * Note: We use a timeout to prevent hanging if RPC is slow
   */
  try {
    console.log("💰 Checking wallet balance...");
    const balancePromise = signer.getBalance();
    const timeoutPromise = new Promise((_, reject) => {
      setTimeout(() => reject(new Error("Balance check timeout")), 10000);
    });
    const balance = (await Promise.race([
      balancePromise,
      timeoutPromise,
    ])) as any;
    const balanceEth = Number(balance.toString()) / 1e18;
    console.log(`💰 Native Balance: ${balanceEth.toFixed(4)} MATIC`);
  } catch (e: any) {
    console.warn(
      `⚠️ Could not fetch native balance: ${e.message || "Unknown error"}`
    );
    console.warn("   Continuing anyway...");
  }

  // ============================================================================
  // STEP 2: SETUP POLYMARKET CLOB CLIENT
  // ============================================================================

  /**
   * Determine proxy address and signature type
   *
   * Polymarket supports two wallet types:
   * 1. EOA (Externally Owned Account) - Direct MetaMask wallet
   *    - signatureType = 0
   *    - funder = wallet address
   *
   * 2. Proxy Wallet - Polymarket web UI wallet
   *    - signatureType = 1 or 2
   *    - funder = proxy address (different from signer)
   *    - Requires POLYMARKET_PROXY_ADDRESS in .env
   *
   * If you use Polymarket's web interface, you need to set POLYMARKET_PROXY_ADDRESS
   * to the address where your funds are deposited.
   */
  const proxyAddress = process.env.POLYMARKET_PROXY_ADDRESS || signer.address;
  const isProxy = !!process.env.POLYMARKET_PROXY_ADDRESS;

  /**
   * Create initial CLOB client (without credentials)
   *
   * The CLOB (Central Limit Order Book) client is used for:
   * - Placing orders
   * - Canceling orders
   * - Querying order status
   * - Authenticating with Polymarket API
   *
   * Initially created without credentials - they will be derived via wallet signature
   */
  let clobClient = new ClobClient(
    "https://clob.polymarket.com", // Polymarket CLOB API endpoint
    CHAIN_ID,
    signer,
    undefined, // creds (will be derived via deriveApiKey())
    isProxy ? 1 : 0, // signatureType: 0 = EOA (Direct), 1 = POLY_PROXY (Web User)
    proxyAddress // funder address (Must match where funds are deposited)
  );

  /**
   * Authenticate with Polymarket by deriving API credentials
   *
   * This process:
   * 1. Signs a message with your wallet
   * 2. Sends signature to Polymarket API
   * 3. Receives API credentials (key, secret, passphrase)
   * 4. These credentials are used for authenticated WebSocket connections
   *
   * Note: This can take 10-30 seconds. We use a timeout to prevent infinite hanging.
   */
  let creds: any = null;
  try {
    console.log("🔐 Authenticating with CLOB...");
    console.log(
      `   Signature Type: ${isProxy ? "1 (Proxy/Web)" : "0 (EOA/Direct)"}`
    );
    console.log(`   Funder Address: ${proxyAddress}`);
    if (isProxy) console.log(`   Signer Address: ${signer.address}`);

    /**
     * Add timeout wrapper to prevent infinite hanging
     * If API derivation takes more than 60 seconds, fail fast
     */
    console.log("   ⏳ Deriving API key (this may take 10-30 seconds)...");
    const timeoutPromise = new Promise((_, reject) => {
      setTimeout(
        () => reject(new Error("Authentication timeout after 60 seconds")),
        60000
      );
    });

    /**
     * Race between API derivation and timeout
     * Whichever completes first wins
     */
    creds = (await Promise.race([
      clobClient.deriveApiKey(),
      timeoutPromise,
    ])) as any;

    console.log("✅ API Key Derived Successfully.");

    /**
     * Re-instantiate client with derived credentials
     *
     * This ensures:
     * - Credentials are properly set
     * - Signature type is correct (2 for proxy, 0 for EOA)
     * - All future API calls use these credentials
     */
    clobClient = new ClobClient(
      "https://clob.polymarket.com",
      CHAIN_ID,
      signer,
      creds, // Pass derived creds here
      isProxy ? 2 : 0, // Force Signature Type 2 (Proxy with Browser Wallet/Metamask key)
      proxyAddress
    );
    console.log("   ✅ Client re-initialized with derived credentials.");
    console.log(`   (Debug) Signer Address: ${signer.address}`);
    console.log(`   (Debug) Configured Funder: ${proxyAddress}`);
    console.log(`   (Debug) Signature Type: ${isProxy ? 2 : 0}`);
  } catch (e: any) {
    console.error("❌ Authentication Failed:", e.message);
    console.error(
      "   Make sure you've enabled trading on polymarket.com with this wallet."
    );
    /**
     * Authentication failure usually means:
     * - Wallet signature failed
     * - Wallet not enabled for trading on Polymarket
     * - Network connectivity issues
     *
     * We exit here because without authentication, the bot cannot function.
     */
    process.exit(1);
  }

  // ============================================================================
  // STEP 3: INITIALIZE SERVICES
  // ============================================================================

  /**
   * PolymarketService: REST API client
   *
   * Handles:
   * - Fetching market data
   * - Getting token IDs for markets
   * - Querying market information
   */
  const polyService = new PolymarketService();

  /**
   * WebSocketService: Real-time data connection
   *
   * Each strategy needs its own WebSocket connection because:
   * - Different strategies may subscribe to different markets
   * - Prevents message conflicts
   * - Allows independent connection management
   */
  const wsServiceGrid = new WebSocketService(clobClient);
  const wsServiceSniper = new WebSocketService(clobClient);

  // ============================================================================
  // STEP 4: SELECT EXECUTOR (PAPER vs LIVE TRADING)
  // ============================================================================

  /**
   * Determine trading mode from environment variable
   *
   * Paper Trading (default):
   * - Simulates trades with fake $1,000 USDC
   * - No real money at risk
   * - Perfect for testing strategies
   *
   * Live Trading:
   * - Uses real funds from your wallet
   * - Real orders on Polymarket
   * - Real profits/losses
   *
   * To enable live trading, set in .env:
   * LIVE_TRADING=true
   */
  const IS_LIVE_TRADING = process.env.LIVE_TRADING === "true";

  /**
   * Create executors for order placement
   *
   * Executors handle:
   * - Placing orders
   * - Canceling orders
   * - Checking order status
   * - Getting positions
   *
   * We create separate executors for each strategy to:
   * - Track positions independently
   * - Prevent order conflicts
   * - Allow different strategies to run simultaneously
   */
  let executorGrid;
  let executorSniper;

  if (IS_LIVE_TRADING) {
    console.log("🚨 MODE: LIVE TRADING (REAL FUNDS) 🚨");
    /**
     * LiveExecutor: Real trading with actual funds
     *
     * Requires:
     * - clobClient: For placing real orders
     * - signer: For signing transactions
     * - proxyAddress: Where funds are deposited
     *
     * Features:
     * - Checks real USDC balance on-chain
     * - Places real orders on Polymarket
     * - Uses real gas (MATIC) for transactions
     */
    executorGrid = new LiveExecutor(clobClient, signer, proxyAddress);
    executorSniper = new LiveExecutor(clobClient, signer, proxyAddress);
  } else {
    console.log("📝 MODE: PAPER TRADING (SIMULATION)");
    /**
     * PaperExecutor: Simulated trading
     *
     * Features:
     * - Starts with fake $1,000 USDC
     * - Simulates order fills
     * - Tracks fake positions
     * - No real money at risk
     *
     * Perfect for:
     * - Testing strategies
     * - Learning how the bot works
     * - Backtesting
     */
    executorGrid = new PaperExecutor(1000);
    executorSniper = new PaperExecutor(1000);
  }

  /*
  // 4. SYSTEM CHECK MODE
  console.log("\n🧪 RUNNING SYSTEM CHECK (Strict 1 YES / 1 NO Test)");
  const systemCheck = new SystemCheck(
    polyService,
    wsServiceSniper,
    executorSniper
  );
  await systemCheck.start();
  */

  // ============================================================================
  // STEP 5: INITIALIZE TRADING STRATEGIES
  // ============================================================================

  /**
   * Initialize trading strategy
   *
   * Available strategies:
   * - SniperLadder: Advanced ladder with dynamic hedging (current default)
   * - SimpleTrap: Simple trap and hedge strategy
   *
   * Each strategy receives:
   * - polyService: For REST API calls
   * - wsService: For WebSocket connections
   * - executor: For order placement
   * - creds: API credentials for authenticated WebSocket
   * - signer: Wallet for TokenClaimer (automatic redemption)
   * - proxyAddress: For TokenClaimer
   */
  // const simpleTrapBot = new SimpleTrap(
  //   polyService,
  //   wsServiceSniper,
  //   executorSniper,
  //   creds,
  //   signer,
  //   proxyAddress
  // );
  const sniperBot = new SniperLadder(
    polyService,
    wsServiceSniper,
    executorSniper,
    creds, // Pass derived credentials for authenticated WebSocket
    signer, // Pass signer for TokenClaimer (automatic redemption)
    proxyAddress // Pass proxy address for TokenClaimer
  );

  // ============================================================================
  // STEP 6: START TRADING STRATEGY
  // ============================================================================

  console.log("\n🎯 RUNNING SNIPER LADDER STRATEGY");
  console.log("   🎯 SniperLadder (Trap @ $0.45 → Ladder → Dynamic Hedge)\n");

  // ============================================================================
  // STEP 7: SETUP GRACEFUL SHUTDOWN HANDLERS
  // ============================================================================

  /**
   * Graceful shutdown handler
   *
   * When bot receives SIGTERM or SIGINT (Ctrl+C):
   * 1. Stop the strategy (cancels all open orders)
   * 2. Close WebSocket connections
   * 3. Save any pending trade logs
   * 4. Exit cleanly
   *
   * This prevents:
   * - Orphaned orders left on Polymarket
   * - Data loss
   * - Connection leaks
   */
  const gracefulShutdown = async (signal: string) => {
    console.log(`\n🛑 Received ${signal}. Shutting down gracefully...`);
    try {
      // Stop bot (cancels orders and closes connections)
      await sniperBot.stop();
      console.log("✅ Bot stopped gracefully.");
    } catch (e: any) {
      console.error(`❌ Error during shutdown: ${e.message}`);
      // Fallback: Try to cancel orders directly
      try {
        await executorSniper.cancelAll();
        wsServiceSniper.close();
      } catch (e2: any) {
        console.error(`❌ Fallback shutdown also failed: ${e2.message}`);
      }
    }
    process.exit(0);
  };

  // Register signal handlers for graceful shutdown
  process.on("SIGTERM", () => gracefulShutdown("SIGTERM")); // Termination signal
  process.on("SIGINT", () => gracefulShutdown("SIGINT")); // Ctrl+C

  /**
   * Start the trading strategy
   *
   * This will:
   * - Find the next market
   * - Place initial orders
   * - Connect to WebSocket
   * - Begin trading
   */
  await sniperBot.start();
  // await Promise.all([gridBot.start(), simpleTrapBot.start()]); // Uncomment to run multiple strategies
}

// ============================================================================
// GLOBAL ERROR HANDLERS
// ============================================================================

/**
 * Handle unhandled promise rejections
 *
 * Prevents bot from crashing on async errors
 * Logs the error but allows bot to continue running
 * This is important for long-running bots that need to recover from transient errors
 */
process.on("unhandledRejection", (reason, promise) => {
  console.error("❌ UNHANDLED PROMISE REJECTION:", reason);
  // Don't exit - let the bots recover
});

/**
 * Handle uncaught exceptions
 *
 * Catches synchronous errors that weren't caught
 * Only exits on truly fatal errors (FATAL, ECONNREFUSED)
 * Otherwise, logs and continues
 */
process.on("uncaughtException", (err) => {
  console.error("❌ UNCAUGHT EXCEPTION:", err.message);
  // Don't exit for non-fatal errors - let the bots recover
  if (err.message?.includes("FATAL") || err.message?.includes("ECONNREFUSED")) {
    console.error("💀 Fatal error - exiting...");
    process.exit(1);
  }
});

/**
 * Keep process alive
 *
 * Node.js will exit if there are no active timers or handles
 * This heartbeat interval ensures the process stays alive
 * even if all trading activity pauses
 */
setInterval(() => {
  // Heartbeat - prevents Node from exiting if all timers complete
}, 60000);

// ============================================================================
// ENTRY POINT
// ============================================================================

/**
 * Start the bot
 *
 * If main() throws an error, log it and exit
 * This catches any initialization errors
 */
main().catch((err) => {
  console.error("❌ FATAL ERROR:", err);
  process.exit(1);
});
