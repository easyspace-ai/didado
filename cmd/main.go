package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/polymarketbot-go/pkg/executor"
	"github.com/polymarketbot-go/pkg/services"
	"github.com/polymarketbot-go/pkg/strategy"
)

func main() {
	fmt.Println("\n🚀 POLYMARKET SNIPER BOT 初始化中...")

	// 加载环境变量
	if err := godotenv.Load(); err != nil {
		fmt.Println("⚠️ 未找到.env文件，使用默认配置")
	}

	// 获取配置
	privateKey := os.Getenv("PRIVATE_KEY")
	proxyAddress := os.Getenv("POLYMARKET_PROXY_ADDRESS")
	isLiveTrading := os.Getenv("LIVE_TRADING") == "true"
	rpcURL := os.Getenv("POLYGON_RPC_URL")
	if rpcURL == "" {
		rpcURL = "https://polygon-rpc.com"
	}

	if privateKey == "" {
		fmt.Println("⚠️ 未在.env中找到PRIVATE_KEY！使用随机钱包（仅纸交易）。")
	}

	// 创建执行器
	var exec executor.IExecutor
	var err error

	if isLiveTrading {
		fmt.Println("🚨 模式: 真实交易（真实资金）🚨")
		
		if privateKey == "" {
			fmt.Println("❌ 真实交易需要PRIVATE_KEY！")
			os.Exit(1)
		}
		
		funderAddress := proxyAddress
		if funderAddress == "" {
			// TODO: 从私钥派生地址
			funderAddress = "0x..."
		}
		
		exec, err = executor.NewLiveExecutor(rpcURL, privateKey, funderAddress)
		if err != nil {
			fmt.Printf("❌ 创建真实执行器失败: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Println("📝 模式: 纸交易（模拟）")
		exec = executor.NewPaperExecutor(1000.0)
	}

	// 创建服务
	polyService := services.NewPolymarketService()
	wsService := services.NewWebSocketService()

	// 创建策略
	sniperBot := strategy.NewSniperLadder(polyService, wsService, exec)

	// 设置优雅关闭
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n🛑 收到停止信号。优雅关闭...")
		if err := sniperBot.Stop(); err != nil {
			fmt.Printf("❌ 关闭错误: %v\n", err)
		}
		os.Exit(0)
	}()

	// 启动策略
	fmt.Println("\n🎯 运行 SNIPER LADDER 策略")
	fmt.Println("   🎯 SniperLadder (陷阱 @ $0.45 → 阶梯 → 动态对冲)\n")

	if err := sniperBot.Start(); err != nil {
		fmt.Printf("❌ 启动策略失败: %v\n", err)
		os.Exit(1)
	}

	// 保持运行
	select {}
}
