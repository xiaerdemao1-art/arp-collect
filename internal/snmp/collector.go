package snmp

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiaerdemao1-art/arp-collect/internal/config"
	"github.com/xiaerdemao1-art/arp-collect/internal/model"

	"github.com/gosnmp/gosnmp"
	"go.uber.org/zap"
)

// errSampleLimit 采样达到上限时终止 BulkWalk 的哨兵错误
var errSampleLimit = errors.New("sample limit reached")

// OID 定义
const (
	OidArpMac = "1.3.6.1.2.1.4.22.1.2"   // ipNetToMediaPhysAddress
	OidIfName = "1.3.6.1.2.1.31.1.1.1.1" // ifName
)

// Collector SNMP 采集器
type Collector struct {
	cfg       *config.SNMPConfig
	logger    *zap.Logger
	semaphore chan struct{}
}

// NewCollector 创建采集器
func NewCollector(cfg *config.SNMPConfig, logger *zap.Logger) *Collector {
	return &Collector{
		cfg:       cfg,
		logger:    logger,
		semaphore: make(chan struct{}, cfg.MaxConcurrency),
	}
}

// CollectDevice 采集单台设备的 ARP 表
func (c *Collector) CollectDevice(device model.Device) model.CollectResult {
	start := time.Now()
	result := model.CollectResult{
		DeviceID:     device.ID,
		DeviceName:   device.Name,
		MgmtIP:       device.PrimaryIP,
		Role:         device.Role,
		Manufacturer: device.Manufacturer,
		Site:         device.Site,
		ContactName:  device.ContactName,
		ContactEmail: device.ContactEmail,
		RegionLevel1: device.RegionLevel1,
		RegionLevel2: device.RegionLevel2,
		RegionLevel3: device.RegionLevel3,
		ProxyName:    device.ProxyName,
		CollectedAt:  start,
		Status:       "fail",
	}

	// 并发控制
	c.semaphore <- struct{}{}
	defer func() { <-c.semaphore }()

	c.logger.Debug("开始采集设备", zap.String("device", device.Name), zap.String("ip", device.PrimaryIP))

	// 确定使用的 Community
	community := c.cfg.Community
	if device.SNMPCommunity != "" {
		community = device.SNMPCommunity
	}

	// 创建 SNMP 客户端
	snmp := &gosnmp.GoSNMP{
		Target:         device.PrimaryIP,
		Port:           161,
		Community:      community,
		Version:        gosnmp.Version2c,
		Timeout:        c.cfg.Timeout,
		Retries:        c.cfg.Retries,
		MaxRepetitions: c.cfg.BulkRepetitions,
	}

	if err := snmp.Connect(); err != nil {
		result.Error = fmt.Sprintf("SNMP 连接失败: %v", err)
		result.DurationMs = time.Since(start).Milliseconds()
		c.logger.Warn("SNMP 连接失败", zap.String("device", device.Name), zap.Error(err))
		return result
	}
	defer snmp.Conn.Close()

	// 1. 获取 ifName 映射表
	ifNameMap, err := c.fetchIfNameMap(snmp)
	if err != nil {
		c.logger.Warn("获取 ifName 失败，将使用 ifIndex", zap.String("device", device.Name), zap.Error(err))
		ifNameMap = make(map[string]string)
	}

	// 2. 获取 ARP 表（limit=0 表示不限制）
	entries, _, err := c.fetchARPTable(snmp, ifNameMap, 0)
	if err != nil {
		result.Error = fmt.Sprintf("获取 ARP 表失败: %v", err)
		result.DurationMs = time.Since(start).Milliseconds()
		c.logger.Error("获取 ARP 表失败", zap.String("device", device.Name), zap.Error(err))
		return result
	}

	result.Entries = entries
	result.ARPCount = len(entries)
	result.Status = "success"
	result.DurationMs = time.Since(start).Milliseconds()

	c.logger.Info("设备采集完成",
		zap.String("device", device.Name),
		zap.Int("arp_count", result.ARPCount),
		zap.Int64("duration_ms", result.DurationMs))

	return result
}

