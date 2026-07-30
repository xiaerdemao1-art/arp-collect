package server

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/xiaerdemao1-art/arp-collect/internal/storage"

	"go.uber.org/zap"
)

// handleHosts GET 列出 / POST 新建
func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		proxyFilter := r.URL.Query().Get("proxy")
		hosts, err := s.pgStore.ListHosts(r.Context(), proxyFilter)
		if err != nil {
			s.logger.Error("列出主机失败", zap.Error(err))
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{"hosts": hosts, "total": len(hosts)})

	case http.MethodPost:
		var h storage.Host
		if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if h.Name == "" || h.IP == "" {
			http.Error(w, "name and ip are required", http.StatusBadRequest)
			return
		}
		created, err := s.pgStore.CreateHost(r.Context(), &h)
		if err != nil {
			s.logger.Error("新建主机失败", zap.Error(err))
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, created)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleHostByID PUT 更新 / DELETE 删除
func (s *Server) handleHostByID(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPut:
		var h storage.Host
		if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if h.Name == "" || h.IP == "" {
			http.Error(w, "name and ip are required", http.StatusBadRequest)
			return
		}
		updated, err := s.pgStore.UpdateHost(r.Context(), id, &h)
		if err != nil {
			s.logger.Error("更新主机失败", zap.Error(err))
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, updated)

	case http.MethodDelete:
		if err := s.pgStore.DeleteHost(r.Context(), id); err != nil {
			s.logger.Error("删除主机失败", zap.Error(err))
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]interface{}{"deleted": id})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleImportHosts POST /api/v1/hosts/import - CSV 批量导入
// 请求体为 CSV 文本（Content-Type: text/csv），表头：name,ip,role,site,region,proxy,snmp_community
func (s *Server) handleImportHosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "读取请求体失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	hosts, err := parseHostsCSV(body)
	if err != nil {
		http.Error(w, "解析 CSV 失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	count, err := s.pgStore.UpsertHosts(r.Context(), hosts)
	if err != nil {
		s.logger.Error("批量导入主机失败", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.logger.Info("批量导入主机", zap.Int("count", count))
	// 导入后立即刷新设备缓存，让新一轮采集用到最新清单
	if _, err := s.deviceSource.FetchDevices(r.Context(), s.cfg.NetBox.Roles); err != nil {
		s.logger.Warn("导入后刷新设备缓存失败", zap.Error(err))
	}
	writeJSON(w, map[string]interface{}{"imported": count})
}

// handleExportHosts GET /api/v1/hosts/export - 导出 CSV
func (s *Server) handleExportHosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hosts, err := s.pgStore.ListHosts(r.Context(), "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=hosts.csv")
	cw := csv.NewWriter(w)
	cw.Write([]string{"name", "ip", "role", "site", "region", "proxy", "snmp_community"})
	for _, h := range hosts {
		cw.Write([]string{h.Name, h.IP, h.Role, h.Site, h.Region, h.Proxy, h.SNMPCommunity})
	}
	cw.Flush()
}

// parseHostsCSV 解析 CSV 为 Host 列表（表头：name,ip,role,site,region,proxy,snmp_community）
func parseHostsCSV(data []byte) ([]storage.Host, error) {
	r := csv.NewReader(strings.NewReader(string(data)))
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, nil
	}

	header := records[0]
	idx := make(map[string]int, len(header))
	for i, h := range header {
		idx[strings.TrimSpace(strings.ToLower(h))] = i
	}
	get := func(row []string, name string) string {
		if i, ok := idx[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}

	hosts := make([]storage.Host, 0, len(records)-1)
	for _, row := range records[1:] {
		if len(row) == 0 || strings.TrimSpace(row[0]) == "" {
			continue
		}
		hosts = append(hosts, storage.Host{
			Name:          get(row, "name"),
			IP:            get(row, "ip"),
			Role:          get(row, "role"),
			Site:          get(row, "site"),
			Region:        get(row, "region"),
			Proxy:         get(row, "proxy"),
			SNMPCommunity: get(row, "snmp_community"),
		})
	}
	return hosts, nil
}

// writeJSON 写 JSON 响应
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
