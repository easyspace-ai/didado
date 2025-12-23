package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"polymarketbot/internal/app"
	"polymarketbot/internal/config"
)

func main() {
	var (
		strategy = flag.String("strategy", "", "策略: sniperladder / simpletrap / gridhedge（空=默认sniperladder）")
	)
	flag.Parse()

	cfg, err := config.Load(config.LoadOptions{
		StrategyOverride: *strategy,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "配置加载失败:", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := app.Run(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "运行失败:", err)
		os.Exit(1)
	}
}
