package source

import (
	"context"
	"sync"

	"github.com/xiaerdemao1-art/arp-collect/internal/config"
	"github.com/xiaerdemao1-art/arp-collect/internal/model"
	"github.com/xiaerdemao1-art/arp-collect/internal/storage"

	"go.uber.org/zap"
)

// DatabaseSource 从 PostgreSQL 的 arp_device 表读取设备清单。
// 配合 Web 主机管理页使用：在页面上增删改设备，server 即从这里读取。
type DatabaseSource struct {
	cfg    *config.Config
	store  *storage.PostgresStore
	logger *zap.Logger

	mu      sync.RWMutex
	devices []model.Device
}

// NewDatabaseSource 创建数据库型设备源
func NewDatabaseSource(cfg *config.Config, store *storage.PostgresStore, logger *zap.Logger) *DatabaseSource {
	return &DatabaseSource{cfg: cfg, store: store, logger: logger}
}

// FetchDevices 从 arp_device 表加载设备并刷新缓存。
// roles 参数对数据库源无意义（表里即清单），保留只为满足接口。
func (d *DatabaseSource) FetchDevices(ctx context.Context, _ []string) ([]model.Device, error) {
	hosts, err := d.store.ListAllHosts(ctx)
	if err != nil {
		return nil, err
	}

	devices := make([]model.Device, 0, len(hosts))
	for _, h := range hosts {
		proxy := h.Proxy
		if proxy == "" {
			proxy = d.cfg.GetProxyForDevice(h.Site, "", h.Region)
		}
		devices = append(devices, model.Device{
			ID:            h.ID,
			Name:          h.Name,
			PrimaryIP:     h.IP,
			Role:          h.Role,
			Site:          h.Site,
			RegionLevel2:  h.Region,
			ProxyName:     proxy,
			SNMPCommunity: h.SNMPCommunity,
		})
	}

	d.mu.Lock()
	d.devices = devices
	d.mu.Unlock()

	d.logger.Info("数据库设备清单已加载", zap.Int("count", len(devices)))
	return devices, nil
}

// GetCachedDevices 返回缓存的设备列表
func (d *DatabaseSource) GetCachedDevices() []model.Device {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.devices
}

// GetDevicesForProxy 返回分配给指定 proxy 的设备
func (d *DatabaseSource) GetDevicesForProxy(proxyName string) []model.Device {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var result []model.Device
	for _, dev := range d.devices {
		if dev.ProxyName == proxyName {
			result = append(result, dev)
		}
	}
	return result
}
