package server

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xiaerdemao1-art/arp-collect/internal/config"
	"github.com/xiaerdemao1-art/arp-collect/internal/kafka"
	"github.com/xiaerdemao1-art/arp-collect/internal/metrics"
	"github.com/xiaerdemao1-art/arp-collect/internal/model"
	"github.com/xiaerdemao1-art/arp-collect/internal/snmp"
	"github.com/xiaerdemao1-art/arp-collect/internal/source"
	"github.com/xiaerdemao1-art/arp-collect/internal/storage"

	"go.uber.org/zap"
)

//go:embed web/dist/*
var webFS embed.FS

// Server ARP 采集服务端
type Server struct {
	cfg           *config.Config
	logger        *zap.Logger
	deviceSource  source.DeviceSource
	pgStore       *storage.PostgresStore
	kafkaProducer *kafka.Producer
	snmpCollector *snmp.Collector // Web 端 SNMP 测试用

	// 指标推送
	metricsPusher *metrics.VictoriaMetricsPusher

	// 设备缓存
	mu      sync.RWMutex
	devices []model.Device

	// HTTP 服务
	httpServer *http.Server
}

// NewServer 创建服务端
func NewServer(cfg *config.Config, logger *zap.Logger) (*Server, error) {
	s := &Server{
		cfg:    cfg,
		logger: logger,
	}

	// 初始化 PostgreSQL（先于设备源，database 源需要它）
	pgStore, err := storage.NewPostgresStore(&cfg.Postgres, logger)
	if err != nil {
		return nil, fmt.Errorf("连接 PostgreSQL 失败: %w", err)
	}
	s.pgStore = pgStore

	// 初始化设备源（netbox / file / database）
	ds, err := source.New(cfg, pgStore, logger)
	if err != nil {
		return nil, fmt.Errorf("初始化设备源失败: %w", err)
	}
	s.deviceSource = ds

	// SNMP 采集器（Web 端 SNMP 测试用）
	s.snmpCollector = snmp.NewCollector(&cfg.SNMP, logger)

	// 初始化 Kafka（可选）
	if cfg.Kafka.Enabled {
		producer, err := kafka.NewProducer(&cfg.Kafka, logger)
		if err != nil {
			logger.Warn("Kafka 初始化失败，将跳过 Kafka 推送", zap.Error(err))
		} else {
			s.kafkaProducer = producer
		}
	}

	// 初始化 VictoriaMetrics 推送器（可选）
	if cfg.VictoriaMetrics.Enabled {
		s.metricsPusher = metrics.NewVictoriaMetricsPusher(&cfg.VictoriaMetrics, logger)
		logger.Info("VictoriaMetrics 推送已启用", zap.String("url", cfg.VictoriaMetrics.URL))
	}

	return s, nil
}

// Close 关闭服务
func (s *Server) Close() {
	if s.metricsPusher != nil {
		s.metricsPusher.Stop()
	}
	if s.pgStore != nil {
		s.pgStore.Close()
	}
	if s.kafkaProducer != nil {
		s.kafkaProducer.Close()
	}
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpServer.Shutdown(ctx)
	}
}

// Start 启动服务
func (s *Server) Start(ctx context.Context) error {
	// 首次同步设备
	if err := s.refreshDevices(ctx); err != nil {
		return fmt.Errorf("首次同步设备失败: %w", err)
	}

	// 启动 HTTP API
	go s.startHTTPServer()

	// 启动设备刷新定时器
	go s.deviceRefreshLoop(ctx)

	// 启动 VictoriaMetrics 指标推送
	if s.metricsPusher != nil {
		go s.metricsPusher.Start(ctx)
	}

	// 记录慢速设备配置
	if len(s.cfg.Collector.SlowDevices) > 0 {
		for _, sd := range s.cfg.Collector.SlowDevices {
			s.logger.Info("慢速设备配置",
				zap.String("ip", sd.IP),
				zap.String("name", sd.Name),
				zap.Duration("interval", sd.Interval))
		}
	}

	s.logger.Info("Server 启动完成",
		zap.String("listen", s.cfg.Server.Listen),
		zap.Int("device_count", len(s.devices)),
		zap.Int("slow_device_count", len(s.cfg.Collector.SlowDevices)))

	<-ctx.Done()
	return nil
}

