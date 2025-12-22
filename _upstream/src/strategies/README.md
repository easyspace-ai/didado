# Strategies Guide

This guide covers everything about creating, customizing, and understanding trading strategies in the bot.

## 📋 Table of Contents

- [How to Add a New Strategy](#how-to-add-a-new-strategy)
- [How to Play Around with the Bot](#how-to-play-around-with-the-bot)
- [Pre-Configured Strategy: SniperLadder](#pre-configured-strategy-sniperladder)
- [Customizing SniperLadder Configuration](#customizing-sniperladder-configuration)
- [Detailed Scenarios](#detailed-scenarios)

## How to Add a New Strategy

1. **Create a new strategy file** in `src/strategies/`:

   ```typescript
   // src/strategies/MyStrategy.ts
   import { PolymarketService } from "../services/PolymarketService";
   import { WebSocketService } from "../services/WebSocketService";
   import { IExecutor } from "../execution/IExecutor";
   import { MarketMath } from "../core/MarketMath";

   export class MyStrategy {
     private api: PolymarketService;
     private ws: WebSocketService;
     private executor: IExecutor;

     constructor(
       api: PolymarketService,
       ws: WebSocketService,
       executor: IExecutor
     ) {
       this.api = api;
       this.ws = ws;
       this.executor = executor;
     }

     async start() {
       // Your strategy logic here
     }

     async stop() {
       // Cleanup logic here
     }
   }
   ```

2. **Use the bot's services**:

   ```typescript
   // Get market tokens
   const tokens = await this.api.getMarketTokenIds("btc-updown-15m-1765895400");
   const tokenIdYes = tokens[0];
   const tokenIdNo = tokens[1];

   // Place an order
   const orderId = await this.executor.placeOrder({
     tokenId: tokenIdYes,
     side: "BUY",
     price: 0.45,
     size: 5,
     type: "GTC",
   });

   // Cancel an order
   await this.executor.cancelOrder(orderId);

   // Get positions
   const positions = await this.executor.getPositions(tokenIdYes, tokenIdNo);
   console.log(`YES: ${positions.yes}, NO: ${positions.no}`);

   // Get balance
   const balance = await this.executor.getBalance();
   console.log(`Balance: $${balance}`);
   ```

3. **Subscribe to WebSocket feeds**:

   ```typescript
   // Market feed (price updates)
   await this.ws.subscribeMarket([tokenIdYes, tokenIdNo], (data: any) => {
     // Handle price updates
     if (data.event_type === "price_change") {
       const price = parseFloat(data.price);
       // Your logic here
     }
   });

   // User feed (your order fills)
   await this.ws.subscribeUser(
     { key: "...", secret: "...", passphrase: "..." },
     (data: any) => {
       // Handle your order fills
       if (data.event_type === "trade") {
         const side = data.side; // "BUY" or "SELL"
         const size = parseFloat(data.size);
         const price = parseFloat(data.price);
         // Your logic here
       }
     }
   );
   ```

4. **Use market utilities**:

   ```typescript
   // Get current market
   const current = MarketMath.getTargetMarketSlug();
   console.log(`Current market: ${current.slug}`);

   // Get next market
   const next = MarketMath.getNextMarketSlug();
   console.log(
     `Next market: ${next.slug}, starts at ${new Date(next.startTimeMs)}`
   );

   // Calculate wait time
   const msUntilStart = MarketMath.getMsUntil(next.startTimeMs);
   console.log(`Wait ${msUntilStart / 1000} seconds`);
   ```

5. **Register your strategy** in `src/index.ts`:

   ```typescript
   import { MyStrategy } from "./strategies/MyStrategy";

   // ... (after executor setup)
   const myBot = new MyStrategy(polyService, wsServiceSniper, executorSniper);
   await myBot.start();
   ```

## How to Play Around with the Bot

### Placing Orders

```typescript
// Buy 5 YES shares at $0.45
const orderId = await executor.placeOrder({
  tokenId: tokenIdYes,
  side: "BUY",
  price: 0.45,
  size: 5,
  type: "GTC", // Good Till Cancel
});

// Sell 3 NO shares at $0.55
const sellOrderId = await executor.placeOrder({
  tokenId: tokenIdNo,
  side: "SELL",
  price: 0.55,
  size: 3,
  type: "GTC",
});
```

### Canceling Orders

```typescript
// Cancel a specific order
await executor.cancelOrder(orderId);

// Cancel all orders
await executor.cancelAll();
```

### Checking Order Status

```typescript
const status = await executor.getOrderStatus(orderId);
console.log(`Matched: ${status.matched} shares`);
console.log(`Cancelled: ${status.cancelled}`);
console.log(`Avg Fill Price: $${status.avgFillPrice}`);
```

### Getting Positions

```typescript
const positions = await executor.getPositions(tokenIdYes, tokenIdNo);
console.log(`YES shares: ${positions.yes}`);
console.log(`NO shares: ${positions.no}`);
```

### Getting Balance

```typescript
const balance = await executor.getBalance();
console.log(`Available USDC: $${balance}`);
```

### Listening to Price Updates

```typescript
await ws.subscribeMarket([tokenIdYes, tokenIdNo], (data: any) => {
  if (data.asset_id === tokenIdYes && data.price) {
    const price = parseFloat(data.price);
    console.log(`YES price: $${price}`);
  }
  if (data.asset_id === tokenIdNo && data.price) {
    const price = parseFloat(data.price);
    console.log(`NO price: $${price}`);
  }
});
```

### Listening to Your Fills

```typescript
await ws.subscribeUser(creds, (data: any) => {
  if (data.event_type === "trade") {
    console.log(`Fill: ${data.side} ${data.size} @ $${data.price}`);
    // Your logic here
  }
});
```

## Pre-Configured Strategy: SniperLadder

The bot comes with a pre-configured **SniperLadder** strategy optimized for Bitcoin 15-minute markets.

### How It Works

The SniperLadder strategy is an event-driven, dynamic hedging system:

**Phase 1: Initial Setup**

- Places trap orders at **$0.45** on both YES and NO sides (5 shares each)
- Waits 1 minute before market starts to place orders

**Phase 2: Trigger Phase**
When one trap fills (~5 shares):

- Immediately cancels the opposite trap
- Places ladder orders at $0.37 and $0.30 on the same side
- Places initial hedge order at $0.52 on the opposite side

**Phase 3: Dynamic Hedging**
As positions change:

- Recalculates position imbalance (aggressor vs defender)
- Places multiple hedge orders at different price levels
- Uses profit margin calculations (targeting 2.0% profit)
- Only recalculates when YOUR orders fill (event-driven)

**Phase 4: Risk Management**

- **Panic Mode 1 (1.5 min before end)**: Partial sell of unbalanced positions
- **Panic Mode 2 (1 min before end)**: Full sell of losing naked positions
- **Break-Even Guard**: Caps hedge prices to prevent losses
- **Position Syncing**: Periodically verifies on-chain positions

**Phase 5: Exit Conditions**

- **Win**: When YES and NO positions are balanced → Exit with profit
- **Timeout**: Panic exits near market end if unbalanced
- **Market End**: Automatic cycle end and restart

### Strategy Parameters

- **Initial Trap Price**: $0.45
- **Initial Trap Size**: 5 shares per side
- **Ladder Levels**: $0.37 (5 shares), $0.30 (5 shares)
- **Initial Hedge Price**: $0.52
- **Profit Target**: 2.0% on total transaction
- **Panic Mode Timing**: 1.5 min (partial), 1 min (full) before market end

## Customizing SniperLadder Configuration

All strategy parameters can be easily customized by editing `src/strategies/SniperLadder.ts`. Here's a complete guide:

**1. Open the strategy file**:

```bash
src/strategies/SniperLadder.ts
```

**2. Find the configuration section** (around line 17-35):

```typescript
// --- CONFIG ---
private INITIAL_TRAP_PRICE = 0.45;        // Trap order price ($0.45)
private INITIAL_TRAP_SIZE = 5;            // Shares per trap order
private LEG_THRESHOLD = 4.95;             // Minimum shares to trigger ladder

private LADDER_LEVELS = [
  { price: 0.37, size: 5 },              // First ladder: $0.37, 5 shares
  { price: 0.3, size: 5 },               // Second ladder: $0.30, 5 shares
];

private INITIAL_PROFIT_TARGET_PERCENT = 0.025;  // 2.5% profit target
private PIVOT_TIME_BEFORE_END_MS = 180000;      // 3 minutes before market end
private MAX_SPREAD = 0.4;                       // Max spread to place ladders
private ONE_CYCLE_ONLY = true;                  // Exit after one cycle

// Panic Mode Timing
private PARTIAL_SELL_TIME_MS = 90000;    // 1.5 min before end (partial sell)
private FULL_SELL_TIME_MS = 60000;       // 1 min before end (full sell)
```

**3. Common Customizations**:

**Change Order Sizes** (e.g., from 5 to 10 shares):

```typescript
private INITIAL_TRAP_SIZE = 10;
private LEG_THRESHOLD = 9.95;  // Should be slightly less than trap size

private LADDER_LEVELS = [
  { price: 0.37, size: 10 },
  { price: 0.3, size: 10 },
];
```

**Change Trap Price** (e.g., from $0.45 to $0.40):

```typescript
private INITIAL_TRAP_PRICE = 0.40;
```

**Change Ladder Prices** (e.g., deeper ladders):

```typescript
private LADDER_LEVELS = [
  { price: 0.35, size: 5 },  // First ladder at $0.35
  { price: 0.28, size: 5 },   // Second ladder at $0.28
];
```

**Change Profit Target** (e.g., from 2.5% to 3%):

```typescript
private INITIAL_PROFIT_TARGET_PERCENT = 0.03;  // 3%
```

**Disable Single Cycle Mode** (run continuously):

```typescript
private ONE_CYCLE_ONLY = false;  // Bot will continue to next market
```

**Adjust Panic Mode Timing** (e.g., earlier exits):

```typescript
private PARTIAL_SELL_TIME_MS = 120000;  // 2 min before end (was 1.5 min)
private FULL_SELL_TIME_MS = 90000;     // 1.5 min before end (was 1 min)
```

**4. Save and restart** the bot for changes to take effect.

**Important Notes**:

- **Order Sizes**: Larger sizes = more capital needed, higher risk/reward
- **Prices**: Make sure prices are reasonable (not too far from current market)
- **Timing**: Panic mode times are in milliseconds (60000 = 1 minute)
- **Profit Target**: Lower = more aggressive, higher = more conservative
- **Testing**: Always test changes in paper trading mode first!

## Detailed Scenarios

Here are 5 common scenarios that demonstrate how SniperLadder performs:

### Scenario 1: Perfect Win (Ideal Outcome)

- Bot places traps: 5 YES @ $0.45, 5 NO @ $0.45
- YES trap fills → Bot cancels NO trap immediately
- Bot places ladder orders: 5 YES @ $0.37, 5 YES @ $0.30
- Bot places initial hedge: 5 NO @ $0.52
- Hedge fills → Position balanced: 5 YES / 5 NO
- **Result:** ✅ **WIN** - Profit: $0.15 (3.1%)

### Scenario 2: Ladder Fills, Then Hedge (Averaging Down)

- YES trap fills (5 shares @ $0.45)
- Bot places ladders: 5 YES @ $0.37, 5 YES @ $0.30
- First ladder fills (5 @ $0.37) → Now have 10 YES
- Bot recalculates: Need 10 NO to balance
- Bot places hedge ladder: 5 NO @ $0.52, 5 NO @ $0.53
- Both hedges fill → Position balanced: 10 YES / 10 NO
- **Result:** ✅ **WIN** - Profit: $0.65 (7.0%)

### Scenario 3: Multiple Ladder Fills (Deep Accumulation)

- YES trap fills (5 shares @ $0.45)
- Bot places ladders: 5 YES @ $0.37, 5 YES @ $0.30
- Both ladders fill → Now have 15 YES total
- Bot recalculates: Need 15 NO to balance
- Bot places hedge ladder: 5 NO @ $0.52, 5 NO @ $0.53, 5 NO @ $0.54
- All hedges fill → Position balanced: 15 YES / 15 NO
- **Result:** ✅ **WIN** - Profit: $1.45 (10.7%)

### Scenario 4: Partial Hedge Fill (Unbalanced Exit)

- YES trap fills (5 shares @ $0.45)
- Ladder fills (5 YES @ $0.37) → Total: 10 YES
- Bot places hedge: 10 NO @ $0.52
- Only 5 NO hedge fills → Position: 10 YES / 5 NO (unbalanced)
- Market approaches end (1.5 minutes remaining)
- **Panic Mode 1**: Bot sells 5 excess YES to balance
- Final position: 5 YES / 5 NO
- **Result:** ⚠️ **PARTIAL WIN** - Loss: $1.70 (but avoided larger loss)

### Scenario 5: Naked Position at Market End (Panic Exit)

- YES trap fills (5 shares @ $0.45)
- Ladder fills (5 YES @ $0.37) → Total: 10 YES
- Hedge orders placed but NONE fill
- Market end approaches (1 minute remaining)
- **Panic Mode 2**: Bot checks price history
- If losing side: Bot sells all 10 YES at market price
- If winning side: Bot holds position
- **Result:** ⚠️ **RISK MITIGATION** - Prevents large losses
