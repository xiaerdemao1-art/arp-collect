package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xiaerdemao1-art/arp-collect/internal/config"
	"github.com/xiaerdemao1-art/arp-collect/internal/model"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// PostgresStore PostgreSQL 存储
type PostgresStore struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

// NewPostgresStore 创建 PostgreSQL 存储
func NewPostgresStore(cfg *config.PostgresConfig, logger *zap.Logger) (*PostgresStore, error) {
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?pool_max_conns=%d",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database, cfg.MaxConnections)

	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("解析连接字符串失败: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		return nil, fmt.Errorf("创建连接池失败: %w", err)
	}

	// 测试连接
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	store := &PostgresStore{
		pool:   pool,
		logger: logger,
	}

	// 初始化表结构
	if err := store.initSchema(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("初始化表结构失败: %w", err)
	}

	logger.Info("PostgreSQL 连接成功", zap.String("host", cfg.Host), zap.Int("port", cfg.Port))
	return store, nil
}

// Close 关闭连接
func (s *PostgresStore) Close() {
	s.pool.Close()
}

// initSchema 初始化表结构（与 Vector/Zabbix 标签对齐）
func (s *PostgresStore) initSchema(ctx context.Context) error {
	schema := `
-- 设备快照元信息（与 NetBox 元数据对齐）
CREATE TABLE IF NOT EXISTS arp_snapshot_current (
device_id       INTEGER,
device_name     TEXT PRIMARY KEY,
mgmt_ip         TEXT NOT NULL,
role            TEXT,
manufacturer    TEXT,
site            TEXT,
contact_name    TEXT,
contact_email   TEXT,
region_level_1  TEXT,
region_level_2  TEXT,
region_level_3  TEXT,
proxy_name      TEXT,
arp_count       INTEGER DEFAULT 0,
status          TEXT DEFAULT 'pending',
duration_ms     INTEGER,
error_message   TEXT,
created_at      TIMESTAMPTZ DEFAULT NOW(),
collected_at    TIMESTAMPTZ DEFAULT NOW(),
updated_at      TIMESTAMPTZ DEFAULT NOW()
);

-- ARP 明细（当前快照）
-- 主键：(device_name, ip, mac) - 三元组唯一标识一个网络终端
-- created_at: 首次发现该三元组的时间
-- collected_at: 最后一次采集到该三元组的时间
-- 优势：
--   1. 可检测 IP 冲突（同一 device_name + ip 对应多个 mac）
--   2. 可追踪终端迁移（同一 mac 在不同 device_name 出现）
--   3. 可记录多路径（同一终端在堆叠/VRRP 环境下被多个网关看到）
CREATE TABLE IF NOT EXISTS arp_entry_current (
device_name     TEXT NOT NULL,
ip              TEXT NOT NULL,
mac             TEXT NOT NULL,
interface       TEXT NOT NULL,
type            TEXT DEFAULT 'dynamic',
created_at      TIMESTAMPTZ DEFAULT NOW(),
collected_at    TIMESTAMPTZ DEFAULT NOW(),
PRIMARY KEY (device_name, ip, mac)
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_arp_entry_ip ON arp_entry_current(ip);  -- IP 冲突检测
CREATE INDEX IF NOT EXISTS idx_arp_entry_mac ON arp_entry_current(mac);  -- MAC 追踪
CREATE INDEX IF NOT EXISTS idx_arp_entry_collected ON arp_entry_current(collected_at);
CREATE INDEX IF NOT EXISTS idx_arp_entry_created ON arp_entry_current(created_at);
CREATE INDEX IF NOT EXISTS idx_arp_snapshot_status ON arp_snapshot_current(status);
CREATE INDEX IF NOT EXISTS idx_arp_snapshot_proxy ON arp_snapshot_current(proxy_name);
CREATE INDEX IF NOT EXISTS idx_arp_snapshot_region ON arp_snapshot_current(region_level_1, region_level_2);

-- 添加 created_at 列（如果不存在）
DO $$ 
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name = 'arp_entry_current' AND column_name = 'created_at') THEN
        ALTER TABLE arp_entry_current ADD COLUMN created_at TIMESTAMPTZ DEFAULT NOW();
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name = 'arp_snapshot_current' AND column_name = 'created_at') THEN
        ALTER TABLE arp_snapshot_current ADD COLUMN created_at TIMESTAMPTZ DEFAULT NOW();
    END IF;
END $$;
`

	_, err := s.pool.Exec(ctx, schema)
	return err
}

