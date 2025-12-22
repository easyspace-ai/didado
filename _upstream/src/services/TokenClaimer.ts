/**
 * TOKEN CLAIMER SERVICE
 *
 * Automatically claims winning tokens from resolved Polymarket markets.
 *
 * How it works:
 * 1. Polls Polymarket Data API every 5 seconds for redeemable positions
 * 2. When redeemable positions are found, claims them via gasless relayer
 * 3. Uses Polymarket's relayer service (no gas fees required)
 *
 * The relayer service allows gasless transactions by:
 * - Polymarket pays the gas fees
 * - User signs the transaction
 * - Relayer submits to blockchain
 *
 * This service runs in the background after each market cycle ends.
 *
 * @class TokenClaimer
 */
import { Wallet } from "@ethersproject/wallet";
import { JsonRpcProvider } from "@ethersproject/providers";
import { RelayClient } from "@polymarket/builder-relayer-client";
import { BuilderConfig } from "@polymarket/builder-signing-sdk";
import { encodeFunctionData, parseAbi } from "viem";
import axios from "axios";

// ============================================================================
// CONSTANTS
// ============================================================================

/**
 * Polymarket relayer URL
 * This is the gasless transaction service endpoint
 */
const RELAYER_URL = "https://relayer-v2.polymarket.com";

/**
 * Polygon Mainnet Chain ID
 */
const POLYGON_CHAIN_ID = 137;

/**
 * Conditional Token Framework (CTF) Contract Address
 * This is the Polymarket smart contract that holds positions
 */
const CTF_ADDRESS = "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045";

/**
 * USDC Token Address on Polygon
 * This is the collateral token used for all Polymarket markets
 */
const USDC_ADDRESS = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174";

/**
 * ABI for the redeemPositions function
 * Used to encode the redemption transaction data
 */
const REDEEM_ABI = parseAbi([
  "function redeemPositions(address collateralToken, bytes32 parentCollectionId, bytes32 conditionId, uint256[] indexSets)",
]);

// ============================================================================
// CONFIGURATION INTERFACE
// ============================================================================

/**
 * Configuration for TokenClaimer
 */
interface TokenClaimerConfig {
  signer: Wallet; // Wallet for signing transactions
  builderKey: string; // Polymarket API key
  builderSecret: string; // Polymarket API secret
  builderPassphrase: string; // Polymarket API passphrase
  proxyAddress?: string; // Proxy address if using web UI
  pollIntervalMs?: number; // Polling interval (default: 5000ms = 5 seconds)
}

// ============================================================================
// TOKEN CLAIMER CLASS
// ============================================================================

export class TokenClaimer {
  // ============================================================================
  // PROPERTIES
  // ============================================================================

  /** Wallet signer for transaction signing */
  private signer: Wallet;

  /** User address to check positions for (proxy or signer address) */
  private userAddress: string;

  /** Relayer client for gasless transactions */
  private relayClient: RelayClient;

  /** Polling interval timer */
  private pollInterval: NodeJS.Timeout | null = null;

  /** Whether polling is currently active */
  private isPolling: boolean = false;

  /** Polling interval in milliseconds */
  private pollIntervalMs: number;

  // ============================================================================
  // CONSTRUCTOR
  // ============================================================================

  /**
   * Initialize TokenClaimer
   *
   * Sets up the relayer client for gasless token redemption.
   *
   * @param {TokenClaimerConfig} config - Configuration object
   */
  constructor(config: TokenClaimerConfig) {
    this.signer = config.signer;

    /**
     * Use proxy address if provided (for web UI users)
     * Otherwise use signer address (for direct wallet users)
     */
    this.userAddress = config.proxyAddress || config.signer.address;

    /**
     * Set polling interval (default: 5 seconds)
     * This determines how often we check for redeemable positions
     */
    this.pollIntervalMs = config.pollIntervalMs || 5000;

    /**
     * Setup RelayClient for gasless transactions
     *
     * The relayer client:
     * - Signs transactions with your wallet
     * - Sends to Polymarket relayer
     * - Relayer pays gas and submits to blockchain
     */
    const provider = new JsonRpcProvider("https://polygon-rpc.com");
    const configBuilder = new BuilderConfig({
      localBuilderCreds: {
        key: config.builderKey,
        secret: config.builderSecret,
        passphrase: config.builderPassphrase,
      },
    });

    this.relayClient = new RelayClient(
      RELAYER_URL,
      POLYGON_CHAIN_ID,
      config.signer,
      configBuilder
    );
  }

