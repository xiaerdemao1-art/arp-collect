package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 全局配置
type Config struct {
	NetBox          NetBoxConfig          `mapstructure:"netbox"`
	SNMP            SNMPConfig            `mapstructure:"snmp"`
	Postgres        PostgresConfig        `mapstructure:"postgres"`
	Kafka           KafkaConfig           `mapstructure:"kafka"`
	Server          ServerConfig          `mapstructure:"server"`
	Collector       CollectorConfig       `mapstructure:"collector"`
	Logging         LoggingConfig         `mapstructure:"logging"`
	VictoriaMetrics VictoriaMetricsConfig `mapstructure:"victoria_metrics"`

	// DeviceSource 设备清单来源（netbox | file），缺省 netbox
	DeviceSource DeviceSourceConfig `mapstructure:"device_source"`

	// ProxyMappings region/site/role 到 proxy 的映射规则
	// 数组顺序即优先级：靠前的规则先匹配
	ProxyMappings []ProxyMappingConfig `mapstructure:"proxy_mappings"`
	// DefaultProxy 没有任何规则命中时使用的 proxy 名称
	DefaultProxy string `mapstructure:"default_proxy"`
}

// DeviceSourceConfig 设备清单来源配置
type DeviceSourceConfig struct {
	Type string           `mapstructure:"type"` // netbox | file（空=netbox）
	File FileSourceConfig `mapstructure:"file"`
}

// FileSourceConfig 文件型设备源配置
type FileSourceConfig struct {
	Path   string `mapstructure:"path"`   // 设备清单文件路径（.yaml/.yml 或 .csv）
	Format string `mapstructure:"format"` // yaml | csv，空=按扩展名推断
}

// NetBoxConfig NetBox API 配置
type NetBoxConfig struct {
	URL   string   `mapstructure:"url"`
	Token string   `mapstructure:"token"`
	Roles []string `mapstructure:"roles"`
}

// SNMPConfig SNMP 采集参数
type SNMPConfig struct {
	Community       string        `mapstructure:"community"`
	Timeout         time.Duration `mapstructure:"timeout"`
	Retries         int           `mapstructure:"retries"`
	BulkRepetitions uint32        `mapstructure:"bulk_repetitions"`
	MaxConcurrency  int           `mapstructure:"max_concurrency"`
}

// PostgresConfig PostgreSQL 连接配置
type PostgresConfig struct {
	Host           string `mapstructure:"host"`
	Port           int    `mapstructure:"port"`
	User           string `mapstructure:"user"`
	Password       string `mapstructure:"password"`
	Database       string `mapstructure:"database"`
	MaxConnections int    `mapstructure:"max_connections"`
}

// KafkaConfig Kafka 配置
type KafkaConfig struct {
	Enabled      bool     `mapstructure:"enabled"`
	Brokers      []string `mapstructure:"brokers"`
	Topic        string   `mapstructure:"topic"`
	Username     string   `mapstructure:"username"`
	Password     string   `mapstructure:"password"`
	Compression  string   `mapstructure:"compression"`
	BatchSize    int      `mapstructure:"batch_size"`
	Async        bool     `mapstructure:"async"`
	TLS          bool     `mapstructure:"tls"`
	BatchTimeout int      `mapstructure:"batch_timeout"`
}

// ServerConfig Server 配置（用于分布式模式）
type ServerConfig struct {
	Listen         string         `mapstructure:"listen"`          // HTTP 监听地址
	Mode           string         `mapstructure:"mode"`            // server / collector / standalone
	AuthToken      string         `mapstructure:"auth_token"`      // Collector API 认证 Token
	AllowedProxies []AllowedProxy `mapstructure:"allowed_proxies"` // 允许的 Proxy 列表
	WebAuth        WebAuthConfig  `mapstructure:"web_auth"`        // Web UI Basic Auth
}

// WebAuthConfig Web UI Basic Auth 配置
type WebAuthConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

// AllowedProxy 允许连接的 Proxy（类似 Zabbix 的 hostname+IP 校验）
type AllowedProxy struct {
	Name string `mapstructure:"name"` // Proxy 名称
	IP   string `mapstructure:"ip"`   // Proxy IP 地址（留空则只校验名称）
}

