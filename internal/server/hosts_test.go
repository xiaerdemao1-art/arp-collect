package server

import "testing"

func TestParseHostsCSV(t *testing.T) {
	csv := `name,ip,role,site,region,proxy,snmp_community
core-sw-01,192.0.2.1,CoreSwitch,DC-East,Region-East,,
core-sw-02,192.0.2.2,CoreSwitch,,,proxy2,
,192.0.2.3,CoreSwitch,,,,
`
	hosts, err := parseHostsCSV([]byte(csv))
	if err != nil {
		t.Fatalf("parseHostsCSV: %v", err)
	}
	// 第三行 name 为空 -> 在 UpsertHosts 里被跳过，但 parse 仍返回；
	// 第四行整行为空被 parse 跳过。这里校验前两条正常解析。
	if len(hosts) < 2 {
		t.Fatalf("expected >=2 hosts, got %d", len(hosts))
	}
	if hosts[0].Name != "core-sw-01" || hosts[0].IP != "192.0.2.1" {
		t.Errorf("host[0] = %+v", hosts[0])
	}
	if hosts[1].Proxy != "proxy2" {
		t.Errorf("host[1].proxy = %q, want proxy2", hosts[1].Proxy)
	}
}

func TestParseHostsCSV_Empty(t *testing.T) {
	hosts, err := parseHostsCSV([]byte(`name,ip`))
	if err != nil {
		t.Fatalf("parseHostsCSV: %v", err)
	}
	if len(hosts) != 0 {
		t.Errorf("expected 0 hosts for header-only CSV, got %d", len(hosts))
	}
}