// refreshDevices 刷新设备列表
func (s *Server) refreshDevices(ctx context.Context) error {
	devices, err := s.deviceSource.FetchDevices(ctx, s.cfg.NetBox.Roles)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.devices = devices
	s.mu.Unlock()

	s.logger.Info("设备列表已刷新", zap.Int("count", len(devices)))
	return nil
}

// deviceRefreshLoop 定期刷新设备
func (s *Server) deviceRefreshLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.Collector.DeviceRefresh)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := s.refreshDevices(ctx); err != nil {
				s.logger.Error("刷新设备失败", zap.Error(err))
			}
		case <-ctx.Done():
			return
		}
	}
}

// startHTTPServer 启动 HTTP API 服务
func (s *Server) startHTTPServer() {
	mux := http.NewServeMux()

	// API 路由（需要 Token 认证）
	mux.HandleFunc("/api/v1/tasks", s.authMiddleware(s.handleGetTasks))      // Collector 获取任务
	mux.HandleFunc("/api/v1/results", s.authMiddleware(s.handlePostResults)) // Collector 提交结果
	mux.HandleFunc("/api/v1/devices", s.handleGetDevices)                    // 查询所有设备（内部用）
	mux.HandleFunc("/api/v1/arp", s.handleQueryARP)                          // 查询 ARP
	mux.HandleFunc("/api/v1/arp/search", s.handleARPSearch)                  // ARP 搜索 API
	mux.HandleFunc("/api/v1/stats", s.handleGetStats)                        // 统计信息
	mux.HandleFunc("/health", s.handleHealth)                                // 健康检查

	// 主机管理（Web 主机管理页使用，BasicAuth 鉴权）
	mux.HandleFunc("GET /api/v1/hosts", s.basicAuthMiddleware(s.handleHosts))
	mux.HandleFunc("POST /api/v1/hosts", s.basicAuthMiddleware(s.handleHosts))
	mux.HandleFunc("PUT /api/v1/hosts/{id}", s.basicAuthMiddleware(s.handleHostByID))
	mux.HandleFunc("DELETE /api/v1/hosts/{id}", s.basicAuthMiddleware(s.handleHostByID))
	mux.HandleFunc("POST /api/v1/hosts/import", s.basicAuthMiddleware(s.handleImportHosts))
	mux.HandleFunc("GET /api/v1/hosts/export", s.basicAuthMiddleware(s.handleExportHosts))
	mux.HandleFunc("POST /api/v1/snmp/test", s.basicAuthMiddleware(s.handleSNMPTest))

	// React SPA 静态资源
	distFS, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		s.logger.Fatal("无法加载静态资源", zap.Error(err))
	}
	fileServer := http.FileServer(http.FS(distFS))
	// 处理所有非 API 请求（SPA fallback）
	mux.HandleFunc("/", s.basicAuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		// 静态资源直接返回
		if strings.HasPrefix(r.URL.Path, "/assets/") || r.URL.Path == "/vite.svg" {
			fileServer.ServeHTTP(w, r)
			return
		}
		// 其他路由返回 index.html（SPA）
		content, err := webFS.ReadFile("web/dist/index.html")
		if err != nil {
			http.Error(w, "页面加载失败", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(content)
	}))

	s.httpServer = &http.Server{
		Addr:    s.cfg.Server.Listen,
		Handler: mux,
		// 大型 Proxy 可能提交 10MB+ 数据（10w+ ARP 条目）
		// ReadTimeout: 读取请求体的超时时间
		// WriteTimeout: 写入响应的超时时间（包括处理时间）
		ReadTimeout:  5 * time.Minute,
		WriteTimeout: 10 * time.Minute,
		IdleTimeout:  2 * time.Minute,
	}

	s.logger.Info("HTTP API 启动", zap.String("listen", s.cfg.Server.Listen))
	if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
		s.logger.Error("HTTP 服务异常退出", zap.Error(err))
	}
}

