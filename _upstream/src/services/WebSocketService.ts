/**
 * WEBSOCKET SERVICE
 *
 * Manages WebSocket connections to Polymarket for real-time data.
 *
 * Two types of connections:
 * 1. Market Channel: Public price data (no auth required)
 *    - Real-time order book updates
 *    - Price changes
 *    - Market data
 *
 * 2. User Channel: Private trade data (auth required)
 *    - Your order fills
 *    - Your trade events
 *    - Order status updates
 *
 * Features:
 * - Automatic reconnection on disconnect
 * - Exponential backoff for reconnection
 * - HMAC-SHA256 authentication for user channel
 * - Connection health monitoring
 *
 * @class WebSocketService
 */
import { ClobClient } from "@polymarket/clob-client";
import WebSocket from "ws";
import { createHmac } from "crypto";

export class WebSocketService {
  // ============================================================================
  // PROPERTIES
  // ============================================================================

  /** CLOB client (used for authentication context) */
  private client: ClobClient;

  /** Market WebSocket connection (public price data) */
  private marketWs: WebSocket | null = null;

  /** User WebSocket connection (private trade data) */
  private userWs: WebSocket | null = null;

  /** Active token IDs being monitored on market channel */
  private activeTokenIds: string[] = [];

  /** Callback for market channel messages */
  private onMarketMessage: ((data: any) => void) | null = null;

  /** Callback for user channel messages */
  private onUserMessage: ((data: any) => void) | null = null;

  /**
   * Store credentials for reconnection
   * When user channel disconnects, we need credentials to reconnect
   */
  private userCreds: {
    key: string;
    secret: string;
    passphrase: string;
  } | null = null;

  /**
   * Reconnection attempt tracking
   * Prevents infinite reconnection loops
   */
  private reconnectAttempts: number = 0;
  private readonly MAX_RECONNECT_ATTEMPTS = 10;

  // ============================================================================
  // CONSTRUCTOR
  // ============================================================================

  /**
   * Initialize WebSocketService
   *
   * @param {ClobClient} client - CLOB client for authentication context
   */
  constructor(client: ClobClient) {
    this.client = client;
  }

  // ============================================================================
  // MARKET CHANNEL (Public Price Data)
  // ============================================================================

  /**
   * Subscribe to market channel for public price data
   *
   * This channel provides:
   * - Real-time order book updates
   * - Price changes
   * - Market depth
   *
   * No authentication required - this is public data.
   *
   * @param {string[]} tokenIds - Array of token IDs to subscribe to (YES and NO)
   * @param {Function} onMessage - Callback function for received messages
   */
  async subscribeMarket(tokenIds: string[], onMessage: (data: any) => void) {
    // Close existing connection if any
    if (this.marketWs) this.marketWs.terminate();

    this.activeTokenIds = tokenIds;
    this.onMarketMessage = onMessage;
    this.connectMarket();
  }

  /**
   * Connect to market WebSocket channel
   *
   * Establishes connection and subscribes to token IDs.
   * Automatically reconnects on disconnect.
   */
  private connectMarket() {
    console.log(`🔌 [WS-MARKET] Connecting...`);
    this.marketWs = new WebSocket(
      "wss://ws-subscriptions-clob.polymarket.com/ws/market"
    );

    /**
     * Connection opened
     * Send subscription message with token IDs
     */
    this.marketWs.on("open", () => {
      console.log("✅ [WS-MARKET] Connected.");
      if (this.activeTokenIds.length > 0) {
        const payload = {
          assets_ids: this.activeTokenIds,
          token_ids: this.activeTokenIds,
          type: "market",
        };
        this.marketWs?.send(JSON.stringify(payload));
      }
    });

    /**
     * Message received
     * Parse JSON and forward to callback
     */
    this.marketWs.on("message", (data: any) => {
      try {
        const msg = JSON.parse(data.toString());
        if (this.onMarketMessage) this.onMarketMessage(msg);
      } catch (e) {
        // Ignore malformed JSON
      }
    });

    /**
     * Connection closed
     * Automatically reconnect after 5 seconds
     */
    this.marketWs.on("close", () =>
      setTimeout(() => this.connectMarket(), 5000)
    );

    /**
     * Connection error
     * Log error but don't crash
     */
    this.marketWs.on("error", (e) =>
      console.error("[WS-MARKET] Error:", e.message)
    );
  }

  // ============================================================================
  // USER CHANNEL (Private Trade Data - Authentication Required)
  // ============================================================================

  /**
   * Subscribe to user channel for private trade data
   *
   * This channel provides:
   * - Your order fills
   * - Your trade events
   * - Order status updates
   *
   * Requires authentication via HMAC-SHA256 signature.
   *
   * @param {Object} creds - API credentials (key, secret, passphrase)
   * @param {Function} onMessage - Callback function for received messages
   */
  async subscribeUser(
    creds: { key: string; secret: string; passphrase: string },
    onMessage: (data: any) => void
  ) {
    // Close existing connection if any
    if (this.userWs) this.userWs.terminate();

    /**
     * Store credentials for reconnection
     * If connection drops, we need these to reconnect
     */
    this.userCreds = creds;
    this.onUserMessage = onMessage;
    this.connectUser(creds);
  }

