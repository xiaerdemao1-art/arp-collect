package collector

import (
	"sync"
	"time"

	"github.com/xiaerdemao1-art/arp-collect/internal/model"

	"go.uber.org/zap"
)

// SlowDeviceTracker 跟踪慢速设备的上次采集时间，
// 用于在采集周期到达时跳过还未到期的慢速设备。
// 分布式模式（RemoteCollector）和单机模式（standalone daemon）共用。
type SlowDeviceTracker struct {
	mu      sync.RWMutex
	lastRun map[string]time.Time // key: device IP
	logger  *zap.Logger
}

// NewSlowDeviceTracker 创建慢速设备跟踪器
func NewSlowDeviceTracker(logger *zap.Logger) *SlowDeviceTracker {
	return &SlowDeviceTracker{
		lastRun: make(map[string]time.Time),
		logger:  logger,
	}
}

// Filter 过滤出本次需要采集的设备：
//   - CollectInterval == 0 的普通设备始终采集
//   - 慢速设备仅当距离上次采集已超过其自定义间隔时才采集（首次必采）
func (t *SlowDeviceTracker) Filter(devices []model.Device) []model.Device {
	now := time.Now()
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []model.Device
	for _, d := range devices {
		// 没有自定义采集间隔：始终采集
		if d.CollectInterval == 0 {
			result = append(result, d)
			continue
		}

		// 首次采集：直接采集
		lastRun, exists := t.lastRun[d.PrimaryIP]
		if !exists {
			result = append(result, d)
			continue
		}

		// 到达自定义间隔才采集
		interval := time.Duration(d.CollectInterval) * time.Second
		elapsed := now.Sub(lastRun)
		if elapsed >= interval {
			result = append(result, d)
		} else {
			t.logger.Debug("跳过慢速设备",
				zap.String("device", d.Name),
				zap.String("ip", d.PrimaryIP),
				zap.Duration("interval", interval),
				zap.Duration("elapsed", elapsed),
				zap.Duration("remaining", interval-elapsed))
		}
	}
	return result
}

// MarkCollected 记录本次已采集的慢速设备的采集时间
func (t *SlowDeviceTracker) MarkCollected(devices []model.Device) {
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, d := range devices {
		if d.CollectInterval > 0 {
			t.lastRun[d.PrimaryIP] = now
		}
	}
}
