# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.0.2] - 2026-07-30

### Added

- **One-command installer** (`deploy/install.sh`): sets up user, directories,
  binary, config template, env file, and systemd unit for server or collector
  in a single step; includes an `uninstall` subcommand
- **Pluggable device source**: new `device_source` config selects `netbox`
  (default) or `file`. File source reads a local yaml/csv device list, so
  deployments without NetBox can run with a hand-written inventory
  (`config/devices.example.yaml`, `config/devices.example.csv`)
- Config auto-discovery now also searches `/opt/arp-collect/config` and
  `/etc/arp-collect`
- Deployment docs rewritten around `install.sh`; manual steps retained as an
  advanced/custom section

## [0.0.1] - 2026-07-30

Initial release.

### Added

- Distributed Server/Collector architecture for ARP table collection
- NetBox device inventory sync with rule-based proxy assignment (`proxy_mappings`)
- SNMPv2c BulkWalk collection (ARP table + ifName) with concurrency control
- PostgreSQL storage with `(device_name, ip, mac)` primary key
  (IP-conflict detection, endpoint drift tracking, first-seen `created_at`)
- Optional Kafka and VictoriaMetrics outputs (disabled by default)
- Embedded React Web UI (dashboard + unified ARP search + CSV export)
- `config.example.yaml` with `${ENV_VAR}` secret expansion and bilingual comments
- Startup config validation (`Validate()`) - refuses to run with missing secrets
- Web UI Basic Auth via `server.web_auth` config (constant-time comparison)
- systemd unit templates, batch deploy script, and unit generator in `deploy/`
- Smoke tests for config loading, validation, and proxy mapping

### Security

- No hardcoded credentials in code, config, or documentation; secrets are
  only read from config files or env vars, and startup validation refuses to
  run when required secrets are missing

[0.0.2]: https://github.com/xiaerdemao1-art/arp-collect/releases/tag/v0.0.2
[0.0.1]: https://github.com/xiaerdemao1-art/arp-collect/releases/tag/v0.0.1
