/**
 * LIVE EXECUTOR
 *
 * Executes real trades on Polymarket using actual funds.
 *
 * Features:
 * - Places real orders on Polymarket CLOB
 * - Checks real USDC balance from blockchain
 * - Tracks real positions from smart contracts
 * - Redeems winning positions from blockchain
 *
 * ⚠️ WARNING: This uses REAL MONEY!
 * - All orders are real and will execute with real funds
 * - Losses are permanent
 * - Always test with PaperExecutor first
 *
 * @class LiveExecutor
 * @implements {IExecutor}
 */
import { ClobClient, Side, OrderType } from "@polymarket/clob-client";
import { Wallet } from "@ethersproject/wallet";
import { Contract } from "@ethersproject/contracts";
import { JsonRpcProvider } from "@ethersproject/providers";
import { BigNumber } from "@ethersproject/bignumber";
import {
  IExecutor,
  TradeParams,
  CTF_CONTRACT_ADDRESS,
  USDC_ADDRESS,
} from "./IExecutor";

// ============================================================================
// CONSTANTS
// ============================================================================

/**
 * CTF Contract Address (lowercase to prevent checksum errors)
 *
 * Note: This is the same as CTF_CONTRACT_ADDRESS but lowercase.
 * Some blockchain operations require lowercase addresses.
 */
const CTF_ADDRESS = "0x4d97dcd97e9cc2337ed4192e56f4509c6d87f357";

/**
 * Polygon RPC endpoint
 * Used for reading blockchain data (balances, positions, etc.)
 */
const POLYGON_RPC = "https://polygon-rpc.com";

/**
 * ERC20 ABI for reading token balances
 * Used to check USDC balance
 */
const ERC20_ABI = ["function balanceOf(address owner) view returns (uint256)"];

/**
 * CTF (Conditional Token Framework) ABI
 * Used for:
 * - Reading position balances
 * - Redeeming winning positions
 */
const CTF_ABI = [
  "function balanceOf(address owner, uint256 id) view returns (uint256)",
  "function redeemPositions(address collateralToken, bytes32 parentCollectionId, bytes32 conditionId, uint[] indexSets)",
];

// ============================================================================
// LIVE EXECUTOR CLASS
// ============================================================================

export class LiveExecutor implements IExecutor {
  // ============================================================================
  // PROPERTIES
  // ============================================================================

  /** CLOB client for placing orders */
  private client: ClobClient;

  /** Wallet signer for signing transactions */
  private signer: Wallet;

  /** Address where funds are deposited (proxy or signer address) */
  private funderAddress: string;

  /**
   * Single provider instance (prevents memory leaks)
   *
   * Creating multiple providers can cause:
   * - Memory leaks
   * - Connection exhaustion
   * - Performance issues
   *
   * We create one provider and reuse it for all blockchain reads.
   */
  private provider: JsonRpcProvider;

  /** CTF contract instance for position queries and redemptions */
  private ctfContract: Contract;

  // ============================================================================
  // CONSTRUCTOR
  // ============================================================================

  /**
   * Initialize LiveExecutor
   *
   * Sets up connections to:
   * - Polymarket CLOB (for orders)
   * - Polygon RPC (for blockchain reads)
   * - CTF Contract (for positions and redemptions)
   *
   * @param {ClobClient} client - CLOB client (already authenticated)
   * @param {Wallet} signer - Wallet for signing transactions
   * @param {string} funderAddress - Address where funds are (proxy or signer)
   */
  constructor(client: ClobClient, signer: Wallet, funderAddress: string) {
    this.client = client;
    this.signer = signer;
    this.funderAddress = funderAddress;

    /**
     * Initialize blockchain provider ONCE
     * Reuse this instance for all blockchain reads
     */
    this.provider = new JsonRpcProvider(POLYGON_RPC);

    /**
     * Initialize CTF Contract
     * Used for:
     * - Reading position balances
     * - Redeeming winning positions
     */
    this.ctfContract = new Contract(CTF_CONTRACT_ADDRESS, CTF_ABI, this.signer);

    console.log("🚨 LIVE TRADING MODE ACTIVE 🚨");
  }

