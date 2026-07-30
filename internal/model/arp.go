package model

import "time"

// ARPEntry 单条 ARP 记录（与 Zabbix 输出兼容）
type ARPEntry struct {
	IP        string `json:"ip"`
	MAC       string `json:"mac"`
	Interface string `json:"interface"`
	Type      string `json:"type"`
}

// Device NetBox 设备信息（与 Vector 标签格式对齐）
type Device struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	PrimaryIP       string `json:"mgmt_ip"`
	Role            string `json:"role"`
	Manufacturer    string `json:"manufacturer"`
	Site            string `json:"site"` // 格式: slug-name
	ContactName     string `json:"contact_name"`
	ContactEmail    string `json:"contact_email"`
	RegionLevel1    string `json:"region_level_1"`                 // 中国
	RegionLevel2    string `json:"region_level_2"`                 // 广东/北京
	RegionLevel3    string `json:"region_level_3"`                 // 广州/深圳（可能为空）
	ProxyName       string `json:"proxy_name"`                     // 分配的采集节点
	SNMPCommunity   string `json:"snmp_community"`                 // SNMP Community（可覆盖默认）
	CollectInterval int64  `json:"collect_interval_sec,omitempty"` // 采集间隔（秒），0 表示使用默认值
}

// CollectResult 单设备采集结果
type CollectResult struct {
	DeviceID     int        `json:"device_id"`
	DeviceName   string     `json:"device_name"`
	MgmtIP       string     `json:"mgmt_ip"`
	Role         string     `json:"role"`
	Manufacturer string     `json:"manufacturer"`
	Site         string     `json:"site"`
	ContactName  string     `json:"contact_name"`
	ContactEmail string     `json:"contact_email"`
	RegionLevel1 string     `json:"region_level_1"`
	RegionLevel2 string     `json:"region_level_2"`
	RegionLevel3 string     `json:"region_level_3"`
	ProxyName    string     `json:"proxy_name"`
	Entries      []ARPEntry `json:"entries"`
	ARPCount     int        `json:"arp_count"`
	Status       string     `json:"status"` // success/partial/fail
	DurationMs   int64      `json:"duration_ms"`
	Error        string     `json:"error,omitempty"`
	CollectedAt  time.Time  `json:"collected_at"`
}
