package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/joho/godotenv"

	"polymarketbot-go/src/clob"
	"polymarketbot-go/src/core"
	"polymarketbot-go/src/execution"
	"polymarketbot-go/src/services"
	"polymarketbot-go/src/strategies"
)

const (
	CHAIN_ID = 137 // Polygon Mainnet
	RPC_URL  = "https://polygon-rpc.com"
)

func main() {
	fmt.Println("\n🚀 POLYMARKET SNIPER BOT INITIALIZING...")

	// 加载环境变量
	if err := godotenv.Load(); err != nil {
		fmt.Println("⚠️ No .env file found, using environment variables")
	}

	// 步骤 1: 设置钱包和区块链提供者
	provider, err := ethclient.Dial(RPC_URL)
	if err != nil {
		fmt.Printf("❌ Failed to connect to Polygon RPC: %v\n", err)
		os.Exit(1)
	}

	privateKeyStr := os.Getenv("PRIVATE_KEY")
	if privateKeyStr == "" {
		fmt.Println("⚠️ NO PRIVATE KEY FOUND IN .env! Using random wallet (Paper Trading only).")
		// 生成随机钱包用于模拟交易
		privateKey, _ := crypto.GenerateKey()
		privateKeyStr = fmt.Sprintf("%x", crypto.FromECDSA(privateKey))
	}

	privateKey, err := crypto.HexToECDSA(privateKeyStr)
	if err != nil {
		fmt.Printf("❌ Invalid private key: %v\n", err)
		os.Exit(1)
	}

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		fmt.Println("❌ Failed to get public key")
		os.Exit(1)
	}

	address := crypto.PubkeyToAddress(*publicKeyECDSA)
	fmt.Printf("🔑 Wallet Address: %s\n", address.Hex())

	// 检查余额
	fmt.Println("💰 Checking wallet balance...")
	balance, err := provider.BalanceAt(context.Background(), address, nil)
	if err != nil {
		fmt.Printf("⚠️ Could not fetch native balance: %v\n", err)
		fmt.Println("   Continuing anyway...")
	} else {
		balanceEth := new(big.Float).Quo(new(big.Float).SetInt(balance), big.NewFloat(1e18))
		fmt.Printf("💰 Native Balance: %.4f MATIC\n", balanceEth)
	}

	// 步骤 2: 设置 Polymarket CLOB 客户端
	proxyAddressStr := os.Getenv("POLYMARKET_PROXY_ADDRESS")
	proxyAddress := address
	if proxyAddressStr != "" {
		proxyAddress = common.HexToAddress(proxyAddressStr)
	}

	isProxy := proxyAddressStr != ""
	signatureType := 0
	if isProxy {
		signatureType = 1
	}

	fmt.Println("🔐 Authenticating with CLOB...")
	if isProxy {
		fmt.Printf("   Signature Type: 1 (Proxy/Web)\n")
		fmt.Printf("   Funder Address: %s\n", proxyAddress.Hex())
		fmt.Printf("   Signer Address: %s\n", address.Hex())
	} else {
		fmt.Printf("   Signature Type: 0 (EOA/Direct)\n")
		fmt.Printf("   Funder Address: %s\n", proxyAddress.Hex())
	}

	fmt.Println("   ⏳ Deriving API key (this may take 10-30 seconds)...")

	// 创建签名器
	chainID := big.NewInt(CHAIN_ID)
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		fmt.Printf("❌ Failed to create transactor: %v\n", err)
		os.Exit(1)
	}

	// 创建初始 CLOB 客户端 (无凭证)
	clobClient := clob.NewClobClientWithPrivateKey(
		"https://clob.polymarket.com",
		CHAIN_ID,
		privateKey,
		nil, // creds (将通过 deriveApiKey 派生)
		signatureType,
		proxyAddress,
	)

	// 尝试从环境变量读取凭证
	apiKey := os.Getenv("POLYMARKET_API_KEY")
	apiSecret := os.Getenv("POLYMARKET_API_SECRET")
	apiPassphrase := os.Getenv("POLYMARKET_PASSPHRASE")

	var creds *clob.Credentials
	var wsCreds *services.UserCredentials

	if apiKey != "" && apiSecret != "" {
		// 使用环境变量中的凭证
		creds = &clob.Credentials{
			Key:        apiKey,
			Secret:     apiSecret,
			Passphrase: apiPassphrase,
		}
		clobClient.SetCredentials(creds)
		fmt.Println("✅ Using API credentials from environment variables.")
	} else {
		// 派生 API 密钥
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		done := make(chan error, 1)
		go func() {
			derivedCreds, err := clobClient.DeriveApiKey()
			if err != nil {
				done <- err
				return
			}
			creds = derivedCreds
			done <- nil
		}()

		select {
		case err := <-done:
			if err != nil {
				fmt.Printf("❌ Authentication Failed: %v\n", err)
				fmt.Println("   Make sure you've enabled trading on polymarket.com with this wallet.")
				os.Exit(1)
			}
			fmt.Println("✅ API Key Derived Successfully.")
		case <-ctx.Done():
			fmt.Println("❌ Authentication timeout after 60 seconds")
			os.Exit(1)
		}

		// 重新创建客户端，使用派生凭证
		clobClient = clob.NewClobClientWithPrivateKey(
			"https://clob.polymarket.com",
			CHAIN_ID,
			privateKey,
			creds,
			signatureType,
			proxyAddress,
		)
		fmt.Println("   ✅ Client re-initialized with derived credentials.")
		fmt.Printf("   (Debug) Signer Address: %s\n", address.Hex())
		fmt.Printf("   (Debug) Configured Funder: %s\n", proxyAddress.Hex())
		fmt.Printf("   (Debug) Signature Type: %d\n", signatureType)
	}

	// 转换为服务层使用的凭证格式
	wsCreds = &services.UserCredentials{
		Key:        creds.Key,
		Secret:     creds.Secret,
		Passphrase: creds.Passphrase,
	}

	// 步骤 3: 初始化服务
	polyService := services.NewPolymarketService()
	wsService := services.NewWebSocketService(clobClient)

	// 步骤 4: 选择执行器 (Paper vs Live)
	isLiveTrading := os.Getenv("LIVE_TRADING") == "true"

	var executor execution.IExecutor

	if isLiveTrading {
		fmt.Println("🚨 MODE: LIVE TRADING (REAL FUNDS) 🚨")

		// 使用包装器包装 ClobClient (clobClient 实现了 ClobClientWrapperInterface)
		clobWrapper := execution.NewClobClientWrapper(clobClient)
		executor, err = execution.NewLiveExecutorWithClient(clobWrapper, auth, proxyAddress)
		if err != nil {
			fmt.Printf("❌ Failed to create LiveExecutor: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Println("📝 MODE: PAPER TRADING (SIMULATION)")
		executor = execution.NewPaperExecutor(1000)
	}

	// 步骤 5: 初始化交易策略
	marketMath := &core.MarketMath{}

	sniperBot := strategies.NewSniperLadder(
		polyService,
		wsService,
		executor,
		wsCreds,
		auth, // signer
		proxyAddress.Hex(),
		marketMath,
	)

	// 步骤 6: 设置优雅关闭处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		fmt.Printf("\n🛑 Received %v. Shutting down gracefully...\n", sig)

		if err := sniperBot.Stop(); err != nil {
			fmt.Printf("❌ Error during shutdown: %v\n", err)
			// 回退：尝试直接取消订单
			executor.CancelAll()
			wsService.Close()
		} else {
			fmt.Println("✅ Bot stopped gracefully.")
		}

		os.Exit(0)
	}()

	// 步骤 7: 启动交易策略
	fmt.Println("\n🎯 RUNNING SNIPER LADDER STRATEGY")
	fmt.Println("   🎯 SniperLadder (Trap @ $0.45 → Ladder → Dynamic Hedge)\n")

	if err := sniperBot.Start(); err != nil {
		fmt.Printf("❌ FATAL ERROR: %v\n", err)
		os.Exit(1)
	}

	// 保持进程运行
	select {}
}
