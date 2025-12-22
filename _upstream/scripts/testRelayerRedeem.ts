import { Wallet } from "@ethersproject/wallet";
import { JsonRpcProvider } from "@ethersproject/providers";
import { RelayClient } from "@polymarket/builder-relayer-client";
import { BuilderConfig } from "@polymarket/builder-signing-sdk";
import { encodeFunctionData, parseAbi } from "viem";
import axios from "axios";
import * as dotenv from "dotenv";

dotenv.config();

// --- CONFIGURATION ---
const RELAYER_URL = "https://relayer-v2.polymarket.com";
const POLYGON_CHAIN_ID = 137;
const CTF_ADDRESS = "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045";
const USDC_ADDRESS = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174";

// Polymarket keys from .env
const PRIVATE_KEY = process.env.PRIVATE_KEY || "";
const BUILDER_KEY = process.env.POLYMARKET_API_KEY || "";
const BUILDER_SECRET = process.env.POLYMARKET_API_SECRET || "";
const BUILDER_PASSPHRASE = process.env.POLYMARKET_PASSPHRASE || "";

// ABI for viem encoding
const REDEEM_ABI = parseAbi([
  "function redeemPositions(address collateralToken, bytes32 parentCollectionId, bytes32 conditionId, uint256[] indexSets)",
]);

async function main() {
  console.log("🚀 Starting GASLESS Redemption Test...");

  if (!PRIVATE_KEY || !BUILDER_KEY) {
    console.error(
      "❌ Missing Keys in .env (PRIVATE_KEY, POLY_BUILDER_KEY, etc)"
    );
    process.exit(1);
  }

  // 1. Setup Client
  const provider = new JsonRpcProvider("https://polygon-rpc.com");
  const signer = new Wallet(PRIVATE_KEY, provider);
  const signerAddress = await signer.getAddress();

  // Check if using proxy address (Polymarket web users)
  const proxyAddress = process.env.POLYMARKET_PROXY_ADDRESS || signerAddress;
  const userAddress = proxyAddress; // Use proxy if set, otherwise use signer

  const config = new BuilderConfig({
    localBuilderCreds: {
      key: BUILDER_KEY,
      secret: BUILDER_SECRET,
      passphrase: BUILDER_PASSPHRASE,
    },
  });

  const relayClient = new RelayClient(
    RELAYER_URL,
    POLYGON_CHAIN_ID,
    signer,
    config
  );

  console.log(`👤 Signer Wallet: ${signerAddress}`);
  if (proxyAddress !== signerAddress) {
    console.log(`🔐 Proxy Address: ${proxyAddress}`);
  }
  console.log(`🔍 Checking positions for: ${userAddress}\n`);

  // 2. Scan for Redeemable Positions
  console.log("🔍 Scanning for winning tickets...");
  const url = `https://data-api.polymarket.com/positions?user=${userAddress}&redeemable=true`;
  console.log(`   API URL: ${url}\n`);

  let positions: any[] = [];
  try {
    const { data } = await axios.get(url);
    positions = Array.isArray(data) ? data : [];
    console.log(`📊 API Response: Found ${positions.length} position(s)\n`);

    // Debug: Show what we got
    if (positions.length > 0) {
      console.log("📋 Position Details:");
      positions.forEach((p, idx) => {
        console.log(`   [${idx + 1}] Condition ID: ${p.conditionId || "N/A"}`);
        console.log(`       Market: ${p.marketSlug || "N/A"}`);
        console.log(`       Outcome: ${p.outcome || "N/A"}`);
        console.log(`       Shares: ${p.shares || "N/A"}`);
        if (p.tokenId) console.log(`       Token ID: ${p.tokenId}`);
        console.log();
      });
    }
  } catch (e: any) {
    console.error("❌ Data API Error:", e.message);
    if (e.response) {
      console.error(`   Status: ${e.response.status}`);
      console.error(`   Data:`, JSON.stringify(e.response.data, null, 2));
    }
    process.exit(1);
  }

  const conditionIds = new Set<string>();
  positions.forEach((p) => {
    if (p.conditionId) conditionIds.add(p.conditionId);
  });

  if (conditionIds.size === 0) {
    console.log("⚠️  No redeemable positions found via API.");
    console.log("\n💡 Possible reasons:");
    console.log("   - Positions may have already been redeemed");
    console.log("   - Markets may not be resolved yet");
    console.log(
      "   - If using Polymarket web UI, check if you're using a proxy address"
    );
    console.log(
      "   - Try setting POLYMARKET_PROXY_ADDRESS in .env if you use web UI"
    );
    console.log("\n🔍 Checking all positions (not just redeemable)...");

    // Try checking all positions
    try {
      const allPositionsUrl = `https://data-api.polymarket.com/positions?user=${userAddress}`;
      const { data: allData } = await axios.get(allPositionsUrl);
      const allPositions = Array.isArray(allData) ? allData : [];
      console.log(`   Found ${allPositions.length} total position(s)`);

      if (allPositions.length > 0) {
        console.log("\n📋 All Positions:");
        allPositions.slice(0, 5).forEach((p: any, idx: number) => {
          console.log(
            `   [${idx + 1}] ${p.marketSlug || "N/A"} - ${p.outcome || "N/A"}`
          );
          console.log(`       Redeemable: ${p.redeemable ? "Yes" : "No"}`);
          console.log(`       Condition: ${p.conditionId || "N/A"}`);
        });
        if (allPositions.length > 5) {
          console.log(`   ... and ${allPositions.length - 5} more`);
        }
      }
    } catch (e: any) {
      console.log(`   Could not fetch all positions: ${e.message}`);
    }

    process.exit(0);
  }

  console.log(`💰 Found ${conditionIds.size} markets to redeem.`);

  // 3. Execute Gasless Redemptions
  for (const conditionId of conditionIds) {
    console.log(`\n--------------------------------------------------`);
    console.log(`🔗 Redeeming Condition: ${conditionId}`);

    // Ensure conditionId is properly formatted as hex string
    const formattedConditionId = conditionId.startsWith("0x")
      ? (conditionId as `0x${string}`)
      : (`0x${conditionId}` as `0x${string}`);

    // Encode Data for Relayer
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

    try {
      const task = await relayClient.execute([
        {
          to: CTF_ADDRESS,
          data: calldata,
          value: "0",
        },
      ]);

      console.log(`⏳ Relayer accepted task. ID: ${task.transactionID}`);
      console.log(`   Waiting for confirmation...`);

      const receipt = await task.wait();
      if (!receipt) {
        console.error(`❌ Transaction failed or timed out`);
        continue;
      }
      console.log(`✅ SUCCESS! Tx Hash: ${receipt.transactionHash}`);
      console.log(
        `   View on PolygonScan: https://polygonscan.com/tx/${receipt.transactionHash}`
      );
    } catch (e: any) {
      console.error(`❌ Relayer Error: ${e.message}`);
      if (e.stack) {
        console.error(`   Stack: ${e.stack}`);
      }
    }
  }

  console.log(`\n✅ Redemption process complete!`);
}

main().catch((error) => {
  console.error("❌ Fatal Error:", error);
  process.exit(1);
});