  // ============================================================================
  // POLLING METHODS
  // ============================================================================

  /**
   * Start polling for redeemable positions and claiming them automatically
   *
   * This method:
   * 1. Does an immediate check for redeemable positions
   * 2. Sets up a periodic interval to check every pollIntervalMs
   * 3. Automatically claims any winning tokens found
   *
   * The polling continues until stopPolling() is called.
   */
  startPolling(): void {
    if (this.isPolling) {
      console.log("[CLAIMER] ⚠️ Already polling, skipping start");
      return;
    }

    this.isPolling = true;
    console.log(
      `[CLAIMER] 🚀 Starting automatic token claiming (polling every ${
        this.pollIntervalMs / 1000
      }s)`
    );
    console.log(`[CLAIMER] 👤 Checking positions for: ${this.userAddress}`);

    /**
     * Do an immediate check (don't wait for first interval)
     * This ensures we claim tokens as soon as polling starts
     */
    this.checkAndClaim().catch((err) => {
      console.error(`[CLAIMER] ❌ Initial check failed: ${err.message}`);
    });

    /**
     * Set up periodic polling
     * Checks every pollIntervalMs milliseconds
     */
    this.pollInterval = setInterval(() => {
      this.checkAndClaim().catch((err) => {
        console.error(`[CLAIMER] ❌ Polling error: ${err.message}`);
      });
    }, this.pollIntervalMs);
  }

  /**
   * Stop polling for redeemable positions
   *
   * Clears the polling interval and stops checking for redeemable tokens.
   * Called when bot shuts down or when starting a new market cycle.
   */
  stopPolling(): void {
    if (this.pollInterval) {
      clearInterval(this.pollInterval);
      this.pollInterval = null;
    }
    this.isPolling = false;
    console.log("[CLAIMER] 🛑 Stopped polling for token claims");
  }