  /**
   * Connect to user WebSocket channel with authentication
   *
   * Uses HMAC-SHA256 signature for secure authentication.
   * Implements exponential backoff for reconnection.
   *
   * @param {Object} creds - API credentials
   */
  private connectUser(creds: {
    key: string;
    secret: string;
    passphrase: string;
  }) {
    console.log(`🔐 [WS-USER] Generating Auth Signature...`);

    /**
     * Generate HMAC-SHA256 signature for authentication
     *
     * Format: HMAC-SHA256(secret, timestamp + "GET/ws/user")
     *
     * This is Polymarket's authentication scheme:
     * - Timestamp prevents replay attacks
     * - HMAC ensures signature can't be forged
     * - Base64 encoded for transmission
     */
    const timestamp = Math.floor(Date.now() / 1000);
    const message = `${timestamp}GET/ws/user`;
    const signature = createHmac("sha256", creds.secret)
      .update(message)
      .digest("base64");

    /**
     * Authentication headers
     * These are sent in the WebSocket handshake
     */
    const headers = {
      "POLY-API-KEY": creds.key,
      "POLY-PASSPHRASE": creds.passphrase,
      "POLY-TIMESTAMP": timestamp.toString(),
      "POLY-SIGNATURE": signature,
    };

    console.log(`🔐 [WS-USER] Connecting Authenticated Stream...`);
    this.userWs = new WebSocket(
      "wss://ws-subscriptions-clob.polymarket.com/ws/user",
      { headers }
    );

    /**
     * Connection opened successfully
     * Reset reconnect counter since we connected
     */
    this.userWs.on("open", () => {
      console.log("✅ [WS-USER] Connected & Authenticated.");
      this.reconnectAttempts = 0;
    });

    /**
     * Message received
     * Filter for trade/order events and forward to callback
     */
    this.userWs.on("message", (data: any) => {
      try {
        const msg = JSON.parse(data.toString());

        /**
         * Filter for relevant events
         * - "trade": Order fill events
         * - "order": Order status updates
         * - "error": Server errors
         */
        if (msg.event_type === "trade" || msg.event_type === "order") {
          if (this.onUserMessage) this.onUserMessage(msg);
        }
        if (msg.event_type === "error") {
          console.error(`❌ [WS-USER] Server Error:`, msg.message);
        }
      } catch (e) {
        // Ignore malformed JSON
      }
    });

    /**
     * Connection closed
     *
     * Implements exponential backoff reconnection:
     * - Attempt 1: Wait 5 seconds
     * - Attempt 2: Wait 10 seconds
     * - Attempt 3: Wait 15 seconds
     * - ... up to 30 seconds max
     *
     * Stops after MAX_RECONNECT_ATTEMPTS to prevent infinite loops
     */
    this.userWs.on("close", () => {
      if (this.reconnectAttempts >= this.MAX_RECONNECT_ATTEMPTS) {
        console.error(
          `❌ [WS-USER] Max reconnection attempts (${this.MAX_RECONNECT_ATTEMPTS}) reached. Stopping reconnection.`
        );
        console.error(`   Check your API credentials and network connection.`);
        return;
      }

      this.reconnectAttempts++;
      const delay = Math.min(5000 * this.reconnectAttempts, 30000); // Exponential backoff, max 30s
      console.warn(
        `⚠️ [WS-USER] Disconnected. Reconnecting... (Attempt ${this.reconnectAttempts}/${this.MAX_RECONNECT_ATTEMPTS})`
      );

      // Use stored credentials for reconnection
      if (this.userCreds) {
        setTimeout(() => this.connectUser(this.userCreds!), delay);
      }
    });

    /**
     * Connection error
     *
     * Special handling for authentication errors:
     * - If auth fails, stop reconnecting (credentials are wrong)
     * - Otherwise, let reconnection logic handle it
     */
    this.userWs.on("error", (e) => {
      console.error(`[WS-USER] Error: ${e.message}`);
      if (
        e.message.includes("auth") ||
        e.message.includes("401") ||
        e.message.includes("403")
      ) {
        this.reconnectAttempts = this.MAX_RECONNECT_ATTEMPTS; // Stop retrying on auth errors
        console.error(
          `❌ [WS-USER] Authentication error detected. Stopping reconnection.`
        );
      }
    });
  }

  /**
   * Close all WebSocket connections
   *
   * Called during graceful shutdown to clean up connections.
   */
  close() {
    this.marketWs?.terminate();
    this.userWs?.terminate();
  }
}
