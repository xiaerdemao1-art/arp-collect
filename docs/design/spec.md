# arp-collect 设计规范（Design Spec）

> 本文是系统核心设计决策的存档，与代码同步维护。

## 1. 背景与目标

- **问题**：Zabbix Item 返回值有 64KB 上限，大型园区核心交换机的 ARP 表（万级~十万级条目）被截断，数据不完整；且新增设备需要手工配置 Zabbix Host，扩展性差。
- **目标**：
  1. 完整采集任意规模的 ARP 表
  2. 设备清单由 NetBox 驱动，自动上下线
  3. 分布式采集：任务按站点就近分发到各采集节点，降低跨地域 SNMP 延迟与失败率
  4. 数据可用于检索（Web UI）、告警（VictoriaMetrics）、下游分析（Kafka）

## 2. 总体架构

- **arp_server**：唯一中心节点。NetBox 设备同步（1h 周期）、按 `proxy_mappings` 规则把设备分配给各采集节点、HTTP API（任务下发 / 结果接收 / 查询）、PostgreSQL 落库、可选 Kafka / VictoriaMetrics 输出、内嵌 React Web UI。
- **arp_collector**：无状态采集代理。10 分钟周期从 Server 拉取本节点设备清单，SNMPv2c BulkWalk 采集，POST 结果回 Server。
- **standalone 模式**：collector 二进制直连 NetBox + PG，用于小规模部署和单设备调试。

## 3. 关键设计决策

### 3.1 存储：三元组主键 `(device_name, ip, mac)`

`arp_entry_current` 以三元组为唯一键（UPSERT，保留 `created_at` 首次发现时间）：

| 能力 | 说明 |
|---|---|
| IP 冲突检测 | 同一 `device_name + ip` 对应多个 mac → 并存记录，可查询 |
| 终端漂移追踪 | 同一 mac 出现在不同 device/ip → 各自独立记录 |
| 多路径可见性 | VRRP/堆叠环境同一终端被多个网关看到 → 完整保留 |
| 网卡更换 | 新旧 MAC 独立记录，形成历史 |

### 3.2 设备分配：配置化 proxy_mappings

设备 → 采集节点的映射完全由配置驱动（**数组顺序即优先级，先匹配先生效**）：

```
Site 子串匹配 > Region L2 匹配 > Region L1 匹配 > default_proxy
```

历史上该映射曾硬编码在代码中（且用 map 存储导致同优先级规则命中顺序随机），
开源版本改为配置文件的 `proxy_mappings` 数组，同时获得了确定性的优先级语义。

### 3.3 慢速设备调度

ARP 表巨大（万级以上）的设备采集耗时长，会拖累整个采集周期。
`collector.slow_devices` 允许为这类设备配置独立采集间隔（如 20m），
Server 在下发任务时通过 `collect_interval_sec` 告知 Collector，
Collector 端由 `SlowDeviceTracker`（`internal/collector/slowdevice.go`）
记录上次采集时间并跳过未到期设备。分布式与单机模式共用同一实现。

### 3.4 认证模型

- **Collector API**：预共享 Token（`X-Auth-Token` header）+ 可选 proxy 名称/IP 白名单（`server.allowed_proxies`，类似 Zabbix 的 Hostname+IP 校验）。
- **Web UI**：Basic Auth（`server.web_auth`），比较使用 `crypto/subtle.ConstantTimeCompare`。
- **秘密管理**：代码零默认凭据；所有秘密字段支持 `${ENV_VAR}` 展开或 `ARP_*` 环境变量覆盖；启动时 `Validate()` 校验必填项，缺失即拒绝启动。

### 3.5 结果处理管线

`POST /api/v1/results`：

1. **PostgreSQL 同步写入**（必须成功，失败返回 500 让 Collector 感知）
2. **Kafka 异步推送**（独立 background context，避免 HTTP 结束导致 context canceled）
3. **VictoriaMetrics 事件触发推送**（每次收到结果立即推送该批设备的指标）

### 3.6 SNMP 采集

- BulkWalk OID `1.3.6.1.2.1.4.22.1.2`（ipNetToMediaPhysAddress）+ `1.3.6.1.2.1.31.1.1.1.1`（ifName）
- 并发用 channel 信号量控制（`snmp.max_concurrency`）
- MAC 统一规范化为小写冒号格式 `xx:xx:xx:xx:xx:xx`

## 4. 数据库结构

见 `internal/storage/postgres.go` 的 `initSchema`：

- `arp_snapshot_current`（PK: device_name）— 设备元数据 + 最近采集状态
- `arp_entry_current`（PK: device_name+ip+mac）— ARP 明细
- 索引：ip、mac、collected_at、created_at、status、proxy_name、region

## 5. 指标

| 指标 | 类型 | 说明 |
|---|---|---|
| `arp_collect_device_arp_count` | gauge | 设备 ARP 条目数 |
| `arp_collect_device_duration_ms` | gauge | 采集耗时 |
| `arp_collect_device_status` | gauge | 1=success, 0=fail |

标签：device_name, device_ip, role, manufacturer, site, contact_name/email, region_level_1/2/3, proxy_name。

## 6. 已知限制

- 仅 SNMPv2c；Server API 仅 HTTP；Kafka TLS 目前跳过证书校验
- 测试覆盖有限（config 冒烟测试）
