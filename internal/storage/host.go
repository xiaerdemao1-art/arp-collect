package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Host 手动管理的设备（arp_device 表，Web 主机管理页增删改）
type Host struct {
	ID            int       `json:"id"`
	Name          string    `json:"name"`
	IP            string    `json:"ip"`
	Role          string    `json:"role"`
	Site          string    `json:"site"`
	Region        string    `json:"region"`
	Proxy         string    `json:"proxy"`
	SNMPCommunity string    `json:"snmp_community,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ListHosts 列出主机（可选按 proxy 过滤）
func (s *PostgresStore) ListHosts(ctx context.Context, proxyFilter string) ([]Host, error) {
	query := `SELECT id, name, ip, COALESCE(role,''), COALESCE(site,''), COALESCE(region,''),
	          COALESCE(proxy,''), COALESCE(snmp_community,''), created_at, updated_at
	          FROM arp_device`
	args := []interface{}{}
	if proxyFilter != "" {
		query += " WHERE proxy = $1"
		args = append(args, proxyFilter)
	}
	query += " ORDER BY name"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []Host
	for rows.Next() {
		var h Host
		if err := rows.Scan(&h.ID, &h.Name, &h.IP, &h.Role, &h.Site, &h.Region,
			&h.Proxy, &h.SNMPCommunity, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
	}
	return hosts, rows.Err()
}

// ListAllHosts 列出全部主机（database 设备源使用）
func (s *PostgresStore) ListAllHosts(ctx context.Context) ([]Host, error) {
	return s.ListHosts(ctx, "")
}

// CreateHost 新建主机，返回带 ID 的主机
func (s *PostgresStore) CreateHost(ctx context.Context, h *Host) (*Host, error) {
	query := `INSERT INTO arp_device (name, ip, role, site, region, proxy, snmp_community)
	          VALUES ($1, $2, $3, $4, $5, $6, $7)
	          RETURNING id, created_at, updated_at`
	err := s.pool.QueryRow(ctx, query,
		h.Name, h.IP, h.Role, h.Site, h.Region, h.Proxy, h.SNMPCommunity).
		Scan(&h.ID, &h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("新建主机失败: %w", err)
	}
	return h, nil
}

// UpdateHost 更新主机
func (s *PostgresStore) UpdateHost(ctx context.Context, id int, h *Host) (*Host, error) {
	query := `UPDATE arp_device SET name=$1, ip=$2, role=$3, site=$4, region=$5,
	          proxy=$6, snmp_community=$7, updated_at=NOW()
	          WHERE id=$8 RETURNING created_at, updated_at`
	err := s.pool.QueryRow(ctx, query,
		h.Name, h.IP, h.Role, h.Site, h.Region, h.Proxy, h.SNMPCommunity, id).
		Scan(&h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("更新主机失败: %w", err)
	}
	h.ID = id
	return h, nil
}

// DeleteHost 删除主机
func (s *PostgresStore) DeleteHost(ctx context.Context, id int) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM arp_device WHERE id=$1", id)
	if err != nil {
		return fmt.Errorf("删除主机失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("主机不存在 (id=%d)", id)
	}
	return nil
}

// UpsertHosts 批量导入主机（按 name 去重，存在则更新）
// 返回处理条数。跳过 name/ip 为空的行。
func (s *PostgresStore) UpsertHosts(ctx context.Context, hosts []Host) (int, error) {
	if len(hosts) == 0 {
		return 0, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback(ctx)

	upsertSQL := `INSERT INTO arp_device (name, ip, role, site, region, proxy, snmp_community)
	              VALUES ($1, $2, $3, $4, $5, $6, $7)
	              ON CONFLICT (name) DO UPDATE SET
	              ip=EXCLUDED.ip, role=EXCLUDED.role, site=EXCLUDED.site,
	              region=EXCLUDED.region, proxy=EXCLUDED.proxy,
	              snmp_community=EXCLUDED.snmp_community, updated_at=NOW()`

	batch := &pgx.Batch{}
	count := 0
	for _, h := range hosts {
		if h.Name == "" || h.IP == "" {
			continue
		}
		batch.Queue(upsertSQL, h.Name, h.IP, h.Role, h.Site, h.Region, h.Proxy, h.SNMPCommunity)
		count++
	}
	if count == 0 {
		return 0, nil
	}

	results := tx.SendBatch(ctx, batch)
	defer results.Close()
	for i := 0; i < count; i++ {
		if _, err := results.Exec(); err != nil {
			return i, fmt.Errorf("批量导入第 %d 条失败: %w", i+1, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("提交事务失败: %w", err)
	}
	return count, nil
}
