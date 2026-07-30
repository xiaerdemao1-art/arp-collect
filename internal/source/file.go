package source

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xiaerdemao1-art/arp-collect/internal/config"
	"github.com/xiaerdemao1-art/arp-collect/internal/model"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// FileSource 从 yaml/csv 文件读取设备清单。
// 适合没有 NetBox 的用户：手写一份设备列表即可驱动采集。
type FileSource struct {
	cfg    *config.Config
	logger *zap.Logger

	mu      sync.RWMutex
	devices []model.Device
}

// NewFileSource 创建文件型设备源
func NewFileSource(cfg *config.Config, logger *zap.Logger) *FileSource {
	return &FileSource{cfg: cfg, logger: logger}
}

// fileDevice 文件中的一条设备定义（yaml 字段名）
type fileDevice struct {
	Name          string `yaml:"name"`
	IP            string `yaml:"ip"`
	Role          string `yaml:"role"`
	Site          string `yaml:"site"`
	Region        string `yaml:"region"`
	Proxy         string `yaml:"proxy"`
	SNMPCommunity string `yaml:"snmp_community"`
}

// FetchDevices 重新读取文件并刷新缓存，返回全部设备。
// roles 参数对文件源无意义（文件即清单，不再按角色筛选），保留只为满足接口。
func (f *FileSource) FetchDevices(_ context.Context, _ []string) ([]model.Device, error) {
	devices, err := f.load()
	if err != nil {
		return nil, err
	}

	// proxy 分配：文件显式指定 proxy 则用之，否则按 proxy_mappings 自动分配
	for i := range devices {
		if devices[i].ProxyName == "" {
			devices[i].ProxyName = f.cfg.GetProxyForDevice(
				devices[i].Site,
				devices[i].RegionLevel1,
				devices[i].RegionLevel2,
			)
		}
	}

	f.mu.Lock()
	f.devices = devices
	f.mu.Unlock()

	f.logger.Info("文件设备清单已加载", zap.Int("count", len(devices)))
	return devices, nil
}

// GetCachedDevices 返回缓存的设备列表
func (f *FileSource) GetCachedDevices() []model.Device {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.devices
}

// GetDevicesForProxy 返回分配给指定 proxy 的设备
func (f *FileSource) GetDevicesForProxy(proxyName string) []model.Device {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var result []model.Device
	for _, d := range f.devices {
		if d.ProxyName == proxyName {
			result = append(result, d)
		}
	}
	return result
}

// load 读取并解析设备文件（按扩展名或 config.format 推断格式）
func (f *FileSource) load() ([]model.Device, error) {
	path := f.cfg.DeviceSource.File.Path
	if path == "" {
		return nil, fmt.Errorf("device_source.file.path 未配置")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取设备文件失败 %s: %w", path, err)
	}

	format := f.cfg.DeviceSource.File.Format
	if format == "" {
		format = inferFormat(path)
	}

	var fd []fileDevice
	switch strings.ToLower(format) {
	case "yaml", "yml":
		var doc struct {
			Devices []fileDevice `yaml:"devices"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("解析 yaml 失败: %w", err)
		}
		fd = doc.Devices
	case "csv":
		fd, err = parseCSV(data)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("不支持的设备文件格式 %q（yaml | csv）", format)
	}

	devices := make([]model.Device, 0, len(fd))
	for _, d := range fd {
		if d.Name == "" || d.IP == "" {
			f.logger.Warn("跳过缺少 name/ip 的设备条目", zap.Any("device", d))
			continue
		}
		devices = append(devices, model.Device{
			Name:          d.Name,
			PrimaryIP:     d.IP,
			Role:          d.Role,
			Site:          d.Site,
			RegionLevel2:  d.Region, // 文件的 region 映射到 Level2（proxy 分配的 L2 优先级）
			ProxyName:     d.Proxy,
			SNMPCommunity: d.SNMPCommunity,
		})
	}
	return devices, nil
}

// inferFormat 按文件扩展名推断格式
func inferFormat(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv":
		return "csv"
	case ".yaml", ".yml":
		return "yaml"
	default:
		return ""
	}
}

// parseCSV 解析 CSV（首行表头：name,ip,role,site,region,proxy,snmp_community）
func parseCSV(data []byte) ([]fileDevice, error) {
	r := csv.NewReader(strings.NewReader(string(data)))
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1 // 允许行末缺省字段（用户省略尾部空列时不报错）
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("解析 csv 失败: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("csv 至少需要表头 + 1 行数据")
	}

	header := records[0]
	idx := make(map[string]int, len(header))
	for i, h := range header {
		idx[strings.TrimSpace(strings.ToLower(h))] = i
	}

	devices := make([]fileDevice, 0, len(records)-1)
	for _, row := range records[1:] {
		if len(row) == 0 || strings.TrimSpace(row[0]) == "" {
			continue
		}
		get := func(name string) string {
			if i, ok := idx[name]; ok && i < len(row) {
				return strings.TrimSpace(row[i])
			}
			return ""
		}
		devices = append(devices, fileDevice{
			Name:          get("name"),
			IP:            get("ip"),
			Role:          get("role"),
			Site:          get("site"),
			Region:        get("region"),
			Proxy:         get("proxy"),
			SNMPCommunity: get("snmp_community"),
		})
	}
	return devices, nil
}
