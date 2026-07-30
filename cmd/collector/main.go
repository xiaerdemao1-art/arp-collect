package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xiaerdemao1-art/arp-collect/internal/collector"
	"github.com/xiaerdemao1-art/arp-collect/internal/config"
	"github.com/xiaerdemao1-art/arp-collect/internal/model"
	"github.com/xiaerdemao1-art/arp-collect/internal/netbox"
	"github.com/xiaerdemao1-art/arp-collect/internal/snmp"
	"github.com/xiaerdemao1-art/arp-collect/internal/storage"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	configPath   = flag.String("config", "", "配置文件路径")
	proxyName    = flag.String("proxy", "", "当前采集节点名称（覆盖配置）")
	serverURL    = flag.String("server", "", "Server API 地址（分布式模式）")
	singleRun    = flag.Bool("once", false, "单次运行模式")
	singleDevice = flag.String("device", "", "单设备采集（IP 或设备名）")
	runMode      = flag.String("mode", "", "运行模式: standalone/collector")
	showVersion  = flag.Bool("version", false, "显示版本信息")
)

// 版本信息（由 Makefile 通过 -ldflags -X 注入）
var (
	version   = "0.0.1"
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("arp_collector version %s (built %s, commit %s)\n", version, buildTime, gitCommit)
		return
	}

	// 初始化日志
	logger := initLogger()
	defer logger.Sync()

	logger.Info("ARP Collector 启动", zap.String("version", version))

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal("加载配置失败", zap.Error(err))
	}

	// 命令行参数覆盖
	if *proxyName != "" {
		cfg.Collector.ProxyName = *proxyName
	}
	if *serverURL != "" {
		cfg.Collector.ServerURL = *serverURL
	}
	if *singleRun {
		cfg.Collector.SingleRun = true
	}
	if *runMode != "" {
		cfg.Server.Mode = *runMode
	}

	// 决定运行模式
	mode := cfg.Server.Mode
	if mode == "" {
		mode = "standalone"
		cfg.Server.Mode = mode
	}

	// 校验配置完整性（缺少必填凭据时直接报错，而不是静默使用默认值）
	if err := cfg.Validate(); err != nil {
		logger.Fatal("配置校验失败", zap.Error(err))
	}

	logger.Info("配置加载完成",
		zap.String("mode", mode),
		zap.String("proxy_name", cfg.Collector.ProxyName),
		zap.Duration("interval", cfg.Collector.Interval))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("收到退出信号", zap.String("signal", sig.String()))
		cancel()
	}()

	switch mode {
	case "collector":
		// 分布式模式：从 Server 获取任务
		runDistributedCollector(ctx, cfg, logger)
	case "standalone":
		// 单机模式：直接连接 NetBox 和 PG
		runStandaloneCollector(ctx, cfg, logger, *singleDevice)
	default:
		logger.Fatal("未知运行模式", zap.String("mode", mode))
	}
}

// runDistributedCollector 分布式采集器模式
func runDistributedCollector(ctx context.Context, cfg *config.Config, logger *zap.Logger) {
	logger.Info("进入分布式 Collector 模式",
		zap.String("server_url", cfg.Collector.ServerURL))

	c := collector.NewRemoteCollector(cfg, logger)
	if err := c.Run(ctx); err != nil {
		logger.Error("Collector 运行失败", zap.Error(err))
	}
}

// runStandaloneCollector 单机采集器模式
func runStandaloneCollector(ctx context.Context, cfg *config.Config, logger *zap.Logger, singleDevice string) {
	logger.Info("进入单机模式")

	// 初始化组件
	netboxClient := netbox.NewClient(cfg, logger)
	snmpCollector := snmp.NewCollector(&cfg.SNMP, logger)

	pgStore, err := storage.NewPostgresStore(&cfg.Postgres, logger)
	if err != nil {
		logger.Fatal("连接 PostgreSQL 失败", zap.Error(err))
	}
	defer pgStore.Close()

	// 获取设备列表
	if _, err := netboxClient.FetchDevices(ctx, cfg.NetBox.Roles); err != nil {
		logger.Fatal("获取设备列表失败", zap.Error(err))
	}

	// 单设备模式
	if singleDevice != "" {
		runSingleDeviceMode(ctx, logger, netboxClient, snmpCollector, pgStore, singleDevice)
		return
	}

	// 获取本节点设备
	devices := netboxClient.GetDevicesForProxy(cfg.Collector.ProxyName)
	if len(devices) == 0 {
		logger.Warn("当前节点没有分配的设备")
		return
	}

	// 单次运行
	if cfg.Collector.SingleRun {
		runOnceMode(ctx, logger, cfg, snmpCollector, pgStore, devices)
		return
	}

	// 守护进程模式
	runDaemonMode(ctx, logger, cfg, netboxClient, snmpCollector, pgStore)
}

// runSingleDeviceMode 单设备采集模式
func runSingleDeviceMode(ctx context.Context, logger *zap.Logger,
	netboxClient *netbox.Client, snmpCollector *snmp.Collector,
	store *storage.PostgresStore, target string) {

	logger.Info("单设备采集模式", zap.String("target", target))

	devices := netboxClient.GetCachedDevices()
	var targetDevice *model.Device
	for i := range devices {
		if devices[i].PrimaryIP == target || devices[i].Name == target {
			targetDevice = &devices[i]
			break
		}
	}

	if targetDevice == nil {
		logger.Fatal("未找到目标设备", zap.String("target", target))
	}

	result := snmpCollector.CollectDevice(*targetDevice)

	logger.Info("采集完成",
		zap.String("device", result.DeviceName),
		zap.String("status", result.Status),
		zap.Int("arp_count", result.ARPCount),
		zap.Int64("duration_ms", result.DurationMs))

	if result.Error != "" {
		logger.Error("采集错误", zap.String("error", result.Error))
		return
	}

	// 打印 JSON 输出
	jsonStr, _ := json.MarshalIndent(result.Entries, "", "  ")
	fmt.Println(string(jsonStr))

	// 保存到数据库
	if err := store.SaveResult(ctx, result); err != nil {
		logger.Error("保存结果失败", zap.Error(err))
	} else {
		logger.Info("结果已保存到数据库")
	}
}

