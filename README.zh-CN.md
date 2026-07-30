# arp-collect

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.23%2B-00ADD8?logo=go)](go.mod)

[English](README.md)

**arp-collect** 是一个面向大型园区网的分布式 ARP 表采集系统。它的诞生是为了替代基于 Zabbix 的 ARP 监控方案——Zabbix Item 有 64KB 返回值上限，核心交换机上万级条目的 ARP 表会被静默截断。

系统从 [NetBox](https://github.com/netbox-community/netbox) 拉取设备清单，把采集任务分发给部署在网络设备旁的轻量采集器（`arp_collector`），通过 SNMPv2c BulkWalk 采集 ARP 表，统一存入 PostgreSQL，并可选输出到 Kafka 与 VictoriaMetrics，自带 React Web 查询界面。

## 架构

```
                        ┌─────────────────── NetBox（设备 CMDB）
                        │   roles: CoreSwitch / DistributionSwitch / ...
                        ▼
┌────────────────────────────────────────────────────────────┐
│  arp_server（中心服务端，监听 :19527）                       │
│  ├─ NetBox 同步（每小时），按 proxy_mappings 规则分配设备     │
│  ├─ HTTP API                                               │
│  │    GET  /api/v1/tasks     Collector 拉取任务              │
│  │    POST /api/v1/results   Collector 上报结果              │
│  │    GET  /api/v1/arp|/arp/search|/devices|/stats         │
│  │    GET  /health                                         │
│  ├─ React SPA Web UI（内嵌，Basic Auth 认证）                │
│  ├─ PostgreSQL（arp_snapshot_current + arp_entry_current） │
│  ├─ Kafka（可选）                                           │
│  └─ VictoriaMetrics（可选，事件触发推送）                     │
└────────────────────────────────────────────────────────────┘
          ▲ GET tasks / POST results（X-Auth-Token + 可选 IP 白名单）
          │
┌─────────┴───────────────────────────────────┐
│  arp_collector（每个采集节点一个）              │
│  对本地设备做 SNMPv2c BulkWalk：              │
│    ARP 表  OID 1.3.6.1.2.1.4.22.1.2         │
│    ifName  OID 1.3.6.1.2.1.31.1.1.1.1       │
└─────────────────────────────────────────────┘
```

三种运行模式：

| 模式 | 二进制 | 说明 |
|---|---|---|
| `server` | `arp_server` | 中心服务端：NetBox 同步、任务分发、存储、Web UI |
| `collector` | `arp_collector -mode collector` | 采集代理：从 Server 拉任务，SNMP 采集后回传 |
| `standalone` | `arp_collector -mode standalone` | 单机一体模式（小规模/测试）：直连 NetBox + PostgreSQL |

## 特性

- **支持单设备 10 万+ ARP 条目** —— 不再有 64KB 截断
- **NetBox 驱动设备清单** —— 设备上下线自动感知，无需逐台配置
- **规则化任务分发** —— 按站点/区域/角色把设备分配给采集器（`proxy_mappings`）
- **三元组主键** `(device_name, ip, mac)` —— 支持 IP 冲突检测、终端漂移追踪、多网关（VRRP/堆叠）场景完整记录；`created_at` 记录首次发现时间
- **慢速设备调度** —— ARP 表巨大的设备可配置独立采集周期
- **Web UI** —— 仪表盘 + 统一搜索（IP/MAC/设备名/Proxy 名智能识别），支持 CSV 导出
- **可选输出** —— Kafka（每 ARP 记录一条消息）、VictoriaMetrics 指标（`arp_collect_device_arp_count`、`arp_collect_device_duration_ms`、`arp_collect_device_status`）
- **静态编译** —— `CGO_ENABLED=0`，配合 systemd 极简部署

## 快速开始（standalone 单机模式）

前置条件：Go 1.23+、可访问的 NetBox、PostgreSQL、至少一台开启 SNMPv2c 的交换机。

```bash
# 1. 编译（无需 Node.js —— Web UI 已预构建并嵌入）
go build -o bin/arp_collector ./cmd/collector
go build -o bin/arp_server     ./cmd/server

# 2. 配置
cp config/config.example.yaml config/config.yaml
# 编辑 config/config.yaml 填入 netbox/snmp/postgres，或用环境变量：
export ARP_NETBOX_TOKEN=你的NetBox令牌
export ARP_POSTGRES_PASSWORD=你的PG密码
export ARP_SNMP_COMMUNITY=你的SNMP社区名

# 3. 先测试单台设备
./bin/arp_collector -config config/config.yaml -device 192.0.2.1

# 4. 单次全量采集
./bin/arp_collector -config config/config.yaml -once

# 5. 或启动中心服务端（含 :19527 Web UI）
./bin/arp_server -config config/config.yaml
```

打开 `http://localhost:19527`，用 `server.web_auth` 配置的账号密码登录。

> **安全说明**：本项目不内置任何默认凭据。秘密全部从配置文件或环境变量读取
> （所有秘密字段支持 `${ENV_VAR}` 展开），缺少必填凭据时服务会拒绝启动。

## 配置说明

见 [`config/config.example.yaml`](config/config.example.yaml)，每个选项都有中英双语注释。重点：

- `device_source` - device inventory source: `netbox` (default) or `file` (local yaml/csv list, no NetBox needed; see `config/devices.example.yaml`)
- `proxy_mappings` —— 有序规则，把 NetBox 站点/区域/角色映射到采集器（**先匹配先生效**），`default_proxy` 兜底
- `server.allowed_proxies` —— 可选的采集器白名单，支持绑定 IP（类似 Zabbix）
- `collector.slow_devices` —— 大 ARP 表设备的独立采集周期
- `server.web_auth` —— Web UI Basic Auth（仅在有其他认证层时才建议关闭）
- `kafka` / `victoria_metrics` —— 可选输出，默认禁用

任意配置项都可以用 `ARP_` 前缀的环境变量覆盖（如 `ARP_SERVER_AUTHTOKEN`）。

## 分布式部署

见 [docs/deployment.zh-CN.md](docs/deployment.zh-CN.md)（[English](docs/deployment.md)）。最快的方式是用一键安装脚本：

    sudo ./deploy/install.sh server      # 在 server 节点
    sudo ./deploy/install.sh collector --proxy <名称> --server-url http://<server>:19527

简要步骤：

1. 在中心节点运行 `arp_server`（需能访问 PostgreSQL）。
2. 在每个采集节点：拷贝静态二进制 + 配置，`systemctl enable --now arp-collector`。
3. `deploy/` 目录提供 systemd 模板、批量部署脚本（`deploy_all_collectors.sh`，由 `proxies.txt` 驱动）和配置生成器。

## API 概览

| 端点 | 认证 | 说明 |
|---|---|---|
| `GET /api/v1/tasks?proxy=<name>` | token + 白名单 | Collector 拉取任务 |
| `POST /api/v1/results?proxy=<name>` | token + 白名单 | Collector 上报结果 |
| `GET /api/v1/arp/search?q=<query>` | — | 统一搜索（IP/MAC/设备/Proxy） |
| `GET /api/v1/arp?device=&mac=&ip=` | — | 条件查询 ARP |
| `GET /api/v1/devices` | — | 设备列表（含采集状态） |
| `GET /api/v1/stats` | — | 采集统计 |
| `GET /health` | — | 健康检查 |

## 从源码构建

```bash
# 后端（会嵌入 internal/server/web/dist 中预构建的前端）
make build            # 本机二进制
make build-static     # CGO_ENABLED=0 linux/amd64，用于部署
make test             # 运行测试

# 前端（仅在修改 web/src 后需要）
cd web && npm ci && npm run build   # 输出到 internal/server/web/dist
```

仓库有意提交了 `internal/server/web/dist`，让用户无需 Node.js 即可 `go build`。

## 数据库结构

- `arp_snapshot_current` —— 每设备一行：元数据、最近一次采集状态/条目数/耗时
- `arp_entry_current` —— 每个 `(device_name, ip, mac)` 三元组一行：接口、类型、`created_at`（首次发现）、`collected_at`（最近采集）

常用分析查询（IP 冲突、漂移追踪、多网关检测）见 [docs/queries.md](docs/queries.md)。

## 已知限制 / Roadmap

- 仅支持 SNMPv2c（暂不支持 SNMPv3）
- Server API 仅 HTTP（如需暴露到可信网络之外，请用反向代理终止 TLS）
- Kafka TLS 目前 `InsecureSkipVerify`（证书校验开关计划中）
- Web 主机管理界面（手动增删改 + CSV 批量导入，类似 Zabbix）-- 计划中
- 测试覆盖较少（仅冒烟测试）—— 欢迎贡献

## 贡献

见 [CONTRIBUTING.md](CONTRIBUTING.md)，欢迎提交 Issue 和 PR。

## 许可证

[Apache License 2.0](LICENSE)
