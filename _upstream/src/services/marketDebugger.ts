import { ClobClient } from "@polymarket/clob-client";
import { ethers } from "ethers";
import axios from "axios";

// 1. SETUP (Mock Wallet for Read-Only Access)
const mockSigner = new ethers.Wallet(ethers.Wallet.createRandom().privateKey);
const client = new ClobClient("https://clob.polymarket.com", 137);

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

async function runDebug() {
  console.log("🔍 STARTING DEEP DEBUG SCAN...");

  // 2. FETCH MARKETS (Broadest possible search: Active, High Volume)
  const url =
    "https://gamma-api.polymarket.com/markets?active=true&closed=false&order=volume24hr&limit=10";
  console.log(`📡 Fetching from: ${url}`);

  try {
    const { data } = await axios.get(url);
    const markets = Array.isArray(data) ? data : data.results || [];
    console.log(
      `✅ API returned ${markets.length} markets. Analyzing each one...\n`
    );

    for (const market of markets) {
      console.log(`------------------------------------------------`);
      console.log(`📝 Question: ${market.question.substring(0, 50)}...`);

      // 3. DEBUG TOKEN ID
      let tokenId = market.tokens?.[0]?.token_id;
      if (!tokenId && market.clobTokenIds) {
        try {
          const parsed = JSON.parse(market.clobTokenIds);
          tokenId = parsed[0];
        } catch {
          tokenId = market.clobTokenIds; // Try direct string
        }
      }

      if (!tokenId) {
        console.log(`❌ FAIL: Could not find Token ID. Skipping.`);
        continue;
      }
      console.log(`🔑 Token ID: ${tokenId.substring(0, 15)}...`);

      // 4. FETCH ORDERBOOK
      try {
        await sleep(250); // Slight delay to be nice to API
        const book = await client.getOrderBook(tokenId);

        if (book.bids.length === 0 || book.asks.length === 0) {
          console.log(`❌ FAIL: Empty Orderbook (No bids or asks).`);
          continue;
        }

        const bestBid = parseFloat(book.bids[0].price);
        const bestAsk = parseFloat(book.asks[0].price);
        const spread = bestAsk - bestBid;

        console.log(`💰 BID: ${bestBid} | ASK: ${bestAsk}`);
        console.log(
          `📉 SPREAD: ${spread.toFixed(4)} (${(spread * 100).toFixed(2)} cents)`
        );

        // 5. VERDICT
        if (spread <= 0.05) {
          console.log(`✅ PASS: 🔥 PERFECT GABAGOOL CANDIDATE!`);
        } else if (spread <= 0.1) {
          console.log(`⚠️ OKAY: Decent, but slightly wide.`);
        } else {
          console.log(`❌ REJECT: Spread too wide (> 10 cents).`);
        }
      } catch (error: any) {
        console.log(`❌ ERROR calling CLOB: ${error.message}`);
      }
    }
  } catch (error: any) {
    console.error("FATAL ERROR:", error.message);
  }
}

runDebug();