// SaveResult 保存采集结果
func (s *PostgresStore) SaveResult(ctx context.Context, result model.CollectResult) error {
	start := time.Now()

	// 使用事务
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Upsert 设备快照
	snapshotSQL := `
INSERT INTO arp_snapshot_current 
(device_id, device_name, mgmt_ip, role, manufacturer, site,
 contact_name, contact_email, region_level_1, region_level_2, region_level_3,
 proxy_name, arp_count, status, duration_ms, error_message, collected_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, NOW())
ON CONFLICT (device_name) DO UPDATE SET
device_id = EXCLUDED.device_id,
mgmt_ip = EXCLUDED.mgmt_ip,
role = EXCLUDED.role,
manufacturer = EXCLUDED.manufacturer,
site = EXCLUDED.site,
contact_name = EXCLUDED.contact_name,
contact_email = EXCLUDED.contact_email,
region_level_1 = EXCLUDED.region_level_1,
region_level_2 = EXCLUDED.region_level_2,
region_level_3 = EXCLUDED.region_level_3,
proxy_name = EXCLUDED.proxy_name,
arp_count = EXCLUDED.arp_count,
status = EXCLUDED.status,
duration_ms = EXCLUDED.duration_ms,
error_message = EXCLUDED.error_message,
collected_at = EXCLUDED.collected_at,
updated_at = NOW()
`

	_, err = tx.Exec(ctx, snapshotSQL,
		result.DeviceID, result.DeviceName, result.MgmtIP, result.Role, result.Manufacturer, result.Site,
		result.ContactName, result.ContactEmail, result.RegionLevel1, result.RegionLevel2, result.RegionLevel3,
		result.ProxyName, result.ARPCount, result.Status, result.DurationMs, result.Error,
		result.CollectedAt)
	if err != nil {
		return fmt.Errorf("保存设备快照失败: %w", err)
	}

	// 2. 使用 UPSERT 更新 ARP 条目（保留 created_at）
	if len(result.Entries) > 0 {
		if err := s.upsertEntries(ctx, tx, result.DeviceName, result.Entries, result.CollectedAt); err != nil {
			return fmt.Errorf("更新 ARP 条目失败: %w", err)
		}
	}

	// 提交事务
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	s.logger.Debug("保存采集结果完成",
		zap.String("device", result.DeviceName),
		zap.Int("arp_count", result.ARPCount),
		zap.Duration("db_duration", time.Since(start)))

	return nil
}

// upsertEntries 批量 UPSERT ARP 条目（保留 created_at，更新 collected_at）
// 主键变更为 (device_name, ip, mac) 后，只有三元组完全相同时才会触发 UPDATE
// 这意味着：
//   - 同一 IP 换了 MAC -> 新增记录（可检测 IP 冲突）
//   - 同一 MAC 换了 IP -> 新增记录（可追踪终端 IP 变更）
//   - 同一 MAC 在不同网关出现 -> 新增记录（可追踪终端漂移）
func (s *PostgresStore) upsertEntries(ctx context.Context, tx pgx.Tx, deviceName string, entries []model.ARPEntry, collectedAt time.Time) error {
	// 使用批量 UPSERT
	upsertSQL := `
INSERT INTO arp_entry_current (device_name, ip, mac, interface, type, created_at, collected_at)
VALUES ($1, $2, $3, $4, $5, $6, $6)
ON CONFLICT (device_name, ip, mac) DO UPDATE SET
type = EXCLUDED.type,
collected_at = EXCLUDED.collected_at
`

	batch := &pgx.Batch{}
	for _, e := range entries {
		batch.Queue(upsertSQL, deviceName, e.IP, e.MAC, e.Interface, e.Type, collectedAt)
	}

	results := tx.SendBatch(ctx, batch)
	defer results.Close()

	for range entries {
		if _, err := results.Exec(); err != nil {
			return err
		}
	}

	return nil
}