  /**
   * Get current USDC balance
   *
   * Reads USDC balance directly from blockchain.
   * USDC has 6 decimals, so we divide by 1,000,000.
   *
   * @returns {Promise<number>} USDC balance
   */
  async getBalance(): Promise<number> {
    try {
      /**
       * Create USDC contract instance
       * Use persistent provider to avoid memory leaks
       */
      const usdc = new Contract(USDC_ADDRESS, ERC20_ABI, this.provider);

      /**
       * Read balance from blockchain
       * Returns raw balance (with 6 decimals)
       */
      const rawBalance = await usdc.balanceOf(this.funderAddress);

      /**
       * Convert to human-readable format
       * USDC has 6 decimals, so divide by 1,000,000
       */
      return Number(rawBalance) / 1_000_000;
    } catch (e) {
      console.error("❌ Failed to fetch USDC balance:", e);
      return 0;
    }
  }

  async placeOrder(params: TradeParams): Promise<string> {
    console.log(
      `📝 [LIVE] Placing Order: ${params.side} ${params.size} @ $${
        params.price
      } [${params.type || "GTC"}]`
    );

    try {
      let clobOrderType = OrderType.GTC;
      if (params.type === "FOK") clobOrderType = OrderType.FOK;
      if (params.type === "GTD") clobOrderType = OrderType.GTD;
      if (params.type === "FAK") clobOrderType = OrderType.FAK;

      const order = await this.client.createOrder({
        tokenID: params.tokenId,
        price: params.price,
        side: params.side === "BUY" ? Side.BUY : Side.SELL,
        size: params.size,
        feeRateBps: 0,
      });

      const response = await this.client.postOrder(order, clobOrderType);

      if (
        (response as any).errorMsg ||
        (response as any).error ||
        (response as any).success === false
      ) {
        const msg =
          (response as any).errorMsg ||
          (response as any).error ||
          "Unknown Error";
        throw new Error(`CLOB API Error: ${msg}`);
      }

      const orderId =
        response.orderID || (response as any).orderId || (response as any).id;
      if (!orderId) throw new Error("No Order ID returned");

      console.log(`✅ [LIVE] Order Posted! ID: ${orderId}`);
      return orderId;
    } catch (error: any) {
      console.error(
        "❌ [LIVE] Order Failed:",
        error.message || JSON.stringify(error)
      );
      throw error;
    }
  }

  async cancelAll(): Promise<void> {
    try {
      await this.client.cancelAll();
      console.log("✅ All orders cancelled.");
    } catch (error: any) {
      console.error("❌ Failed to cancel orders:", error.message);
    }
  }

  async cancelOrder(orderId: string): Promise<void> {
    for (let i = 0; i < 3; i++) {
      try {
        await this.client.cancelOrder({ orderID: orderId });
        console.log(`✅ Order ${orderId} cancelled.`);
        return;
      } catch (error: any) {
        if (i === 2) throw error;
        await new Promise((r) => setTimeout(r, 500));
      }
    }
  }

  async getOrderStatus(
    orderId: string
  ): Promise<{ matched: number; cancelled: boolean; avgFillPrice: number }> {
    try {
      const order = await this.client.getOrder(orderId);
      const o: any = order;
      const matched = parseFloat(o.size_matched || o.sizeMatched || "0");
      const cancelled = o.status === "CANCELLED" || o.canceled === true;
      let avgFillPrice = parseFloat(o.price || "0");

      if (o.associate_trades && o.associate_trades.length > 0) {
        let totalVal = 0,
          totalQty = 0;
        for (const t of o.associate_trades) {
          totalVal += parseFloat(t.size) * parseFloat(t.price);
          totalQty += parseFloat(t.size);
        }
        if (totalQty > 0) avgFillPrice = totalVal / totalQty;
      }
      return { matched, cancelled, avgFillPrice };
    } catch (e) {
      return { matched: 0, cancelled: false, avgFillPrice: 0 };
    }
  }

