// Package source 提供可插拔的设备清单来源抽象。
// 当前实现：netbox（默认）、file（yaml/csv 文件）。
// 通过 config.device_source.type 选择，由 New 工厂返回对应实现。
package source

import (
	"context"
	"fmt"

	"github.com/xiaerdemao1-art/arp-collect/internal/config"
	"github.com/xiaerdemao1-art/arp-collect/internal/model"
	"github.com/xiaerdemao1-art/arp-collect/internal/netbox"

	"go.uber.org/zap"
)

// DeviceSource 设备清单来源接口。
// Server（refreshDevices）和 standalone collector 通过它获取设备列表；
// 分布式 collector 不直接用本接口（从 Server 拉任务）。
type DeviceSource interface {
	// FetchDevices 拉取/加载设备清单并刷新内部缓存，返回全部设备。
	FetchDevices(ctx context.Context, roles []string) ([]model.Device, error)
	// GetCachedDevices 返回最近一次 FetchDevices 的缓存。
	GetCachedDevices() []model.Device
	// GetDevicesForProxy 返回缓存中分配给指定 proxy 的设备。
	GetDevicesForProxy(proxyName string) []model.Device
}

// New 按 cfg.DeviceSource.Type 创建设备源。
func New(cfg *config.Config, logger *zap.Logger) (DeviceSource, error) {
	switch cfg.DeviceSource.Type {
	case "", "netbox":
		return netbox.NewClient(cfg, logger), nil
	case "file":
		return NewFileSource(cfg, logger), nil
	default:
		return nil, fmt.Errorf("unknown device_source.type %q (netbox | file)", cfg.DeviceSource.Type)
	}
}
