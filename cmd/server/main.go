package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/xiaerdemao1-art/arp-collect/internal/config"
	"github.com/xiaerdemao1-art/arp-collect/internal/server"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	configPath  = flag.String("config", "", "配置文件路径")
	showVersion = flag.Bool("version", false, "显示版本信息")
)

// 版本信息（由 Makefile 通过 -ldflags -X 注入）
var (
	version   = "0.0.3"
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("arp_server version %s (built %s, commit %s)\n", version, buildTime, gitCommit)
		return
	}

	// 初始化日志
	logger := initLogger()
	defer logger.Sync()

	logger.Info("ARP Server 启动", zap.String("version", version))

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal("加载配置失败", zap.Error(err))
	}

	// 强制设置为 server 模式
	cfg.Server.Mode = "server"

	// 校验配置完整性（缺少必填凭据时直接报错，而不是静默使用默认值）
	if err := cfg.Validate(); err != nil {
		logger.Fatal("配置校验失败", zap.Error(err))
	}

	logger.Info("配置加载完成",
		zap.String("listen", cfg.Server.Listen),
		zap.Bool("kafka_enabled", cfg.Kafka.Enabled))

	// 创建 Server
	srv, err := server.NewServer(cfg, logger)
	if err != nil {
		logger.Fatal("创建 Server 失败", zap.Error(err))
	}
	defer srv.Close()

	// 信号处理
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		logger.Info("收到退出信号", zap.String("signal", sig.String()))
		cancel()
	}()

	// 启动 Server
	if err := srv.Start(ctx); err != nil {
		logger.Fatal("Server 运行失败", zap.Error(err))
	}

	logger.Info("ARP Server 已退出")
}

func initLogger() *zap.Logger {
	config := zap.NewProductionConfig()
	config.EncoderConfig.TimeKey = "ts"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder

	logger, err := config.Build()
	if err != nil {
		panic(err)
	}
	return logger
}
