/**
 * POLYMARKET REST API SERVICE
 *
 * This service handles all REST API calls to Polymarket's Gamma API.
 *
 * Main functions:
 * - Fetching market token IDs (YES and NO tokens)
 * - Querying market information
 * - Getting market data
 *
 * The Gamma API is Polymarket's public REST API for market data.
 *
 * @class PolymarketService
 */
import axios from "axios";
import { DashboardManager } from "./DashboardManager";

export class PolymarketService {
  /**
   * Base URL for Polymarket's Gamma API
   * This is the public REST API endpoint
   */
  private baseUrl = "https://gamma-api.polymarket.com";

  /**
   * Get token IDs for a market
   *
   * Each Polymarket market has two tokens:
   * - YES token: Represents the "Yes" outcome
   * - NO token: Represents the "No" outcome
   *
   * These token IDs are required for:
   * - Placing orders
   * - Subscribing to WebSocket feeds
   * - Querying positions
   *
   * @param {string} slug - Market slug (e.g., "btc-updown-15m-1765895400")
   * @returns {Promise<string[]>} Array of token IDs [YES_token, NO_token]
   *
   * @throws Will return empty array if market not found or API error
   *
   * Example:
   * - Input: "btc-updown-15m-1765895400"
   * - Output: ["0x1234...", "0x5678..."]
   */
  async getMarketTokenIds(slug: string): Promise<string[]> {
    console.log(`[API] 🔍 Fetching Tokens for Slug: ${slug}`);

    try {
      /**
       * Step 1: Fetch event data from Gamma API
       *
       * The Events endpoint returns event information including markets.
       * We search for the event that matches our slug.
       *
       * Note: We include User-Agent header to prevent 403 blocks from some servers
       */
      const url = `https://gamma-api.polymarket.com/events?slug=${slug}`;
      const response = await axios.get(url, {
        headers: { "User-Agent": "Mozilla/5.0" }, // Prevent 403 blocks
      });

      /**
       * Check if event was found
       *
       * If no event found, possible reasons:
       * - Market doesn't exist yet (too far in future)
       * - Slug is incorrect
       * - API is temporarily unavailable
       */
      if (!response.data || response.data.length === 0) {
        console.warn(
          `[API] ⚠️ Event not found. Market might be too far in future.`
        );
        DashboardManager.logEvent(
          "SNIPER",
          `⚠️ API: Event not found (${slug})`
        );
        return [];
      }

      /**
       * Step 2: Extract market from event
       *
       * Events contain one or more markets.
       * For 15-minute Bitcoin markets, there's usually only one market per event.
       */
      const event = response.data[0];
      const markets = event.markets;

      if (!markets || markets.length === 0) {
        console.warn(`[API] ⚠️ Event found but no markets inside.`);
        DashboardManager.logEvent("SNIPER", `⚠️ API: No markets in event`);
        return [];
      }

      /**
       * Step 3: Get the main market
       *
       * For 15-minute crypto markets, there's typically only one market.
       * We take the first one.
       */
      const market = markets[0];

      /**
       * Step 4: Parse token IDs from market data
       *
       * Token IDs can be in different formats:
       * 1. String JSON: "[\"0x...\", \"0x...\"]"
       * 2. Array: ["0x...", "0x..."]
       * 3. tokens array: [{outcome: "Yes", token_id: "0x..."}, ...]
       *
       * We handle all three formats for maximum compatibility.
       */
      let clobTokenIds: string[] = [];

      // Format 1: String JSON
      if (typeof market.clobTokenIds === "string") {
        try {
          clobTokenIds = JSON.parse(market.clobTokenIds);
        } catch (e) {
          console.error(`[API] ❌ Failed to parse clobTokenIds: ${e}`);
          DashboardManager.logEvent("SNIPER", `❌ API: Parse error`);
          return [];
        }
      }
      // Format 2: Already an array
      else if (Array.isArray(market.clobTokenIds)) {
        clobTokenIds = market.clobTokenIds;
      }
      // Format 3: Fallback to tokens array
      else {
        /**
         * Fallback: Try tokens array with explicit YES/NO mapping
         * Some API responses use a tokens array instead of clobTokenIds
         */
        if (Array.isArray(market.tokens)) {
          const yesObj = market.tokens.find(
            (t: any) =>
              t.outcome === "Yes" || t.outcome === "yes" || t.outcome === "YES"
          );
          const noObj = market.tokens.find(
            (t: any) =>
              t.outcome === "No" || t.outcome === "no" || t.outcome === "NO"
          );

          if (yesObj?.token_id && noObj?.token_id) {
            console.log(
              `[API] ✅ Found Tokens via tokens array: YES ${yesObj.token_id.slice(
                0,
                6
              )}... | NO ${noObj.token_id.slice(0, 6)}...`
            );
            return [yesObj.token_id, noObj.token_id];
          }
        }

        console.error(`[API] ❌ No valid token format found in market`);
        DashboardManager.logEvent("SNIPER", `❌ API: Token format error`);
        return [];
      }

      /**
       * Step 5: Validate token IDs
       *
       * We need exactly 2 tokens (YES and NO)
       * If we have less, the market data is incomplete
       */
      if (clobTokenIds.length < 2) {
        console.error(`[API] ❌ clobTokenIds has less than 2 tokens`);
        DashboardManager.logEvent("SNIPER", `❌ API: Insufficient tokens`);
        return [];
      }

      /**
       * Step 6: Extract YES and NO token IDs
       *
       * Convention: First token is YES, second is NO
       */
      const yesTokenId = clobTokenIds[0];
      const noTokenId = clobTokenIds[1];

      console.log(
        `[API] ✅ Found Tokens: YES ${yesTokenId.slice(
          0,
          6
        )}... | NO ${noTokenId.slice(0, 6)}...`
      );
      return [yesTokenId, noTokenId];
    } catch (error: any) {
      /**
       * Handle API errors gracefully
       *
       * Common errors:
       * - Network timeout
       * - 404 (market not found)
       * - 500 (server error)
       *
       * We log the error and return empty array so the bot can retry
       */
      console.error(`[API] ❌ API Request Failed: ${error.message}`);
      DashboardManager.logEvent("SNIPER", `❌ API Error: ${error.message}`);
      return [];
    }
  }
}
