# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.0.1] - 2026-07-30

Initial open-source release (sanitized from an internal project).

### Added

- Distributed Server/Collector architecture for ARP table collection
- NetBox device inventory sync with rule-based proxy assignment (`proxy_mappings`)
- SNMPv2c BulkWalk collection (ARP table + ifName) with concurrency control
- PostgreSQL storage with `(device_name, ip, mac)` primary key
  (IP-conflict detection, endpoint drift tracking, first-seen `created_at`)
- Optional Kafka and VictoriaMetrics outputs (disabled by default)
- Embedded React Web UI (dashboard + unified ARP search + CSV export)
- `config.example.yaml` with `${ENV_VAR}` secret expansion and bilingual comments
- Startup config validation (`Validate()`) — refuses to run with missing secrets
- Web UI Basic Auth via `server.web_auth` config (constant-time comparison)
- systemd unit templates, batch deploy script, and unit generator in `deploy/`
- Smoke tests for config loading, validation, and proxy mapping

### Security

- Removed all hardcoded credentials and internal endpoints from code, config,
  and documentation; secrets are only read from config files or env vars

### Pre-open-source history

Developed internally from 2026-01 to 2026-02 (v0.0.0 – v1.0.7). The most
significant internal change was the v1.0.7 primary-key migration
`(device_name, ip)` → `(device_name, ip, mac)`; its migration guide is
preserved at [docs/migration/v1.0.7.md](docs/migration/v1.0.7.md).

[0.0.1]: https://github.com/xiaerdemao1-art/arp-collect/releases/tag/v0.0.1
