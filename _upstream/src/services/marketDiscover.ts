import { ClobClient } from "@polymarket/clob-client";
import { Wallet } from "@ethersproject/wallet";
import axios from "axios";

// --- CONFIGURATION ---
const RPC_URL = "https://polygon-rpc.com"; // Standard Polygon RPC
const CHAIN_ID = 137; // Polygon Mainnet

// SETUP
const mockSigner = new Wallet(Wallet.createRandom().privateKey);
const client = new ClobClient(
  "https://clob.polymarket.com",
  CHAIN_ID,
  mockSigner
);

interface MarketOpportunity {
  question: string;
  tokenId: string;
  bestBid: number;
  bestAsk: number;
  spread: number;
  volume: number;
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * Calculates the exact URL slugs for the Current and Next 15m BTC markets.
 * Polymarket 15m slugs end with the Unix Timestamp of the EXPIRATION time.
 */
function getBtcSlugs() {
  const nowSeconds = Math.floor(Date.now() / 1000);
  const interval = 900; // 15 minutes in seconds

  // Math.ceil(now / 900) * 900 = The next 15-minute mark (Expiration of CURRENT market)
  const currentExpiration = Math.ceil(nowSeconds / interval) * interval;

  // The NEXT market expires 15 minutes after that
  const nextExpiration = currentExpiration + interval;

  return {
    current: `btc-updown-15m-${currentExpiration}`, // The one resolving right now
    next: `btc-updown-15m-${nextExpiration}`, // The one opening for bets
  };
}

async function fetchSpecificMarket(slug: string) {
  try {
    console.log(`📡 Fetching metadata for slug: ${slug}`);

    // We use the '/markets' endpoint filtering by slug
    const url = `https://gamma-api.polymarket.com/markets?slug=${slug}`;
    const { data } = await axios.get(url);

    // Gamma API can return an array or object wrapper
    const markets = Array.isArray(data) ? data : data.results || [];

    if (markets.length === 0) {
      console.log(`⚠️ Market not found or not active yet: ${slug}`);
      return null;
    }

    // Grab the first matching market
    const market = markets[0];
    return market;
  } catch (error: any) {
    console.error(`❌ API Error for ${slug}:`, error.message);
    return null;
  }
}

async function checkOrderBook(market: any) {
  // 1. EXTRACT TOKEN ID
  // The API often returns 'clobTokenIds' as a STRINGIFIED JSON array "['123', '456']"
  let tokenId = "";

  if (market.clobTokenIds && typeof market.clobTokenIds === "string") {
    try {
      const parsed = JSON.parse(market.clobTokenIds);
      tokenId = parsed[0]; // Usually [0] is YES/UP, [1] is NO/DOWN
    } catch (e) {
      console.log("⚠️ Could not parse clobTokenIds string.");
    }
  } else if (Array.isArray(market.tokens)) {
    tokenId = market.tokens[0]?.token_id;
  }

  if (!tokenId) {
    console.log("❌ Could not determine Token ID.");
    return;
  }

  console.log(`🔑 Token ID: ${tokenId}`);

  // 2. FETCH ORDERBOOK
  try {
    const book = await client.getOrderBook(tokenId);

    if (book.bids.length > 0 && book.asks.length > 0) {
      const bestBid = parseFloat(book.bids[0].price);
      const bestAsk = parseFloat(book.asks[0].price);
      const spread = bestAsk - bestBid;

      console.log(`\n📊 ORDERBOOK STATUS:`);
      console.log(`   Bid: ${bestBid.toFixed(2)}`);
      console.log(`   Ask: ${bestAsk.toFixed(2)}`);
      console.log(`   Spread: ${spread.toFixed(4)}`);

      if (spread < 0.05) console.log("   ✅ Market is LIQUID.");
      else console.log("   ⚠️ Spread is WIDE.");
    } else {
      console.log("   ❌ Orderbook empty (No liquidity yet).");
    }
  } catch (e) {
    console.log("   ⚠️ Failed to fetch CLOB/Orderbook.");
  }
}

// --- MAIN EXECUTION ---
if (require.main === module) {
  (async () => {
    // 1. Get Calculated Slugs
    const slugs = getBtcSlugs();

    console.log(`🕒 Time Check: ${new Date().toISOString()}`);
    console.log(`Running Market Slug: ${slugs.current}`);
    console.log(`Next Market Slug:    ${slugs.next}`);
    console.log("------------------------------------------------");

    // 2. Try to fetch the NEXT market first (usually the betting target)
    console.log("👉 Checking NEXT market (Target)...");
    let market = await fetchSpecificMarket(slugs.next);

    // 3. If Next isn't found, fallback to Current
    if (!market) {
      console.log(
        "\n👉 'Next' market not found. Checking 'Current' (Running)..."
      );
      market = await fetchSpecificMarket(slugs.current);
    }

    // 4. If we found a market, check the books
    if (market) {
      console.log(`\n✅ FOUND: ${market.question}`);
      await checkOrderBook(market);
    } else {
      console.log("\n❌ Could not find active BTC 15m markets.");
    }
  })();
}