// CollectDeviceSample 采集设备 ARP 表的采样（最多 limit 条），用于 Web 端 SNMP 测试。
// 比 CollectDevice 快：达到 limit 即停止 BulkWalk，足以验证连通性与采集能力。
// 返回采集结果与是否因达到 limit 而截断的标志。
func (c *Collector) CollectDeviceSample(device model.Device, limit int) (model.CollectResult, bool) {
	start := time.Now()
	result := model.CollectResult{
		DeviceID:    device.ID,
		DeviceName:  device.Name,
		MgmtIP:      device.PrimaryIP,
		CollectedAt: start,
		Status:      "fail",
	}

	c.semaphore <- struct{}{}
	defer func() { <-c.semaphore }()

	community := c.cfg.Community
	if device.SNMPCommunity != "" {
		community = device.SNMPCommunity
	}

	snmp := &gosnmp.GoSNMP{
		Target:         device.PrimaryIP,
		Port:           161,
		Community:      community,
		Version:        gosnmp.Version2c,
		Timeout:        c.cfg.Timeout,
		Retries:        c.cfg.Retries,
		MaxRepetitions: c.cfg.BulkRepetitions,
	}

	if err := snmp.Connect(); err != nil {
		result.Error = fmt.Sprintf("SNMP 连接失败: %v", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result, false
	}
	defer snmp.Conn.Close()

	ifNameMap, err := c.fetchIfNameMap(snmp)
	if err != nil {
		ifNameMap = make(map[string]string)
	}

	entries, truncated, err := c.fetchARPTable(snmp, ifNameMap, limit)
	if err != nil {
		result.Error = fmt.Sprintf("获取 ARP 表失败: %v", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result, false
	}

	result.Entries = entries
	result.ARPCount = len(entries)
	result.Status = "success"
	result.DurationMs = time.Since(start).Milliseconds()
	return result, truncated
}

// CollectDevices 并发采集多台设备
func (c *Collector) CollectDevices(devices []model.Device) []model.CollectResult {
	var wg sync.WaitGroup
	results := make([]model.CollectResult, len(devices))

	for i, device := range devices {
		wg.Add(1)
		go func(idx int, dev model.Device) {
			defer wg.Done()
			results[idx] = c.CollectDevice(dev)
		}(i, device)
	}

	wg.Wait()
	return results
}

// fetchIfNameMap 获取 ifIndex -> ifName 映射
func (c *Collector) fetchIfNameMap(snmp *gosnmp.GoSNMP) (map[string]string, error) {
	result := make(map[string]string)

	err := snmp.BulkWalk(OidIfName, func(pdu gosnmp.SnmpPDU) error {
		// OID 格式: 1.3.6.1.2.1.31.1.1.1.1.<ifIndex>
		parts := strings.Split(pdu.Name, ".")
		if len(parts) == 0 {
			return nil
		}
		ifIndex := parts[len(parts)-1]

		var ifName string
		switch pdu.Type {
		case gosnmp.OctetString:
			ifName = string(pdu.Value.([]byte))
		default:
			ifName = fmt.Sprintf("%v", pdu.Value)
		}

		result[ifIndex] = ifName
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// fetchARPTable 获取 ARP 表。limit>0 时只取前 limit 条（用于 Web SNMP 测试快速采样）。
// 返回 entries、是否因达到 limit 而截断、错误。
func (c *Collector) fetchARPTable(snmp *gosnmp.GoSNMP, ifNameMap map[string]string, limit int) ([]model.ARPEntry, bool, error) {
	var entries []model.ARPEntry

	err := snmp.BulkWalk(OidArpMac, func(pdu gosnmp.SnmpPDU) error {
		// OID 格式: 1.3.6.1.2.1.4.22.1.2.<ifIndex>.<IP>
		// 例如: 1.3.6.1.2.1.4.22.1.2.2.192.0.2.1

		// 解析 ifIndex 和 IP
		ifIndex, ip := parseARPIndex(pdu.Name)
		if ifIndex == "" || ip == "" {
			return nil
		}

		// 解析 MAC
		mac, valid := parseMac(pdu.Value)
		if !valid {
			return nil
		}

		// 查找接口名
		ifName, ok := ifNameMap[ifIndex]
		if !ok {
			ifName = fmt.Sprintf("ifIndex %s", ifIndex)
		}

		entries = append(entries, model.ARPEntry{
			IP:        ip,
			MAC:       mac,
			Interface: ifName,
			Type:      "dynamic",
		})

		if limit > 0 && len(entries) >= limit {
			return errSampleLimit
		}
		return nil
	})

	if err != nil && !errors.Is(err, errSampleLimit) {
		return nil, false, err
	}

	return entries, errors.Is(err, errSampleLimit), nil
}

// parseARPIndex 从 OID 解析 ifIndex 和 IP
// OID 格式: 1.3.6.1.2.1.4.22.1.2.<ifIndex>.<IP>
func parseARPIndex(oid string) (ifIndex, ip string) {
	// 去掉 OID 前缀
	prefix := "." + OidArpMac + "."
	if !strings.HasPrefix(oid, prefix) {
		// 尝试无前导点
		prefix = OidArpMac + "."
		if !strings.HasPrefix(oid, prefix) {
			return "", ""
		}
	}

	suffix := strings.TrimPrefix(oid, prefix)
	parts := strings.SplitN(suffix, ".", 2)
	if len(parts) != 2 {
		return "", ""
	}

	ifIndex = parts[0]
	ip = parts[1]

	// 验证 IP 格式
	ipParts := strings.Split(ip, ".")
	if len(ipParts) != 4 {
		return "", ""
	}

	return ifIndex, ip
}

// parseMac 解析并规范化 MAC 地址
func parseMac(value interface{}) (string, bool) {
	var raw string

	switch v := value.(type) {
	case []byte:
		// 二进制格式（最常见）
		if len(v) == 6 {
			raw = fmt.Sprintf("%02x%02x%02x%02x%02x%02x", v[0], v[1], v[2], v[3], v[4], v[5])
		} else {
			raw = string(v)
		}
	case string:
		raw = v
	default:
		return "", false
	}

	return normalizeMac(raw)
}

// normalizeMac 规范化 MAC 地址格式
// 输出格式: xx:xx:xx:xx:xx:xx（小写，冒号分隔）
func normalizeMac(raw string) (string, bool) {
	// 去除常见前缀
	raw = strings.TrimPrefix(raw, "Hex-STRING: ")
	raw = strings.TrimPrefix(raw, "STRING: ")
	raw = strings.TrimSpace(raw)

	// 移除所有分隔符
	re := regexp.MustCompile(`[:\-\.\s]`)
	clean := re.ReplaceAllString(raw, "")
	clean = strings.ToLower(clean)

	// 验证长度
	if len(clean) != 12 {
		return "", false
	}

	// 验证是否为有效的十六进制
	if _, err := strconv.ParseUint(clean, 16, 64); err != nil {
		return "", false
	}

	// 过滤无效 MAC
	if clean == "000000000000" || clean == "ffffffffffff" {
		return "", false
	}

	// 格式化为 xx:xx:xx:xx:xx:xx
	mac := fmt.Sprintf("%s:%s:%s:%s:%s:%s",
		clean[0:2], clean[2:4], clean[4:6],
		clean[6:8], clean[8:10], clean[10:12])

	return mac, true
}