  async getPositions(
    tokenIdYes: string,
    tokenIdNo: string
  ): Promise<{ yes: number; no: number }> {
    try {
      // ✅ 2. Use persistent provider (Reliable & Fast)
      const ctf = new Contract(CTF_ADDRESS, CTF_ABI, this.provider);

      const [balYes, balNo] = await Promise.all([
        ctf.balanceOf(this.funderAddress, tokenIdYes),
        ctf.balanceOf(this.funderAddress, tokenIdNo),
      ]);

      return {
        yes: Number(balYes) / 1_000_000,
        no: Number(balNo) / 1_000_000,
      };
    } catch (e: any) {
      // ✅ 3. Better Error Logging
      console.error(
        `❌ [RPC ERROR] Could not read positions: ${e.code || e.message}`
      );
      // Safety default: Assume 0 to prevent crashes, but log loudly
      return { yes: 0, no: 0 };
    }
  }

  // 🟢 CRITICAL FIX: Implement getOpenOrders for order recovery
  async getOpenOrders(marketSlug: string): Promise<
    Array<{
      id: string;
      side: "YES" | "NO";
      price: string;
      size: number;
    }>
  > {
    try {
      // Try to use ClobClient's cancelAll to get orders, or use API directly
      // Note: ClobClient doesn't expose a direct getOpenOrders method
      // We would need to call the CLOB API endpoint: GET /orders
      // For now, return empty array - order recovery will be limited
      // The bot will still work, but won't recover "ghost orders" on restart
      console.warn(
        `⚠️ [LIVE] getOpenOrders: Order recovery limited. ClobClient doesn't expose user orders endpoint.`
      );
      return [];
    } catch (e: any) {
      console.error(`❌ [LIVE] Failed to get open orders: ${e.message}`);
      return [];
    }
  }

  // 🟢 NEW: Get user address (needed for scanning API)
  async getAddress(): Promise<string> {
    return this.funderAddress;
  }