// runOnceMode 单次运行模式
func runOnceMode(ctx context.Context, logger *zap.Logger, cfg *config.Config,
	snmpCollector *snmp.Collector, store *storage.PostgresStore, devices []model.Device) {

	logger.Info("开始单次采集",
		zap.String("proxy", cfg.Collector.ProxyName),
		zap.Int("device_count", len(devices)))

	results := snmpCollector.CollectDevices(devices)

	// 统计
	successCount := 0
	totalARP := 0
	for _, r := range results {
		if r.Status == "success" {
			successCount++
			totalARP += r.ARPCount
		}
	}

	logger.Info("采集完成",
		zap.Int("success", successCount),
		zap.Int("fail", len(results)-successCount),
		zap.Int("total_arp", totalARP))

	// 保存结果
	if err := store.SaveResults(ctx, results); err != nil {
		logger.Error("保存结果失败", zap.Error(err))
	}
}

// slowDeviceTracker 记录慢速设备的上次采集时间（与分布式模式共用实现）
var slowDeviceTracker *collector.SlowDeviceTracker

// runDaemonMode 守护进程模式
func runDaemonMode(ctx context.Context, logger *zap.Logger, cfg *config.Config,
	netboxClient *netbox.Client, snmpCollector *snmp.Collector, store *storage.PostgresStore) {

	logger.Info("进入守护进程模式",
		zap.Duration("collect_interval", cfg.Collector.Interval),
		zap.Duration("device_refresh", cfg.Collector.DeviceRefresh))

	slowDeviceTracker = collector.NewSlowDeviceTracker(logger)

	// 构建慢速设备 IP 映射
	slowDeviceMap := make(map[string]int64)
	for _, sd := range cfg.Collector.SlowDevices {
		slowDeviceMap[sd.IP] = int64(sd.Interval.Seconds())
		logger.Info("注册慢速设备",
			zap.String("ip", sd.IP),
			zap.String("name", sd.Name),
			zap.Duration("interval", sd.Interval))
	}

	collectTicker := time.NewTicker(cfg.Collector.Interval)
	defer collectTicker.Stop()

	refreshTicker := time.NewTicker(cfg.Collector.DeviceRefresh)
	defer refreshTicker.Stop()

	// 立即执行一次
	doStandaloneCollect(ctx, logger, cfg, netboxClient, snmpCollector, store, slowDeviceMap)

	for {
		select {
		case <-collectTicker.C:
			doStandaloneCollect(ctx, logger, cfg, netboxClient, snmpCollector, store, slowDeviceMap)

		case <-refreshTicker.C:
			logger.Info("刷新设备列表")
			if _, err := netboxClient.FetchDevices(ctx, cfg.NetBox.Roles); err != nil {
				logger.Error("刷新设备列表失败", zap.Error(err))
			}

		case <-ctx.Done():
			return
		}
	}
}

// doStandaloneCollect 执行一次单机采集
func doStandaloneCollect(ctx context.Context, logger *zap.Logger, cfg *config.Config,
	netboxClient *netbox.Client, snmpCollector *snmp.Collector, store *storage.PostgresStore,
	slowDeviceMap map[string]int64) {

	start := time.Now()
	devices := netboxClient.GetDevicesForProxy(cfg.Collector.ProxyName)

	// 为设备填充自定义采集间隔
	for i := range devices {
		if interval, ok := slowDeviceMap[devices[i].PrimaryIP]; ok {
			devices[i].CollectInterval = interval
		}
	}

	// 过滤慢速设备，只保留需要采集的设备
	devicesToCollect := slowDeviceTracker.Filter(devices)

	logger.Info("开始采集周期",
		zap.String("proxy", cfg.Collector.ProxyName),
		zap.Int("total_devices", len(devices)),
		zap.Int("devices_to_collect", len(devicesToCollect)),
		zap.Int("slow_devices_skipped", len(devices)-len(devicesToCollect)))

	if len(devicesToCollect) == 0 {
		logger.Info("本次周期没有需要采集的设备")
		return
	}

	results := snmpCollector.CollectDevices(devicesToCollect)

	// 更新慢速设备的采集时间记录
	slowDeviceTracker.MarkCollected(devicesToCollect)

	if err := store.SaveResults(ctx, results); err != nil {
		logger.Error("保存结果失败", zap.Error(err))
	}

	// 统计
	successCount := 0
	totalARP := 0
	for _, r := range results {
		if r.Status == "success" {
			successCount++
			totalARP += r.ARPCount
		}
	}

	logger.Info("采集周期完成",
		zap.Int("success", successCount),
		zap.Int("fail", len(results)-successCount),
		zap.Int("total_arp", totalARP),
		zap.Duration("duration", time.Since(start)))
}

func initLogger() *zap.Logger {
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder

	logger, err := cfg.Build()
	if err != nil {
		panic(err)
	}
	return logger
}