// basicAuthMiddleware Web UI 基础认证中间件（账号密码走 server.web_auth 配置）
func (s *Server) basicAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	webAuth := s.cfg.Server.WebAuth
	if !webAuth.Enabled {
		return next
	}
	expectedUser := []byte(webAuth.Username)
	expectedPass := []byte(webAuth.Password)
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		userMatch := subtle.ConstantTimeCompare([]byte(user), expectedUser) == 1
		passMatch := subtle.ConstantTimeCompare([]byte(pass), expectedPass) == 1
		if !ok || !userMatch || !passMatch {
			w.Header().Set("WWW-Authenticate", `Basic realm="ARP Collect Dashboard"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// authMiddleware 认证中间件（校验 Token 和 Proxy IP）
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. 校验 Token（如果配置了）
		if s.cfg.Server.AuthToken != "" {
			token := r.Header.Get("X-Auth-Token")
			if token != s.cfg.Server.AuthToken {
				s.logger.Warn("Token 认证失败",
					zap.String("remote_addr", r.RemoteAddr),
					zap.String("provided_token", token[:min(len(token), 8)]+"..."))
				http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
				return
			}
		}

		// 2. 校验 Proxy（类似 Zabbix 的 hostname+IP 校验）
		proxyName := r.URL.Query().Get("proxy")
		if proxyName == "" {
			proxyName = r.Header.Get("X-Proxy-Name")
		}

		if proxyName != "" && len(s.cfg.Server.AllowedProxies) > 0 {
			clientIP := getClientIP(r)
			if !s.isProxyAllowed(proxyName, clientIP) {
				s.logger.Warn("Proxy 校验失败",
					zap.String("proxy_name", proxyName),
					zap.String("client_ip", clientIP),
					zap.String("remote_addr", r.RemoteAddr))
				http.Error(w, "Forbidden: proxy not allowed or IP mismatch", http.StatusForbidden)
				return
			}
		}

		next(w, r)
	}
}

// isProxyAllowed 检查 Proxy 是否在白名单中且 IP 匹配
func (s *Server) isProxyAllowed(proxyName, clientIP string) bool {
	for _, p := range s.cfg.Server.AllowedProxies {
		if p.Name == proxyName {
			// 如果 IP 配置为空，只检查名称
			if p.IP == "" {
				return true
			}
			// 检查 IP 是否匹配
			return p.IP == clientIP
		}
	}
	return false
}

// getClientIP 获取客户端真实 IP
func getClientIP(r *http.Request) string {
	// 优先使用 X-Forwarded-For
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	// 使用 X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// 从 RemoteAddr 提取 IP
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// handleGetTasks Collector 获取任务（设备列表）
// GET /api/v1/tasks?proxy=proxy1
func (s *Server) handleGetTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	proxyName := r.URL.Query().Get("proxy")
	if proxyName == "" {
		http.Error(w, "missing proxy parameter", http.StatusBadRequest)
		return
	}

	// 构建慢速设备 IP 映射
	slowDeviceMap := make(map[string]int64)
	for _, sd := range s.cfg.Collector.SlowDevices {
		slowDeviceMap[sd.IP] = int64(sd.Interval.Seconds())
	}

	s.mu.RLock()
	var tasks []model.Device
	for _, d := range s.devices {
		if d.ProxyName == proxyName {
			// 检查是否是慢速设备
			if interval, ok := slowDeviceMap[d.PrimaryIP]; ok {
				d.CollectInterval = interval
			}
			tasks = append(tasks, d)
		}
	}
	s.mu.RUnlock()

	resp := TaskResponse{
		ProxyName:   proxyName,
		Devices:     tasks,
		DeviceCount: len(tasks),
		Timestamp:   time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)

	s.logger.Debug("任务下发",
		zap.String("proxy", proxyName),
		zap.Int("count", len(tasks)))
}

// handlePostResults Collector 提交采集结果
// POST /api/v1/results
func (s *Server) handlePostResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var results []model.CollectResult
	if err := json.NewDecoder(r.Body).Decode(&results); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 使用独立的 context，避免 HTTP 请求超时导致后台任务被取消
	// 设置 5 分钟超时，足够处理大型数据集
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 保存到 PostgreSQL（同步，必须成功；失败时返回 500 让 Collector 感知）
	if err := s.pgStore.SaveResults(ctx, results); err != nil {
		s.logger.Error("保存结果到 PG 失败", zap.Error(err))
		http.Error(w, "failed to save results: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 推送到 Kafka（异步，不阻塞响应）
	if s.kafkaProducer != nil {
		go func(results []model.CollectResult) {
			kafkaCtx, kafkaCancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer kafkaCancel()
			if err := s.sendKafkaResults(kafkaCtx, results); err != nil {
				s.logger.Error("推送结果到 Kafka 失败", zap.Error(err))
			}
		}(results)
	}

	// 事件触发式推送指标到 VictoriaMetrics（异步）
	if s.metricsPusher != nil {
		go s.metricsPusher.PushResults(context.Background(), results)
	}

	// 统计
	successCount := 0
	totalARP := 0
	for _, r := range results {
		if r.Status == "success" {
			successCount++
			totalARP += r.ARPCount
		}
	}

	s.logger.Info("收到采集结果",
		zap.Int("total", len(results)),
		zap.Int("success", successCount),
		zap.Int("total_arp", totalARP))

	resp := ResultResponse{
		Received:  len(results),
		Processed: successCount,
		Timestamp: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleGetDevices 查询所有设备（带状态信息）
// GET /api/v1/devices?proxy=xxx&role=xxx&status=xxx
func (s *Server) handleGetDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	proxyFilter := r.URL.Query().Get("proxy")
	roleFilter := r.URL.Query().Get("role")
	statusFilter := r.URL.Query().Get("status")

	// 从数据库获取设备状态
	ctx := r.Context()
	deviceStatuses, err := s.pgStore.GetDeviceStatuses(ctx)
	if err != nil {
		s.logger.Warn("获取设备状态失败", zap.Error(err))
		deviceStatuses = make(map[string]storage.DeviceStatus)
	}

	// 构造带状态的设备响应
	type DeviceWithStatus struct {
		model.Device
		Status       string  `json:"status"`
		ARPCount     int     `json:"arp_count"`
		DurationMs   int     `json:"duration_ms"`
		ErrorMessage string  `json:"error_message,omitempty"`
		CollectedAt  *string `json:"collected_at,omitempty"`
	}

	s.mu.RLock()
	var devices []DeviceWithStatus
	for _, d := range s.devices {
		// 状态筛选
		status := "pending"
		arpCount := 0
		durationMs := 0
		errorMsg := ""
		var collectedAt *string
		if st, ok := deviceStatuses[d.Name]; ok {
			status = st.Status
			arpCount = st.ARPCount
			durationMs = st.DurationMs
			errorMsg = st.ErrorMessage
			if !st.CollectedAt.IsZero() {
				t := st.CollectedAt.Format(time.RFC3339)
				collectedAt = &t
			}
		}

		// 应用筛选
		if proxyFilter != "" && d.ProxyName != proxyFilter {
			continue
		}
		if roleFilter != "" && d.Role != roleFilter {
			continue
		}
		if statusFilter != "" && status != statusFilter {
			continue
		}

		devices = append(devices, DeviceWithStatus{
			Device:       d,
			Status:       status,
			ARPCount:     arpCount,
			DurationMs:   durationMs,
			ErrorMessage: errorMsg,
			CollectedAt:  collectedAt,
		})
	}
	s.mu.RUnlock()

	// 返回包装格式，便于前端解析
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"devices": devices,
		"total":   len(devices),
	})
}

// handleQueryARP 查询 ARP
// GET /api/v1/arp?device=xxx&mac=xxx&ip=xxx&limit=100
func (s *Server) handleQueryARP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	device := r.URL.Query().Get("device")
	mac := r.URL.Query().Get("mac")
	ip := r.URL.Query().Get("ip")
	limitStr := r.URL.Query().Get("limit")

	// 解析 limit
	limit := 500
	if limitStr != "" {
		if l, err := parseInt(limitStr); err == nil && l > 0 && l <= 100000 {
			limit = l
		}
	}

	// 至少需要一个查询条件
	if device == "" && mac == "" && ip == "" {
		http.Error(w, "missing search parameter (device, mac, or ip)", http.StatusBadRequest)
		return
	}

	// 构建查询条件
	filters := make(map[string]string)
	if device != "" {
		filters["device"] = device
	}
	if mac != "" {
		filters["mac"] = mac
	}
	if ip != "" {
		filters["ip"] = ip
	}

	// 执行查询
	results, err := s.pgStore.QueryARP(ctx, filters, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// parseInt 解析整数
func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

// isIPPrefix 判断是否为 IP 前缀（如 192.168.1 或 10.0）
func isIPPrefix(s string) bool {
	// 必须只包含数字和点
	for _, c := range s {
		if c != '.' && (c < '0' || c > '9') {
			return false
		}
	}
	// 必须以数字开头
	if len(s) == 0 || s[0] < '0' || s[0] > '9' {
		return false
	}
	// 检查每个部分是否为有效数字 (0-255)
	parts := strings.Split(s, ".")
	if len(parts) < 1 || len(parts) > 4 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		n, err := parseInt(part)
		if err != nil || n < 0 || n > 255 {
			return false
		}
	}
	return true
}

// handleGetStats 获取统计信息
// GET /api/v1/stats?proxy=xxx
func (s *Server) handleGetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	proxyName := r.URL.Query().Get("proxy")
	stats, err := s.pgStore.GetSnapshotStats(r.Context(), proxyName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 添加设备分布
	s.mu.RLock()
	proxyStats := make(map[string]int)
	for _, d := range s.devices {
		proxyStats[d.ProxyName]++
	}
	s.mu.RUnlock()
	stats["device_distribution"] = proxyStats

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleHealth 健康检查
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now(),
	})
}

// TaskResponse 任务响应
type TaskResponse struct {
	ProxyName   string         `json:"proxy_name"`
	Devices     []model.Device `json:"devices"`
	DeviceCount int            `json:"device_count"`
	Timestamp   time.Time      `json:"timestamp"`
}

// ResultResponse 结果响应
type ResultResponse struct {
	Received  int       `json:"received"`
	Processed int       `json:"processed"`
	Timestamp time.Time `json:"timestamp"`
}

// sendKafkaResults 发送 Kafka 结果
func (s *Server) sendKafkaResults(ctx context.Context, results []model.CollectResult) error {
	for _, result := range results {
		if err := s.kafkaProducer.SendARPRecords(ctx, result); err != nil {
			s.logger.Error("发送 ARP 记录到 Kafka 失败",
				zap.String("device", result.DeviceName),
				zap.Error(err))
		}
	}
	return nil
}

// handleARPSearch ARP 搜索 API（用于前端搜索页面）
// GET /api/v1/arp/search?q=xxx
func (s *Server) handleARPSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))

	// 需要查询条件
	if query == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "请输入搜索条件",
			"records": []interface{}{},
		})
		return
	}

	ctx := r.Context()

	// 构建查询条件 - 智能识别搜索类型
	filters := make(map[string]string)
	queryLower := strings.ToLower(query)

	// 判断搜索类型
	if strings.Contains(query, ":") {
		// MAC 地址（包含冒号）
		filters["mac"] = strings.ToLower(strings.ReplaceAll(query, "-", ":"))
	} else if strings.Count(query, "-") >= 5 {
		// MAC 地址（使用横线分隔，如 aa-bb-cc-dd-ee-ff）
		filters["mac"] = strings.ToLower(strings.ReplaceAll(query, "-", ":"))
	} else if strings.Contains(queryLower, "proxy") {
		// Proxy 名称（启发式：包含 "proxy" 字样的视为节点名）
		filters["proxy"] = query
	} else if net.ParseIP(query) != nil {
		// 完整 IP 地址
		filters["ip"] = query
	} else if isIPPrefix(query) {
		// IP 前缀（如 192.168.1）
		filters["ip"] = query
	} else {
		// 设备名称（默认）
		filters["device"] = query
	}

	// 执行查询
	results, err := s.pgStore.QueryARP(ctx, filters, 100000)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   err.Error(),
			"records": []interface{}{},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"records": results,
		"count":   len(results),
	})
}