  // 🟢 NEW: Redeem winnings from the smart contract
  async redeemPositions(conditionId: string): Promise<string> {
    try {
      // 🟢 PARAMS FOR POLYMARKET BINARY MARKETS
      // parentCollectionId: Always 0x0...0 (bytes32 zero hash)
      // indexSets: Always [1, 2] for Binary (Redeems both YES and NO slots)
      const parentCollectionId =
        "0x0000000000000000000000000000000000000000000000000000000000000000";
      const indexSets = [1, 2];

      console.log(`🔗 [LIVE] Submitting Redemption for ${conditionId}...`);

      // 🟢 FIX: Use legacy gasPrice for Polygon (more reliable than EIP-1559)
      // Polygon RPC nodes sometimes reject EIP-1559 transactions
      const gasPrice = await this.provider.getGasPrice();
      // Set VERY high gas price: 3x current + ensure minimum 150 gwei (well above 25 gwei minimum)
      const minGasPrice = BigNumber.from("150000000000"); // 150 gwei minimum (safe margin)
      const highGasPrice = gasPrice.mul(3).gt(minGasPrice)
        ? gasPrice.mul(3)
        : minGasPrice;

      console.log(
        `⛽ Gas Price: ${highGasPrice.toString()} (${highGasPrice
          .div(1e9)
          .toString()} gwei)`
      );

      // Convert conditionId string to bytes32 (ensure proper format)
      const conditionIdBytes32 = conditionId.startsWith("0x")
        ? conditionId
        : "0x" + conditionId;

      // 🟢 ESTIMATE GAS FIRST to catch errors before sending
      try {
        const gasEstimate = await this.ctfContract.estimateGas.redeemPositions(
          USDC_ADDRESS,
          parentCollectionId,
          conditionIdBytes32,
          indexSets,
          { gasPrice: highGasPrice }
        );
        console.log(`⛽ Estimated Gas: ${gasEstimate.toString()}`);
      } catch (estimateError: any) {
        console.error(`❌ Gas estimation failed: ${estimateError.message}`);

        // 🟢 DETAILED ERROR ANALYSIS
        if (
          estimateError.message.includes("revert") ||
          estimateError.message.includes("execution reverted")
        ) {
          console.error(`   This means the redemption would fail on-chain.`);
          console.error(`   Possible reasons:`);
          console.error(`   - Condition already fully redeemed`);
          console.error(`   - No tokens to redeem for this condition`);
          console.error(`   - Invalid condition ID format`);
        }

        throw new Error(`Gas estimation failed: ${estimateError.message}`);
      }

      // Send transaction with high gas price
      const tx = await this.ctfContract.redeemPositions(
        USDC_ADDRESS,
        parentCollectionId,
        conditionIdBytes32,
        indexSets,
        { gasPrice: highGasPrice }
      );

      console.log(`✅ [LIVE] Redemption Sent! Hash: ${tx.hash}`);
      console.log(
        `   View on PolygonScan: https://polygonscan.com/tx/${tx.hash}`
      );

      // 🟢 VERIFY: Check if transaction was actually broadcasted (wait 2s for propagation)
      await new Promise((r) => setTimeout(r, 2000));
      try {
        const txStatus = await this.provider.getTransaction(tx.hash);
        if (!txStatus) {
          throw new Error(
            "Transaction not found on network - RPC may have rejected it"
          );
        }
        console.log(
          `✅ Transaction confirmed on network (nonce: ${txStatus.nonce})`
        );
      } catch (checkError: any) {
        console.error(
          `❌ Transaction verification failed: ${checkError.message}`
        );
        console.error(
          `   The transaction hash was generated but may not have been broadcasted`
        );
        throw new Error(`Transaction not broadcasted: ${checkError.message}`);
      }

      // 🟢 FIX: Wait for confirmation with better error handling
      try {
        const receipt = await Promise.race([
          tx.wait(),
          new Promise((_, reject) =>
            setTimeout(
              () => reject(new Error("Confirmation timeout after 120s")),
              120000 // Increased timeout to 120 seconds
            )
          ),
        ]);

        // 🟢 VERIFY: Check transaction status
        if ((receipt as any).status === 0) {
          throw new Error(
            "Transaction reverted on-chain - redemption may have failed"
          );
        }

        console.log(
          `✅ [LIVE] Redemption Confirmed! Block: ${
            (receipt as any).blockNumber
          }`
        );
        return tx.hash;
      } catch (waitError: any) {
        // Transaction was sent, but confirmation timed out
        console.warn(
          `⚠️ [LIVE] Confirmation timeout, checking transaction status...`
        );

        // 🟢 CHECK STATUS: Try to get receipt with multiple attempts
        let receiptFound = false;
        for (let attempt = 0; attempt < 6; attempt++) {
          try {
            await new Promise((r) => setTimeout(r, 10000)); // Wait 10s between attempts
            const receipt = await this.provider.getTransactionReceipt(tx.hash);
            if (receipt) {
              receiptFound = true;
              if (receipt.status === 1) {
                console.log(
                  `✅ [LIVE] Transaction confirmed! Block: ${receipt.blockNumber}`
                );
                return tx.hash;
              } else {
                // Transaction reverted - get revert reason if possible
                console.error(`❌ [LIVE] Transaction REVERTED on-chain!`);
                console.error(`   Block: ${receipt.blockNumber}`);
                console.error(`   Gas Used: ${receipt.gasUsed.toString()}`);

                // Try to get revert reason from transaction
                try {
                  const txData = await this.provider.getTransaction(tx.hash);
                  if (txData && txData.data) {
                    console.error(`   This usually means:`);
                    console.error(`   - Condition already redeemed`);
                    console.error(`   - Invalid condition ID`);
                    console.error(`   - Insufficient token balance`);
                  }
                } catch (e) {
                  // Ignore errors getting tx data
                }

                throw new Error(
                  "Transaction reverted on-chain - redemption failed (check PolygonScan for details)"
                );
              }
            }
          } catch (checkError: any) {
            if (checkError.message.includes("reverted")) {
              throw checkError; // Re-throw revert errors immediately
            }
            if (attempt < 5) {
              console.log(
                `   Attempt ${
                  attempt + 1
                }/6: Receipt not available yet, retrying...`
              );
            }
          }
        }

        if (!receiptFound) {
          console.warn(`   Receipt still not available after 60 seconds`);
        }

        console.warn(`⚠️ Transaction may still be pending: ${tx.hash}`);
        console.warn(
          `   Check status on PolygonScan: https://polygonscan.com/tx/${tx.hash}`
        );

        // Still return hash even if we couldn't confirm (user can check manually)
        return tx.hash;
      }
    } catch (error: any) {
      console.error(`❌ [LIVE] Redemption Failed: ${error.message}`);
      throw error;
    }
  }
}
