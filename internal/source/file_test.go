package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xiaerdemao1-art/arp-collect/internal/config"

	"go.uber.org/zap"
)

const yamlContent = `devices:
  - name: core-sw-01
    ip: 192.0.2.1
    role: CoreSwitch
    site: DC-East
    region: Region-East
  - name: core-sw-02
    ip: 192.0.2.2
    role: CoreSwitch
    proxy: proxy2
  - name: bad-entry
    ip: ""
    role: CoreSwitch
`

const csvContent = `name,ip,role,site,region,proxy,snmp_community
core-sw-01,192.0.2.1,CoreSwitch,DC-East,Region-East,,
core-sw-02,192.0.2.2,CoreSwitch,,,proxy2,
dist-sw-01,192.0.2.3,DistributionSwitch,DC-West,Region-West,,
`

func newTestCfg(path, format string) *config.Config {
	return &config.Config{
		DeviceSource: config.DeviceSourceConfig{
			Type: "file",
			File: config.FileSourceConfig{Path: path, Format: format},
		},
		DefaultProxy: "fallback",
		ProxyMappings: []config.ProxyMappingConfig{
			{Proxy: "east", Sites: []string{"DC-East"}, Regions: []string{"Region-East"}},
			{Proxy: "west", Regions: []string{"Region-West"}},
		},
	}
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFileSource_YAML(t *testing.T) {
	path := writeFile(t, "devices.yaml", yamlContent)
	cfg := newTestCfg(path, "")
	fs := NewFileSource(cfg, zap.NewNop())

	devices, err := fs.FetchDevices(context.Background(), nil)
	if err != nil {
		t.Fatalf("FetchDevices: %v", err)
	}
	// bad-entry (empty ip) 应被跳过 -> 2 台
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devices))
	}

	// core-sw-01: 无显式 proxy，按 site/region 自动分配到 east
	if devices[0].ProxyName != "east" {
		t.Errorf("core-sw-01 proxy = %q, want east", devices[0].ProxyName)
	}
	// core-sw-02: 显式 proxy2 覆盖自动分配
	if devices[1].ProxyName != "proxy2" {
		t.Errorf("core-sw-02 proxy = %q, want proxy2", devices[1].ProxyName)
	}
}

func TestFileSource_CSV(t *testing.T) {
	path := writeFile(t, "devices.csv", csvContent)
	cfg := newTestCfg(path, "")
	fs := NewFileSource(cfg, zap.NewNop())

	devices, err := fs.FetchDevices(context.Background(), nil)
	if err != nil {
		t.Fatalf("FetchDevices: %v", err)
	}
	if len(devices) != 3 {
		t.Fatalf("expected 3 devices, got %d", len(devices))
	}
	// 第三行 dist-sw-01 区域 Region-West -> west
	if devices[2].ProxyName != "west" {
		t.Errorf("dist-sw-01 proxy = %q, want west", devices[2].ProxyName)
	}
}

func TestFileSource_GetDevicesForProxy(t *testing.T) {
	path := writeFile(t, "devices.csv", csvContent)
	cfg := newTestCfg(path, "")
	fs := NewFileSource(cfg, zap.NewNop())

	if _, err := fs.FetchDevices(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	if got := fs.GetDevicesForProxy("east"); len(got) != 1 {
		t.Errorf("east devices = %d, want 1", len(got))
	}
	if got := fs.GetDevicesForProxy("proxy2"); len(got) != 1 {
		t.Errorf("proxy2 devices = %d, want 1", len(got))
	}
	if got := fs.GetDevicesForProxy("nonexistent"); len(got) != 0 {
		t.Errorf("nonexistent devices = %d, want 0", len(got))
	}
}

func TestFileSource_FallbackProxy(t *testing.T) {
	const yaml = `devices:
  - name: sw-99
    ip: 192.0.2.99
    site: Nowhere
`
	path := writeFile(t, "devices.yaml", yaml)
	cfg := newTestCfg(path, "")
	fs := NewFileSource(cfg, zap.NewNop())

	devices, err := fs.FetchDevices(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if devices[0].ProxyName != "fallback" {
		t.Errorf("proxy = %q, want fallback", devices[0].ProxyName)
	}
}

func TestInferFormat(t *testing.T) {
	cases := map[string]string{
		"devices.yaml": "yaml",
		"devices.yml":  "yaml",
		"devices.csv":  "csv",
		"devices.txt":  "",
	}
	for path, want := range cases {
		if got := inferFormat(path); got != want {
			t.Errorf("inferFormat(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestFileSource_MissingPath(t *testing.T) {
	cfg := newTestCfg("", "")
	fs := NewFileSource(cfg, zap.NewNop())
	if _, err := fs.FetchDevices(context.Background(), nil); err == nil {
		t.Fatal("expected error for empty path")
	}
}
