import { Wallet } from "@ethersproject/wallet";
import { Contract } from "@ethersproject/contracts";
import { JsonRpcProvider } from "@ethersproject/providers";
import axios from "axios";
import * as dotenv from "dotenv";

dotenv.config();

const PRIVATE_KEY = process.env.PRIVATE_KEY || "";
const RPC_URL = "https://polygon-rpc.com";
const CTF_ADDRESS = "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045";
const USDC_ADDRESS = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174";

// Extended ABI to read events
const CTF_ABI = [
  "function balanceOf(address owner, uint256 id) view returns (uint256)",
  "event TransferSingle(address indexed operator, address indexed from, address indexed to, uint256 id, uint256 value)",
  "event TransferBatch(address indexed operator, address indexed from, address indexed to, uint256[] ids, uint256[] values)",
];

const ERC20_ABI = [
  "function balanceOf(address owner) view returns (uint256)",
  "event Transfer(address indexed from, address indexed to, uint256 value)",
];

async function main() {
  console.log("🔍 Checking Redemption Transaction Status...\n");

  if (!PRIVATE_KEY) {
    console.error("❌ Error: Missing PRIVATE_KEY in .env");
    process.exit(1);
  }

  const provider = new JsonRpcProvider(RPC_URL);
  const signer = new Wallet(PRIVATE_KEY, provider);
  const signerAddress = await signer.getAddress();
  const proxyAddress = process.env.POLYMARKET_PROXY_ADDRESS || signerAddress;
  const userAddress = proxyAddress;

  console.log(`👤 Checking for: ${userAddress}\n`);

  // 1. Check current USDC balance
  const usdcContract = new Contract(USDC_ADDRESS, ERC20_ABI, provider);
  const usdcBalance = await usdcContract.balanceOf(userAddress);
  console.log(`💰 Current USDC Balance: ${Number(usdcBalance) / 1_000_000} USDC\n`);

  // 2. Check what positions are still redeemable via API
  console.log("📋 Checking API for redeemable positions...");
  try {
    const url = `https://data-api.polymarket.com/positions?user=${userAddress}&redeemable=true`;
    const { data } = await axios.get(url);
    
    if (Array.isArray(data) && data.length > 0) {
      console.log(`⚠️  API shows ${data.length} redeemable position(s):\n`);
      
      const conditionIds = new Set<string>();
      for (const pos of data) {
        if (pos.conditionId) {
          conditionIds.add(pos.conditionId);
          console.log(`   📦 Condition: ${pos.conditionId}`);
          console.log(`      Market: ${pos.marketSlug || "N/A"}`);
          console.log(`      Outcome: ${pos.outcome || "N/A"}`);
          console.log(`      Shares: ${pos.shares || "N/A"}`);
          if (pos.tokenId) {
            console.log(`      Token ID: ${pos.tokenId}`);
          }
          console.log();
        }
      }

      // 3. Check on-chain token balances for these conditions
      console.log("🔍 Checking on-chain token balances...\n");
      const ctfContract = new Contract(CTF_ADDRESS, CTF_ABI, provider);
      
      for (const conditionId of conditionIds) {
        // For binary markets, token IDs are derived from condition ID
        // YES token = conditionId (index 1)
        // NO token = conditionId + 1 (index 2)
        // But actually, we need the actual token IDs from the API
        
        // Check if we have token IDs from API
        const positions = data.filter((p: any) => p.conditionId === conditionId);
        const tokenIds = positions.map((p: any) => p.tokenId).filter(Boolean);
        
        if (tokenIds.length > 0) {
          console.log(`   Condition: ${conditionId.slice(0, 20)}...`);
          for (const tokenId of tokenIds) {
            try {
              const balance = await ctfContract.balanceOf(userAddress, tokenId);
              const balanceNum = Number(balance) / 1_000_000;
              console.log(`      Token ${tokenId.slice(0, 20)}...: ${balanceNum.toFixed(4)} shares`);
              
              if (balanceNum > 0.01) {
                console.log(`      ⚠️  Still has tokens! Redemption may not have worked.`);
              } else {
                console.log(`      ✅ No tokens (already redeemed)`);
              }
            } catch (e: any) {
              console.log(`      ⚠️  Could not check token ${tokenId.slice(0, 20)}...: ${e.message}`);
            }
          }
          console.log();
        } else {
          console.log(`   Condition: ${conditionId.slice(0, 20)}...`);
          console.log(`      ⚠️  No token IDs found in API response`);
          console.log();
        }
      }
    } else {
      console.log(`✅ API shows no redeemable positions\n`);
    }
  } catch (e: any) {
    console.error(`❌ Failed to check API: ${e.message}\n`);
  }

  // 4. Explain what redeemPositions does
  console.log("=".repeat(60));
  console.log("📚 What redeemPositions() Does:");
  console.log("=".repeat(60));
  console.log(`
The redeemPositions() function on the CTF contract:

1. Takes a conditionId (the resolved market)
2. Burns BOTH YES and NO tokens (indexSets [1, 2])
3. Returns the underlying USDC collateral to your wallet

For example:
- If you had 5 YES tokens @ $0.60 each = $3.00 invested
- And 5 NO tokens @ $0.40 each = $2.00 invested  
- Total invested: $5.00
- After market resolves, redeemPositions burns both tokens
- You get back: $5.00 USDC (the collateral)

⚠️  Important Notes:
- The transaction can "succeed" even if there are NO tokens to redeem
- If tokens were already redeemed, the transaction succeeds but does nothing
- Check token balances BEFORE redeeming to confirm you have tokens
- The Polymarket UI may take time to update after on-chain redemption
`);

  console.log("=".repeat(60));
  console.log("💡 Recommendation:");
  console.log("=".repeat(60));
  console.log(`
If the API still shows redeemable positions but transactions succeeded:
1. Check if you actually have token balances on-chain (see above)
2. If you have tokens, the redemption should work
3. If you DON'T have tokens, they may have been redeemed already
4. The Polymarket UI might just be slow to update

Run the redemption script again to retry:
  npm run redeem
`);
}

main().catch((error) => {
  console.error("❌ Fatal error:", error);
  process.exit(1);
});


