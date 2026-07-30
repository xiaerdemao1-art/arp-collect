package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestValidate 校验必填配置检查
func TestValidate(t *testing.T) {
	base := func() *Config {
		return &Config{
			NetBox:   NetBoxConfig{URL: "http://localhost:8000", Token: "token"},
			Postgres: PostgresConfig{Host: "localhost", Password: "pass"},
			Server: ServerConfig{
				Mode:      "server",
				AuthToken: "tok",
				WebAuth:   WebAuthConfig{Enabled: true, Username: "admin", Password: "pw"},
			},
			Collector: CollectorConfig{ServerURL: "http://localhost:19527", AuthToken: "tok"},
		}
	}

	t.Run("server ok", func(t *testing.T) {
		if err := base().Validate(); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("server missing netbox token", func(t *testing.T) {
		c := base()
		c.NetBox.Token = ""
		if err := c.Validate(); err == nil {
			t.Fatal("expected error for missing netbox token")
		}
	})

	t.Run("server missing postgres password", func(t *testing.T) {
		c := base()
		c.Postgres.Password = ""
		if err := c.Validate(); err == nil {
			t.Fatal("expected error for missing postgres password")
		}
	})

	t.Run("server web_auth enabled without password", func(t *testing.T) {
		c := base()
		c.Server.WebAuth.Password = ""
		if err := c.Validate(); err == nil {
			t.Fatal("expected error for empty web_auth password")
		}
	})

	t.Run("file source skips netbox token requirement", func(t *testing.T) {
		c := base()
		c.DeviceSource.Type = "file"
		c.DeviceSource.File.Path = "/etc/arp-collect/devices.yaml"
		c.NetBox.Token = "" // 文件源不需要 NetBox 凭据
		c.NetBox.URL = ""
		if err := c.Validate(); err != nil {
			t.Fatalf("file source should not require netbox creds, got %v", err)
		}
	})

	t.Run("file source requires file path", func(t *testing.T) {
		c := base()
		c.DeviceSource.Type = "file"
		c.DeviceSource.File.Path = ""
		if err := c.Validate(); err == nil {
			t.Fatal("expected error for missing file path")
		}
	})

	t.Run("collector ok", func(t *testing.T) {
		c := base()
		c.Server.Mode = "collector"
		if err := c.Validate(); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("collector missing auth token", func(t *testing.T) {
		c := base()
		c.Server.Mode = "collector"
		c.Collector.AuthToken = ""
		if err := c.Validate(); err == nil {
			t.Fatal("expected error for missing collector auth token")
		}
	})

	t.Run("unknown mode", func(t *testing.T) {
		c := base()
		c.Server.Mode = "bogus"
		if err := c.Validate(); err == nil {
			t.Fatal("expected error for unknown mode")
		}
	})
}

// TestGetProxyForDevice 校验 proxy 分配规则优先级
func TestGetProxyForDevice(t *testing.T) {
	cfg := &Config{
		DefaultProxy: "fallback",
		ProxyMappings: []ProxyMappingConfig{
			{Proxy: "site-proxy", Sites: []string{"DC-East"}},
			{Proxy: "region-proxy", Regions: []string{"Region-South"}},
			{Proxy: "first-wins", Regions: []string{"Region-South"}},
		},
	}

	tests := []struct {
		name                     string
		site, regionL1, regionL2 string
		want                     string
	}{
		{"site match wins", "DC-East-BuildingA", "", "", "site-proxy"},
		{"region L2 match", "Other-Site", "", "Region-South-City", "region-proxy"},
		{"region L1 match", "Other-Site", "Region-South", "", "region-proxy"},
		{"first rule wins on tie", "Other-Site", "", "Region-South", "region-proxy"},
		{"fallback", "Nowhere", "", "", "fallback"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.GetProxyForDevice(tt.site, tt.regionL1, tt.regionL2); got != tt.want {
				t.Errorf("GetProxyForDevice(%q, %q, %q) = %q, want %q",
					tt.site, tt.regionL1, tt.regionL2, got, tt.want)
			}
		})
	}
}

// TestLoadFromFile 校验从 YAML 文件加载并展开环境变量
func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
netbox:
  url: "http://192.0.2.10:8000"
  token: "${ARP_TEST_NETBOX_TOKEN}"
postgres:
  password: "${ARP_TEST_PG_PASSWORD}"
server:
  mode: "server"
proxy_mappings:
  - proxy: "p1"
    sites: ["Site-A"]
default_proxy: "p0"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ARP_TEST_NETBOX_TOKEN", "secret-token")
	t.Setenv("ARP_TEST_PG_PASSWORD", "secret-pass")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.NetBox.URL != "http://192.0.2.10:8000" {
		t.Errorf("unexpected netbox url: %s", cfg.NetBox.URL)
	}
	if cfg.NetBox.Token != "secret-token" {
		t.Errorf("env expansion failed for netbox token: %q", cfg.NetBox.Token)
	}
	if cfg.Postgres.Password != "secret-pass" {
		t.Errorf("env expansion failed for postgres password: %q", cfg.Postgres.Password)
	}
	if got := cfg.GetProxyForDevice("Site-A-01", "", ""); got != "p1" {
		t.Errorf("proxy mapping from file not applied, got %q", got)
	}
	if got := cfg.GetProxyForDevice("Nowhere", "", ""); got != "p0" {
		t.Errorf("default proxy from file not applied, got %q", got)
	}
}
