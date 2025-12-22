/**
 * MARKET MATH UTILITIES
 *
 * This class provides utility functions for calculating Bitcoin 15-minute market information.
 *
 * Bitcoin markets run on 15-minute intervals (e.g., 12:00, 12:15, 12:30, 12:45).
 * Each market has a slug format: btc-updown-15m-{epoch_seconds}
 *
 * Example:
 * - At 12:53, the active market is btc-updown-15m-1765895400 (started at 12:45)
 * - The next market is btc-updown-15m-1765896300 (starts at 13:00)
 *
 * @class MarketMath
 */
export class MarketMath {
  /**
   * 15-minute interval in seconds
   * Bitcoin markets run every 15 minutes (900 seconds)
   */
  private static INTERVAL_SECONDS = 900; // 15 minutes

  /**
   * Get the currently active market
   *
   * Returns the market that is currently running.
   * Uses Math.floor to round down to the start of the current 15-minute interval.
   *
   * Example:
   * - Current time: 12:53:30
   * - Current interval start: 12:45:00
   * - Returns: btc-updown-15m-1765895400 (market that started at 12:45)
   *
   * @returns {Object} Market information with slug, start time, and end time
   * @returns {string} slug - Market slug (e.g., "btc-updown-15m-1765895400")
   * @returns {number} startTimeMs - Market start time in milliseconds
   * @returns {number} endTimeMs - Market end time in milliseconds
   */
  static getTargetMarketSlug(): {
    slug: string;
    startTimeMs: number;
    endTimeMs: number;
  } {
    const nowMs = Date.now();
    const nowSec = Math.floor(nowMs / 1000);

    /**
     * Calculate the start of the current 15-minute interval
     *
     * Example:
     * - nowSec = 1765895610 (12:53:30)
     * - INTERVAL_SECONDS = 900
     * - currentStartSec = floor(1765895610 / 900) * 900 = 1962106 * 900 = 1765895400 (12:45:00)
     */
    const currentStartSec =
      Math.floor(nowSec / this.INTERVAL_SECONDS) * this.INTERVAL_SECONDS;

    return {
      slug: `btc-updown-15m-${currentStartSec}`,
      startTimeMs: currentStartSec * 1000,
      endTimeMs: (currentStartSec + this.INTERVAL_SECONDS) * 1000,
    };
  }

  /**
   * Get the next market (the one that will start after the current one ends)
   *
   * This is used when the bot wants to wait for the current market to end
   * and then trade the next market.
   *
   * Example:
   * - Current time: 12:53:30
   * - Current market: 12:45-13:00 (ends at 13:00)
   * - Next market: 13:00-13:15 (starts at 13:00)
   * - Returns: btc-updown-15m-1765896300
   *
   * @returns {Object} Next market information
   */
  static getNextMarketSlug(): {
    slug: string;
    startTimeMs: number;
    endTimeMs: number;
  } {
    const now = new Date();

    /**
     * Step 1: Calculate 15-minute interval boundaries
     * Convert 15 minutes to milliseconds for calculation
     */
    const msPer15Min = 15 * 60 * 1000;
    const currentIntervalStart =
      Math.floor(now.getTime() / msPer15Min) * msPer15Min;

    /**
     * Step 2: Calculate next market start time
     * Next market starts when current interval ends
     */
    const nextMarketStartTime = currentIntervalStart + msPer15Min;

    /**
     * Step 3: Convert to epoch seconds for the slug
     * Polymarket uses epoch seconds in the market slug
     */
    const epochSeconds = Math.floor(nextMarketStartTime / 1000);

    /**
     * Step 4: Construct the market slug
     * Format: btc-updown-15m-{epoch_seconds}
     */
    const slug = `btc-updown-15m-${epochSeconds}`;

    /**
     * Step 5: Calculate end time
     * Market ends 15 minutes after it starts
     */
    const endTimeMs = nextMarketStartTime + msPer15Min;

    return {
      slug: slug,
      startTimeMs: nextMarketStartTime,
      endTimeMs: endTimeMs,
    };
  }

  /**
   * Calculate milliseconds until a given timestamp
   *
   * Used for:
   * - Scheduling market end timers
   * - Calculating wait times
   * - Displaying countdown timers
   *
   * @param {number} timestampMs - Target timestamp in milliseconds
   * @returns {number} Milliseconds until timestamp (never negative)
   *
   * Example:
   * - timestampMs = Date.now() + 60000 (1 minute from now)
   * - Returns: 60000
   *
   * - timestampMs = Date.now() - 1000 (1 second ago)
   * - Returns: 0 (already passed)
   */
  static getMsUntil(timestampMs: number): number {
    return Math.max(0, timestampMs - Date.now());
  }

  /**
   * Get the market after the next one
   *
   * Used for pre-market order placement (placing orders for the market
   * after the next one, before it starts).
   *
   * Example:
   * - Current time: 12:53
   * - Current market: 12:45-13:00
   * - Next market: 13:00-13:15
   * - Market after next: 13:15-13:30
   *
   * @returns {Object} Market after next information
   */
  static getMarketAfterNext(): {
    slug: string;
    startTimeMs: number;
    endTimeMs: number;
  } {
    const nowMs = Date.now();
    const nowSec = Math.floor(nowMs / 1000);

    /**
     * Calculate next market start using Math.ceil
     * This rounds UP to the next interval boundary
     *
     * Example:
     * - nowSec = 1765895610 (12:53:30)
     * - ceil(1765895610 / 900) * 900 = 1962107 * 900 = 1765896300 (13:00:00)
     */
    const nextStartSec =
      Math.ceil(nowSec / this.INTERVAL_SECONDS) * this.INTERVAL_SECONDS;

    /**
     * Market after next starts one interval after next market
     */
    const afterNextStartSec = nextStartSec + this.INTERVAL_SECONDS;

    return {
      slug: `btc-updown-15m-${afterNextStartSec}`,
      startTimeMs: afterNextStartSec * 1000,
      endTimeMs: (afterNextStartSec + this.INTERVAL_SECONDS) * 1000,
    };
  }
}