// SaveResults 批量保存多个采集结果
// 单个设备失败不阻塞其它设备，但会聚合成错误返回，调用方应据此决定响应状态
func (s *PostgresStore) SaveResults(ctx context.Context, results []model.CollectResult) error {
	var failed []string
	for _, result := range results {
		if err := s.SaveResult(ctx, result); err != nil {
			s.logger.Error("保存结果失败",
				zap.String("device", result.DeviceName),
				zap.Error(err))
			failed = append(failed, fmt.Sprintf("%s: %v", result.DeviceName, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d/%d 台设备保存失败: %s", len(failed), len(results), strings.Join(failed, "; "))
	}
	return nil
}

// GetSnapshotStats 获取快照统计信息
func (s *PostgresStore) GetSnapshotStats(ctx context.Context, proxyName string) (map[string]interface{}, error) {
	query := `
SELECT 
COUNT(*) as total_devices,
COUNT(*) FILTER (WHERE status = 'success') as success_count,
COUNT(*) FILTER (WHERE status = 'fail') as fail_count,
COALESCE(SUM(arp_count), 0) as total_arp_count,
COALESCE(AVG(duration_ms), 0) as avg_duration_ms
FROM arp_snapshot_current
WHERE ($1 = '' OR proxy_name = $1)
`

	var totalDevices, successCount, failCount int
	var totalARPCount int64
	var avgDurationMs float64

	err := s.pool.QueryRow(ctx, query, proxyName).Scan(
		&totalDevices, &successCount, &failCount, &totalARPCount, &avgDurationMs)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total_devices":   totalDevices,
		"success_count":   successCount,
		"fail_count":      failCount,
		"total_arp_count": totalARPCount,
		"avg_duration_ms": avgDurationMs,
	}, nil
}

// QueryARP 查询 ARP 条目
func (s *PostgresStore) QueryARP(ctx context.Context, filters map[string]string, limit int) ([]map[string]interface{}, error) {
	var conditions []string
	var args []interface{}
	argIdx := 1

	if device, ok := filters["device"]; ok && device != "" {
		conditions = append(conditions, fmt.Sprintf("e.device_name ILIKE $%d", argIdx))
		args = append(args, "%"+device+"%")
		argIdx++
	}
	if ip, ok := filters["ip"]; ok && ip != "" {
		// IP 支持前缀匹配
		conditions = append(conditions, fmt.Sprintf("e.ip LIKE $%d", argIdx))
		args = append(args, ip+"%")
		argIdx++
	}
	if mac, ok := filters["mac"]; ok && mac != "" {
		conditions = append(conditions, fmt.Sprintf("e.mac ILIKE $%d", argIdx))
		args = append(args, "%"+mac+"%")
		argIdx++
	}
	if proxy, ok := filters["proxy"]; ok && proxy != "" {
		conditions = append(conditions, fmt.Sprintf("s.proxy_name ILIKE $%d", argIdx))
		args = append(args, "%"+proxy+"%")
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	if limit <= 0 {
		limit = 10000
	}
	if limit > 200000 {
		limit = 200000
	}

	query := fmt.Sprintf(`
SELECT 
e.device_name, e.ip, e.mac, e.interface, e.type, e.collected_at, e.created_at,
s.mgmt_ip, s.site, s.region_level_1, s.region_level_2, s.proxy_name
FROM arp_entry_current e
LEFT JOIN arp_snapshot_current s ON e.device_name = s.device_name
%s
ORDER BY e.collected_at DESC
LIMIT %d
`, whereClause, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var deviceName, ip, mac, iface, entryType string
		var collectedAt, createdAt time.Time
		var mgmtIP, site, region1, region2, proxyName *string

		if err := rows.Scan(&deviceName, &ip, &mac, &iface, &entryType, &collectedAt, &createdAt,
			&mgmtIP, &site, &region1, &region2, &proxyName); err != nil {
			continue
		}

		result := map[string]interface{}{
			"device_name":  deviceName,
			"ip":           ip,
			"mac":          mac,
			"interface":    iface,
			"type":         entryType,
			"collected_at": collectedAt,
			"created_at":   createdAt,
		}
		if mgmtIP != nil {
			result["device_ip"] = *mgmtIP
		}
		if site != nil {
			result["site"] = *site
		}
		if region1 != nil {
			result["region_level_1"] = *region1
		}
		if region2 != nil {
			result["region_level_2"] = *region2
		}
		if proxyName != nil {
			result["proxy_name"] = *proxyName
		}

		results = append(results, result)
	}

	return results, nil
}

// DeviceStatus 设备状态信息
type DeviceStatus struct {
	Status       string    `json:"status"`
	ARPCount     int       `json:"arp_count"`
	DurationMs   int       `json:"duration_ms"`
	ErrorMessage string    `json:"error_message"`
	CollectedAt  time.Time `json:"collected_at"`
}

// GetDeviceStatuses 批量获取设备状态（从 arp_snapshot_current 表）
func (s *PostgresStore) GetDeviceStatuses(ctx context.Context) (map[string]DeviceStatus, error) {
	query := `
		SELECT device_name, status, COALESCE(arp_count, 0), COALESCE(duration_ms, 0), COALESCE(error_message, ''), collected_at
		FROM arp_snapshot_current
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]DeviceStatus)
	for rows.Next() {
		var deviceName, status, errorMsg string
		var arpCount, durationMs int
		var collectedAt time.Time
		if err := rows.Scan(&deviceName, &status, &arpCount, &durationMs, &errorMsg, &collectedAt); err != nil {
			continue
		}
		result[deviceName] = DeviceStatus{
			Status:       status,
			ARPCount:     arpCount,
			DurationMs:   durationMs,
			ErrorMessage: errorMsg,
			CollectedAt:  collectedAt,
		}
	}

	return result, rows.Err()
}