// CollectorConfig 采集器配置
type CollectorConfig struct {
	ProxyName     string        `mapstructure:"proxy_name"`     // 当前节点名称
	ServerURL     string        `mapstructure:"server_url"`     // Server API 地址（分布式模式）
	AuthToken     string        `mapstructure:"auth_token"`     // 认证 Token
	Interval      time.Duration `mapstructure:"interval"`       // 采集周期
	DeviceRefresh time.Duration `mapstructure:"device_refresh"` // 设备列表刷新周期
	SingleRun     bool          `mapstructure:"single_run"`     // 单次运行模式
	SlowDevices   []SlowDevice  `mapstructure:"slow_devices"`   // 慢速采集设备列表
}

// SlowDevice 慢速采集设备配置
type SlowDevice struct {
	IP       string        `mapstructure:"ip"`       // 设备 IP 地址
	Name     string        `mapstructure:"name"`     // 设备名称（可选，仅用于注释）
	Interval time.Duration `mapstructure:"interval"` // 采集周期（如 20m）
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	Level string `mapstructure:"level"`
	File  string `mapstructure:"file"`
}

// VictoriaMetricsConfig VictoriaMetrics 推送配置
type VictoriaMetricsConfig struct {
	Enabled  bool          `mapstructure:"enabled"`
	URL      string        `mapstructure:"url"`
	User     string        `mapstructure:"user"`
	Password string        `mapstructure:"password"`
	Interval time.Duration `mapstructure:"interval"` // 推送周期
}

// ProxyMappingConfig region/site/role 到 proxy 的单条映射规则
// Sites / Regions / Roles 之间是 OR 关系，命中任意一个即匹配
type ProxyMappingConfig struct {
	Proxy   string   `mapstructure:"proxy"`   // 目标 proxy 名称
	Sites   []string `mapstructure:"sites"`   // 站点名（支持子串匹配）
	Regions []string `mapstructure:"regions"` // Region 名（L1/L2，支持子串匹配）
	Roles   []string `mapstructure:"roles"`   // 设备角色名（精确匹配）
}

// Load 加载配置文件
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// 设置默认值
	setDefaults(v)

	// 配置文件
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath("./config")
		v.AddConfigPath("/opt/arp-collect/config") // install.sh 默认布局
		v.AddConfigPath("/etc/arp-collect")        // 系统级配置目录
	}

	// 环境变量覆盖
	v.SetEnvPrefix("ARP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// 读取配置
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("读取配置文件失败: %w", err)
		}
		// 配置文件不存在，使用默认值
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	// 环境变量替换（支持 ${VAR} 语法，用于所有敏感字段）
	cfg.NetBox.Token = expandEnv(cfg.NetBox.Token)
	cfg.Postgres.Password = expandEnv(cfg.Postgres.Password)
	cfg.SNMP.Community = expandEnv(cfg.SNMP.Community)
	cfg.Kafka.Username = expandEnv(cfg.Kafka.Username)
	cfg.Kafka.Password = expandEnv(cfg.Kafka.Password)
	cfg.VictoriaMetrics.Password = expandEnv(cfg.VictoriaMetrics.Password)
	cfg.Server.AuthToken = expandEnv(cfg.Server.AuthToken)
	cfg.Server.WebAuth.Password = expandEnv(cfg.Server.WebAuth.Password)
	cfg.Collector.AuthToken = expandEnv(cfg.Collector.AuthToken)

	return &cfg, nil
}

// Validate 校验配置完整性，缺少必填项时返回错误
// mode 取 cfg.Server.Mode（server / collector / standalone）
func (c *Config) Validate() error {
	mode := c.Server.Mode

	switch mode {
	case "server", "standalone":
		// 设备源校验：netbox 需要 url+token；file 需要 path
		switch c.DeviceSource.Type {
		case "", "netbox":
			if c.NetBox.URL == "" {
				return fmt.Errorf("缺少必填配置 netbox.url")
			}
			if c.NetBox.Token == "" {
				return fmt.Errorf("缺少必填配置 netbox.token（可通过配置文件或 ARP_NETBOX_TOKEN 环境变量设置）")
			}
		case "file":
			if c.DeviceSource.File.Path == "" {
				return fmt.Errorf("device_source.type=file 但未设置 device_source.file.path")
			}
		default:
			return fmt.Errorf("未知 device_source.type %q（可选: netbox | file）", c.DeviceSource.Type)
		}
		if c.Postgres.Password == "" {
			return fmt.Errorf("缺少必填配置 postgres.password（可通过配置文件或 ARP_POSTGRES_PASSWORD 环境变量设置）")
		}
		if mode == "server" {
			if c.Server.AuthToken == "" {
				return fmt.Errorf("缺少必填配置 server.auth_token（Collector API 认证 Token，可通过 ARP_SERVER_AUTHTOKEN 环境变量设置）")
			}
			if c.Server.WebAuth.Enabled && c.Server.WebAuth.Password == "" {
				return fmt.Errorf("web_auth.enabled=true 但未设置 web_auth.password（可通过 ARP_SERVER_WEBAUTH_PASSWORD 环境变量设置）")
			}
		}
	case "collector":
		if c.Collector.ServerURL == "" {
			return fmt.Errorf("缺少必填配置 collector.server_url")
		}
		if c.Collector.AuthToken == "" {
			return fmt.Errorf("缺少必填配置 collector.auth_token（需与 Server 端 server.auth_token 一致）")
		}
	default:
		return fmt.Errorf("未知运行模式 %q（可选: server / collector / standalone）", mode)
	}

	return nil
}