  /**
   * Check for redeemable positions and claim them
   *
   * This method:
   * 1. Queries Polymarket Data API for redeemable positions
   * 2. Groups positions by conditionId (each market has one conditionId)
   * 3. For each conditionId, redeems both YES (index 1) and NO (index 2) slots
   * 4. Uses gasless relayer to execute redemption
   *
   * @returns {Promise<number>} Number of conditions successfully redeemed
   *
   * Note: Returns 0 if no redeemable positions found (this is normal)
   */
  async checkAndClaim(): Promise<number> {
    try {
      /**
       * Step 1: Fetch redeemable positions from Polymarket Data API
       *
       * The API returns all positions that:
       * - Are owned by the user
       * - Are in resolved markets
       * - Have winning outcomes
       * - Have not been redeemed yet
       */
      const url = `https://data-api.polymarket.com/positions?user=${this.userAddress}&redeemable=true`;
      const { data } = await axios.get(url);
      const positions = Array.isArray(data) ? data : [];

      /**
       * If no positions found, silently return
       * This is normal - most of the time there won't be redeemable positions
       */
      if (positions.length === 0) {
        return 0;
      }

      console.log(
        `[CLAIMER] 💰 Found ${positions.length} redeemable position(s)`
      );

      /**
       * Step 2: Group positions by conditionId
       *
       * Each market has one conditionId.
       * We can redeem all positions for a market in a single transaction.
       * Using Set to get unique conditionIds.
       */
      const conditionIds = new Set<string>();
      positions.forEach((p: any) => {
        if (p.conditionId) conditionIds.add(p.conditionId);
      });

      if (conditionIds.size === 0) {
        return 0;
      }

      console.log(
        `[CLAIMER] 🔗 Redeeming ${conditionIds.size} condition(s)...`
      );

      let successCount = 0;

      /**
       * Step 3: Redeem each condition
       *
       * For each unique conditionId:
       * 1. Format conditionId as hex string
       * 2. Encode redemption transaction data
       * 3. Execute via gasless relayer
       * 4. Wait for transaction confirmation
       */
      for (const conditionId of conditionIds) {
        try {
          /**
           * Format conditionId
           * Must be a hex string starting with 0x
           */
          const formattedConditionId = conditionId.startsWith("0x")
            ? (conditionId as `0x${string}`)
            : (`0x${conditionId}` as `0x${string}`);

          /**
           * Encode redemption transaction data
           *
           * Parameters:
           * - collateralToken: USDC address (what we're redeeming to)
           * - parentCollectionId: Zero bytes (root collection)
           * - conditionId: The market condition ID
           * - indexSets: [1n, 2n] - Redeem both YES (1) and NO (2) slots
           *
           * We redeem both slots because:
           * - If we have winning YES shares, slot 1 contains them
           * - If we have winning NO shares, slot 2 contains them
           * - Redeeming both ensures we get all winnings
           */
          const calldata = encodeFunctionData({
            abi: REDEEM_ABI,
            functionName: "redeemPositions",
            args: [
              USDC_ADDRESS,
              "0x0000000000000000000000000000000000000000000000000000000000000000",
              formattedConditionId,
              [1n, 2n], // Redeem both Yes (1) and No (2) slots
            ],
          });

          /**
           * Execute redemption via gasless relayer
           *
           * The relayer:
           * - Signs the transaction
           * - Pays the gas fees
           * - Submits to blockchain
           * - Returns a task ID for tracking
           */
          const task = await this.relayClient.execute([
            {
              to: CTF_ADDRESS, // Conditional Token Framework contract
              data: calldata, // Encoded redemption function call
              value: "0", // No ETH/MATIC sent (gasless)
            },
          ]);

          console.log(
            `[CLAIMER] ⏳ Condition ${conditionId.slice(
              0,
              10
            )}... - Relayer task ID: ${task.transactionID}`
          );

          /**
           * Wait for transaction confirmation
           *
           * The relayer submits the transaction and we wait for it to be mined.
           * This can take 10-30 seconds depending on network congestion.
           */
          const receipt = await task.wait();
          if (receipt) {
            console.log(
              `[CLAIMER] ✅ SUCCESS! Condition ${conditionId.slice(
                0,
                10
              )}... - Tx: ${receipt.transactionHash}`
            );
            console.log(
              `[CLAIMER]    View on PolygonScan: https://polygonscan.com/tx/${receipt.transactionHash}`
            );
            successCount++;
          } else {
            console.error(
              `[CLAIMER] ❌ Transaction failed or timed out for condition ${conditionId.slice(
                0,
                10
              )}...`
            );
          }
        } catch (e: any) {
          /**
           * Handle errors for individual condition redemption
           *
           * Common errors:
           * - Condition already redeemed
           * - Network timeout
           * - Relayer service error
           *
           * We log but continue to next condition
           */
          console.error(
            `[CLAIMER] ❌ Error redeeming condition ${conditionId.slice(
              0,
              10
            )}...: ${e.message}`
          );
        }
      }

      if (successCount > 0) {
        console.log(
          `[CLAIMER] ✅ Successfully redeemed ${successCount}/${conditionIds.size} condition(s)`
        );
      }

      return successCount;
    } catch (e: any) {
      /**
       * Handle API errors gracefully
       *
       * We don't throw errors because:
       * - Polling should continue even if one check fails
       * - Network errors are transient
       * - 404 (no positions) is normal, not an error
       */
      if (e.response?.status === 404 || e.message?.includes("404")) {
        // No positions found - this is normal, not an error
        return 0;
      }
      console.error(`[CLAIMER] ❌ Check failed: ${e.message}`);
      return 0;
    }
  }

  /**
   * Manually trigger a single check and claim
   *
   * Useful for:
   * - Testing redemption functionality
   * - One-time manual claims
   * - Debugging
   *
   * @returns {Promise<number>} Number of conditions successfully redeemed
   */
  async claimOnce(): Promise<number> {
    return this.checkAndClaim();
  }
}
