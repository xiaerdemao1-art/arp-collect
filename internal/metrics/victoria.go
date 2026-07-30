package metrics

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/xiaerdemao1-art/arp-collect/internal/config"
	"github.com/xiaerdemao1-art/arp-collect/internal/model"

	"go.uber.org/zap"
)

// VictoriaMetricsPusher 指标推送器（事件触发模式：收到采集结果时推送）
type VictoriaMetricsPusher struct {
	cfg    *config.VictoriaMetricsConfig
	logger *zap.Logger
	client *http.Client
	stopCh chan struct{}
}

// NewVictoriaMetricsPusher 创建推送器
func NewVictoriaMetricsPusher(cfg *config.VictoriaMetricsConfig, logger *zap.Logger) *VictoriaMetricsPusher {
	return &VictoriaMetricsPusher{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{Timeout: 30 * time.Second},
		stopCh: make(chan struct{}),
	}
}

// Start 启动（保留接口兼容，但不再定时推送）
func (p *VictoriaMetricsPusher) Start(ctx context.Context) {
	if !p.cfg.Enabled {
		p.logger.Info("VictoriaMetrics 推送已禁用")
		return
	}

	p.logger.Info("VictoriaMetrics 已启用（事件触发模式）",
		zap.String("url", p.cfg.URL))

	// 保持 goroutine 存活，等待上下文取消
	<-ctx.Done()
}

// Stop 停止推送
func (p *VictoriaMetricsPusher) Stop() {
	select {
	case <-p.stopCh:
		// 已经关闭
	default:
		close(p.stopCh)
	}
}

// PushResults 事件触发式推送 - 收到采集结果时立即推送指标
// 注意：使用 background context 而非请求 context，避免 HTTP 响应返回后取消推送
func (p *VictoriaMetricsPusher) PushResults(_ context.Context, results []model.CollectResult) {
	if !p.cfg.Enabled || len(results) == 0 {
		return
	}

	// 构建 Prometheus 格式的指标
	var lines []string
	timestamp := time.Now().UnixMilli()

	for _, result := range results {
		// 构建标签
		labels := p.buildResultLabels(result)

		// arp_collect_device_arp_count - ARP 条目数量
		lines = append(lines, fmt.Sprintf("arp_collect_device_arp_count{%s} %d %d",
			labels, result.ARPCount, timestamp))

		// arp_collect_device_duration_ms - 采集耗时
		lines = append(lines, fmt.Sprintf("arp_collect_device_duration_ms{%s} %d %d",
			labels, result.DurationMs, timestamp))

		// arp_collect_device_status - 采集状态 (1=success, 0=fail)
		statusValue := 0
		if result.Status == "success" {
			statusValue = 1
		}
		lines = append(lines, fmt.Sprintf("arp_collect_device_status{%s} %d %d",
			labels, statusValue, timestamp))
	}

	if len(lines) == 0 {
		return
	}

	// 异步推送，使用独立 context（30秒超时），避免阻塞主流程
	pushCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	go func() {
		defer cancel()
		p.pushLines(pushCtx, lines, len(results))
	}()
}

// pushLines 推送指标行
func (p *VictoriaMetricsPusher) pushLines(ctx context.Context, lines []string, deviceCount int) {
	body := strings.Join(lines, "\n")
	req, err := http.NewRequestWithContext(ctx, "POST", p.cfg.URL, bytes.NewBufferString(body))
	if err != nil {
		p.logger.Error("创建请求失败", zap.Error(err))
		return
	}

	req.Header.Set("Content-Type", "text/plain")
	if p.cfg.User != "" {
		req.SetBasicAuth(p.cfg.User, p.cfg.Password)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		p.logger.Error("推送指标失败", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		p.logger.Error("推送指标返回错误", zap.Int("status", resp.StatusCode))
		return
	}

	p.logger.Debug("指标推送成功",
		zap.Int("metrics_count", len(lines)),
		zap.Int("devices", deviceCount))
}

// buildResultLabels 从采集结果构建标签
func (p *VictoriaMetricsPusher) buildResultLabels(result model.CollectResult) string {
	escape := func(s string) string {
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, "\"", "\\\"")
		s = strings.ReplaceAll(s, "\n", "\\n")
		return s
	}

	labels := []string{
		fmt.Sprintf("device_name=\"%s\"", escape(result.DeviceName)),
		fmt.Sprintf("device_ip=\"%s\"", escape(result.MgmtIP)),
		fmt.Sprintf("role=\"%s\"", escape(result.Role)),
		fmt.Sprintf("manufacturer=\"%s\"", escape(result.Manufacturer)),
		fmt.Sprintf("site=\"%s\"", escape(result.Site)),
		fmt.Sprintf("contact_name=\"%s\"", escape(result.ContactName)),
		fmt.Sprintf("contact_email=\"%s\"", escape(result.ContactEmail)),
		fmt.Sprintf("region_level_1=\"%s\"", escape(result.RegionLevel1)),
		fmt.Sprintf("region_level_2=\"%s\"", escape(result.RegionLevel2)),
		fmt.Sprintf("region_level_3=\"%s\"", escape(result.RegionLevel3)),
		fmt.Sprintf("proxy_name=\"%s\"", escape(result.ProxyName)),
	}

	return strings.Join(labels, ",")
}