func setDefaults(v *viper.Viper) {
	// 设备源（缺省 netbox）
	v.SetDefault("device_source.type", "netbox")

	// NetBox
	v.SetDefault("netbox.url", "http://localhost:8000")
	v.SetDefault("netbox.token", "")
	v.SetDefault("netbox.roles", []string{"CoreSwitch", "DistributionSwitch"})

	// SNMP
	v.SetDefault("snmp.community", "public")
	v.SetDefault("snmp.timeout", "5s")
	v.SetDefault("snmp.retries", 2)
	v.SetDefault("snmp.bulk_repetitions", 50)
	v.SetDefault("snmp.max_concurrency", 20)

	// PostgreSQL
	v.SetDefault("postgres.host", "localhost")
	v.SetDefault("postgres.port", 5432)
	v.SetDefault("postgres.user", "postgres")
	v.SetDefault("postgres.password", "")
	v.SetDefault("postgres.database", "postgres")
	v.SetDefault("postgres.max_connections", 10)

	// Kafka（默认禁用，按需开启）
	v.SetDefault("kafka.enabled", false)
	v.SetDefault("kafka.brokers", []string{"localhost:9092"})
	v.SetDefault("kafka.topic", "arp_collect")
	v.SetDefault("kafka.username", "")
	v.SetDefault("kafka.password", "")
	v.SetDefault("kafka.compression", "lz4")
	v.SetDefault("kafka.batch_size", 5000)
	v.SetDefault("kafka.async", false)

	// Server
	v.SetDefault("server.listen", ":19527")
	v.SetDefault("server.mode", "standalone") // standalone / server / collector
	v.SetDefault("server.auth_token", "")
	v.SetDefault("server.web_auth.enabled", true)
	v.SetDefault("server.web_auth.username", "admin")
	v.SetDefault("server.web_auth.password", "")

	// Collector
	v.SetDefault("collector.proxy_name", "default")
	v.SetDefault("collector.server_url", "http://localhost:19527")
	v.SetDefault("collector.auth_token", "")
	v.SetDefault("collector.interval", "10m")      // ARP 采集周期：每 10 分钟一次
	v.SetDefault("collector.device_refresh", "1h") // 设备列表刷新：每 1 小时一次
	v.SetDefault("collector.single_run", false)

	// Proxy 分配
	v.SetDefault("default_proxy", "default")

	// Logging
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.file", "")
}

// expandEnv 替换 ${VAR} 为环境变量值
func expandEnv(s string) string {
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		envKey := s[2 : len(s)-1]
		if val := os.Getenv(envKey); val != "" {
			return val
		}
	}
	return s
}

// GetProxyForDevice 根据设备信息返回对应的 proxy 名称
// 优先级: Site 匹配 > Region Level2 匹配 > Region Level1 匹配 > DefaultProxy
// 规则按 ProxyMappings 数组顺序匹配，靠前的规则优先
func (c *Config) GetProxyForDevice(site, regionLevel1, regionLevel2 string) string {
	// 1. Site 匹配（优先级最高）
	for _, rule := range c.ProxyMappings {
		for _, s := range rule.Sites {
			if s != "" && (strings.Contains(site, s) || s == site) {
				return rule.Proxy
			}
		}
	}

	// 2. Region Level2 匹配（省/市级别）
	if regionLevel2 != "" {
		for _, rule := range c.ProxyMappings {
			for _, r := range rule.Regions {
				if r != "" && (r == regionLevel2 || strings.Contains(regionLevel2, r)) {
					return rule.Proxy
				}
			}
		}
	}

	// 3. Region Level1 匹配
	if regionLevel1 != "" {
		for _, rule := range c.ProxyMappings {
			for _, r := range rule.Regions {
				if r != "" && r == regionLevel1 {
					return rule.Proxy
				}
			}
		}
	}

	// 4. 默认 proxy
	return c.DefaultProxy
}
