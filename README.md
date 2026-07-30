# arp-collect

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.23%2B-00ADD8?logo=go)](go.mod)

[中文文档](README.zh-CN.md)

**arp-collect** is a distributed ARP table collection system for large campus networks. It was built to replace Zabbix-based ARP monitoring, whose 64KB item-value limit silently truncates large ARP tables on core switches (10k–100k+ entries).

It pulls the device inventory from [NetBox](https://github.com/netbox-community/netbox), distributes collection tasks to lightweight agents (`arp_collector`) running next to your network devices, collects ARP tables via SNMPv2c BulkWalk, and stores everything in PostgreSQL — with optional outputs to Kafka and VictoriaMetrics, plus a built-in React web UI for searching.

## Architecture

```
                        ┌─────────────────── NetBox (device inventory CMDB)
                        │   roles: CoreSwitch / DistributionSwitch / ...
                        ▼
┌────────────────────────────────────────────────────────────┐
│  arp_server  (central server, listens on :19527)            │
│  ├─ NetBox sync (hourly) — assigns each device to a proxy  │
│  │   via configurable proxy_mappings rules                 │
│  ├─ HTTP API                                               │
│  │    GET  /api/v1/tasks     collectors pull tasks         │
│  │    POST /api/v1/results   collectors submit results     │
│  │    GET  /api/v1/arp|/arp/search|/devices|/stats         │
│  │    GET  /health                                         │
│  ├─ React SPA web UI (embedded, Basic Auth)                │
│  ├─ PostgreSQL  (arp_snapshot_current + arp_entry_current) │
│  ├─ Kafka            (optional)                            │
│  └─ VictoriaMetrics  (optional, event-driven push)         │
└────────────────────────────────────────────────────────────┘
          ▲ GET tasks / POST results  (X-Auth-Token + optional IP allowlist)
          │
┌─────────┴───────────────────────────────────┐
│  arp_collector  (one per collection node)   │
│  SNMPv2c BulkWalk on local devices:         │
│    ARP  table  OID 1.3.6.1.2.1.4.22.1.2     │
│    ifName      OID 1.3.6.1.2.1.31.1.1.1.1   │
└─────────────────────────────────────────────┘
```

Three run modes:

| Mode | Binary | Description |
|---|---|---|
| `server` | `arp_server` | Central server: NetBox sync, task distribution, storage, Web UI |
| `collector` | `arp_collector -mode collector` | Agent: pulls tasks from server, collects via SNMP, posts results |
| `standalone` | `arp_collector -mode standalone` | All-in-one for small setups/testing: talks to NetBox + PostgreSQL directly |

## Features

- **Scales to 100k+ ARP entries per device** — no 64KB truncation
- **NetBox-driven inventory** — devices appear/disappear automatically, no per-host configuration
- **Rule-based task distribution** — assign devices to collectors by site / region / role (`proxy_mappings`)
- **Smart primary key** `(device_name, ip, mac)` — enables IP-conflict detection, endpoint drift tracking, and multi-gateway (VRRP/stack) visibility; `created_at` records first-seen time
- **Slow-device scheduling** — devices with huge ARP tables can run on their own longer interval
- **Web UI** — dashboard + unified ARP search (IP / MAC / device / proxy), CSV export, and a host-management page (manual CRUD + CSV batch import) for the `database` device source
- **Optional outputs** — Kafka topic per ARP record; VictoriaMetrics metrics (`arp_collect_device_arp_count`, `arp_collect_device_duration_ms`, `arp_collect_device_status`)
- **Static binaries** — `CGO_ENABLED=0`, trivial to deploy with systemd

## Quick Start (standalone mode)

Prerequisites: Go 1.23+, a reachable NetBox instance, PostgreSQL, and at least one switch with SNMPv2c enabled.

```bash
# 1. Build (no Node.js needed — the web UI is pre-built and embedded)
go build -o bin/arp_collector ./cmd/collector
go build -o bin/arp_server     ./cmd/server

# 2. Configure
cp config/config.example.yaml config/config.yaml
# edit config/config.yaml — set netbox/snmp/postgres, or use env vars:
export ARP_NETBOX_TOKEN=your-netbox-token
export ARP_POSTGRES_PASSWORD=your-pg-password
export ARP_SNMP_COMMUNITY=your-snmp-community

# 3. Test a single device first
./bin/arp_collector -config config/config.yaml -device 192.0.2.1

# 4. Run one full collection pass
./bin/arp_collector -config config/config.yaml -once

# 5. Or run the central server (includes Web UI on :19527)
./bin/arp_server -config config/config.yaml
```

Open `http://localhost:19527` and log in with the `server.web_auth` credentials.

> **Security note:** no credentials have defaults. Secrets are read from the
> config file or environment variables (`${ENV_VAR}` expansion is supported for
> every secret field), and the server refuses to start when required secrets
> are missing.

## Configuration

See [`config/config.example.yaml`](config/config.example.yaml) — every option is commented in both English and Chinese. Highlights:

- `device_source` - device inventory source: `netbox` (default) / `file` (local yaml/csv) / `database` (managed via Web UI host-management page)
- `proxy_mappings` — ordered rules mapping NetBox site/region/role → collector name (`first match wins`), with `default_proxy` as fallback
- `server.allowed_proxies` — optional collector allowlist with IP pinning (Zabbix-style)
- `collector.slow_devices` — per-device collection intervals for large ARP tables
- `server.web_auth` — Web UI Basic Auth (disable only behind another auth layer)
- `kafka` / `victoria_metrics` — optional outputs, disabled by default

Any key can also be overridden with an `ARP_`-prefixed environment variable (e.g. `ARP_SERVER_AUTHTOKEN`).

## Distributed Deployment

See [docs/deployment.md](docs/deployment.md) ([中文](docs/deployment.zh-CN.md)). The fastest path is the one-command installer:

    sudo ./deploy/install.sh server      # on the server node
    sudo ./deploy/install.sh collector --proxy <name> --server-url http://<server>:19527

In short:

1. Run `arp_server` on a central node (with PostgreSQL reachable).
2. On each collection node: copy the static binary + config, `systemctl enable --now arp-collector`.
3. `deploy/` contains systemd unit templates, a batch deployment script (`deploy_all_collectors.sh`, driven by `proxies.txt`), and a unit-file generator.

## API Overview

| Endpoint | Auth | Description |
|---|---|---|
| `GET /api/v1/tasks?proxy=<name>` | token + allowlist | Collector task pull |
| `POST /api/v1/results?proxy=<name>` | token + allowlist | Collector result submission |
| `GET /api/v1/arp/search?q=<query>` | — | Unified search (IP / MAC / device / proxy) |
| `GET /api/v1/arp?device=&mac=&ip=` | — | Filtered ARP query |
| `GET /api/v1/devices` | — | Devices with collection status |
| `GET /api/v1/stats` | — | Collection statistics |
| `GET /health` | — | Health check |

## Building from Source

```bash
# Backend (embeds the pre-built web UI from internal/server/web/dist)
make build            # local binaries
make build-static     # CGO_ENABLED=0 linux/amd64, for deployment
make test             # go tests

# Frontend (only needed when you change web/src)
cd web && npm ci && npm run build   # outputs to internal/server/web/dist
```

The repository intentionally commits `internal/server/web/dist` so users can `go build` without Node.js.

## Database Schema

- `arp_snapshot_current` — one row per device: metadata, last collection status/count/duration
- `arp_entry_current` — one row per `(device_name, ip, mac)` triple: interface, type, `created_at` (first seen), `collected_at` (last seen)

Useful queries (IP conflicts, drift tracking, multi-gateway detection) are documented in [docs/queries.md](docs/queries.md).

## Known Limitations / Roadmap

- SNMPv2c only (no SNMPv3 yet)
- HTTP-only server API (terminate TLS at a reverse proxy if exposed beyond a trusted network)
- Kafka TLS uses `InsecureSkipVerify` (verification toggle planned)
- Test coverage is minimal (smoke tests only) — contributions welcome

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Bug reports and pull requests are welcome.

## License

[Apache License 2.0](LICENSE)
