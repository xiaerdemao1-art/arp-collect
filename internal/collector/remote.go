package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/xiaerdemao1-art/arp-collect/internal/config"
	"github.com/xiaerdemao1-art/arp-collect/internal/model"
	"github.com/xiaerdemao1-art/arp-collect/internal/snmp"

	"go.uber.org/zap"
)

// RemoteCollector 分布式采集器（连接 Server）
type RemoteCollector struct {
	cfg           *config.Config
	logger        *zap.Logger
	snmpCollector *snmp.Collector
	httpClient    *http.Client
	slowDevices   *SlowDeviceTracker
}

// NewRemoteCollector 创建分布式采集器
func NewRemoteCollector(cfg *config.Config, logger *zap.Logger) *RemoteCollector {
	return &RemoteCollector{
		cfg:           cfg,
		logger:        logger,
		snmpCollector: snmp.NewCollector(&cfg.SNMP, logger),
		httpClient: &http.Client{
			Timeout: 5 * time.Minute, // 增加到 5 分钟，支持大批量数据提交
		},
		slowDevices: NewSlowDeviceTracker(logger),
	}
}

// Run 运行采集器
func (c *RemoteCollector) Run(ctx context.Context) error {
	c.logger.Info("分布式 Collector 启动",
		zap.String("proxy_name", c.cfg.Collector.ProxyName),
		zap.String("server_url", c.cfg.Collector.ServerURL),
		zap.Duration("interval", c.cfg.Collector.Interval))

	// 单次运行模式
	if c.cfg.Collector.SingleRun {
		return c.doCollect(ctx)
	}

	// 立即执行一次
	if err := c.doCollect(ctx); err != nil {
		c.logger.Error("首次采集失败", zap.Error(err))
	}

	// 定时采集
	ticker := time.NewTicker(c.cfg.Collector.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := c.doCollect(ctx); err != nil {
				c.logger.Error("采集失败", zap.Error(err))
			}
		case <-ctx.Done():
			c.logger.Info("Collector 退出")
			return nil
		}
	}
}

// doCollect 执行一次采集
func (c *RemoteCollector) doCollect(ctx context.Context) error {
	start := time.Now()

	// 1. 从 Server 获取任务
	devices, err := c.fetchTasks(ctx)
	if err != nil {
		return fmt.Errorf("获取任务失败: %w", err)
	}

	if len(devices) == 0 {
		c.logger.Warn("没有分配的设备")
		return nil
	}

	// 2. 过滤慢速设备（如果还没到采集时间则跳过）
	devicesToCollect := c.slowDevices.Filter(devices)

	c.logger.Info("开始采集",
		zap.Int("total_devices", len(devices)),
		zap.Int("devices_to_collect", len(devicesToCollect)),
		zap.Int("slow_devices_skipped", len(devices)-len(devicesToCollect)))

	if len(devicesToCollect) == 0 {
		c.logger.Info("本次周期没有需要采集的设备")
		return nil
	}

	// 3. SNMP 采集
	results := c.snmpCollector.CollectDevices(devicesToCollect)

	// 4. 更新慢速设备的采集时间记录
	c.slowDevices.MarkCollected(devicesToCollect)

	// 5. 提交结果给 Server
	if err := c.submitResults(ctx, results); err != nil {
		return fmt.Errorf("提交结果失败: %w", err)
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

	c.logger.Info("采集完成",
		zap.Int("success", successCount),
		zap.Int("fail", len(results)-successCount),
		zap.Int("total_arp", totalARP),
		zap.Duration("duration", time.Since(start)))

	return nil
}

// fetchTasks 从 Server 获取任务
func (c *RemoteCollector) fetchTasks(ctx context.Context) ([]model.Device, error) {
	url := fmt.Sprintf("%s/api/v1/tasks?proxy=%s", c.cfg.Collector.ServerURL, c.cfg.Collector.ProxyName)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	// 添加认证 Header
	if c.cfg.Collector.AuthToken != "" {
		req.Header.Set("X-Auth-Token", c.cfg.Collector.AuthToken)
	}
	req.Header.Set("X-Proxy-Name", c.cfg.Collector.ProxyName)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Server 返回错误: %d - %s", resp.StatusCode, string(body))
	}

	var taskResp struct {
		ProxyName   string         `json:"proxy_name"`
		Devices     []model.Device `json:"devices"`
		DeviceCount int            `json:"device_count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	c.logger.Debug("获取任务成功",
		zap.String("proxy", taskResp.ProxyName),
		zap.Int("count", taskResp.DeviceCount))

	return taskResp.Devices, nil
}

// submitResults 提交结果给 Server
func (c *RemoteCollector) submitResults(ctx context.Context, results []model.CollectResult) error {
	url := fmt.Sprintf("%s/api/v1/results?proxy=%s", c.cfg.Collector.ServerURL, c.cfg.Collector.ProxyName)

	data, err := json.Marshal(results)
	if err != nil {
		return fmt.Errorf("序列化结果失败: %w", err)
	}

	// 计算总 ARP 条目数用于日志
	totalARP := 0
	for _, r := range results {
		totalARP += r.ARPCount
	}

	c.logger.Info("开始提交采集结果",
		zap.String("url", url),
		zap.Int("result_count", len(results)),
		zap.Int("total_arp_entries", totalARP),
		zap.Int("data_size_bytes", len(data)),
	)

	// 使用独立的超时 context，不受父 context 影响
	submitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(submitCtx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	// 添加认证 Header
	if c.cfg.Collector.AuthToken != "" {
		req.Header.Set("X-Auth-Token", c.cfg.Collector.AuthToken)
	}
	req.Header.Set("X-Proxy-Name", c.cfg.Collector.ProxyName)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Server 返回错误: %d - %s", resp.StatusCode, string(body))
	}

	var resultResp struct {
		Received  int `json:"received"`
		Processed int `json:"processed"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&resultResp); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}

	c.logger.Debug("提交结果成功",
		zap.Int("received", resultResp.Received),
		zap.Int("processed", resultResp.Processed))

	return nil
}
