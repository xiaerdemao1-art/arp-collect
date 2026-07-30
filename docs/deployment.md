# Distributed Deployment Guide

[中文](deployment.zh-CN.md)

This guide deploys arp-collect as a central **server** plus one or more
**collector** agents. All examples use systemd on Linux. The
[`deploy/install.sh`](../deploy/install.sh) installer handles the repetitive
setup (user, directories, systemd unit, config template) in one command.

```
                 ┌──────────────────────────────┐
                 │ server node                  │
                 │  arp_server     (:19527)     │
                 │  PostgreSQL                  │
                 └───────────────▲──────────────┘
              HTTP (token auth) │
   ┌──────────────┐  ┌──────────┴───┐  ┌──────────────┐
   │ proxy node 1 │  │ proxy node 2 │  │ proxy node N │
   │ arp_collector│  │ arp_collector│  │ arp_collector│
   └──────┬───────┘  └──────┬───────┘  └──────┬───────┘
          ▼ SNMP            ▼ SNMP            ▼ SNMP
      switches ...      switches ...      switches ...
```

## 1. Build static binaries

On any build machine with Go 1.23+:

```bash
make build-static   # produces bin/arp_server and bin/arp_collector (linux/amd64, CGO_ENABLED=0)
```

## 2. Server node

```bash
sudo ./deploy/install.sh server
```

That single command creates the `arp` user, lays out `/opt/arp-collect/`,
copies the binary + config template, writes `/etc/arp-collect/env`, installs
the `arp-server` systemd unit, and starts it.

Then edit the config, set secrets, and restart:

```bash
sudo vi /opt/arp-collect/config/config.yaml
#   - device_source: netbox or file (see README)
#   - postgres / snmp settings
#   - server.auth_token          (Collector API pre-shared token)
#   - server.web_auth            (Web UI credentials)
#   - server.allowed_proxies     (name + IP of every collector node)
#   - proxy_mappings + default_proxy

sudo vi /etc/arp-collect/env        # uncomment and fill ARP_* secret vars
sudo systemctl restart arp-server
```

Verify:

```bash
curl http://localhost:19527/health
curl -u admin http://localhost:19527/   # prompts for your web_auth password
```

## 3. Collector nodes

On each collection node (must have SNMP reachability to its devices and HTTP
reachability to the server):

```bash
sudo ./deploy/install.sh collector --proxy <name> --server-url http://<server-ip>:19527
```

The installer pre-fills `collector.proxy_name` and `collector.server_url` in
the config; finish the rest and start collecting:

```bash
sudo vi /opt/arp-collect/config/config.yaml   # set snmp, collector.auth_token
sudo vi /etc/arp-collect/env                   # ARP_SNMP_COMMUNITY, ARP_COLLECTOR_AUTHTOKEN
sudo systemctl restart arp-collector
journalctl -u arp-collector -f
```

> `collector.auth_token` must equal the server's `server.auth_token`.

Smoke test without installing (runs one collection cycle and exits):

```bash
/opt/arp-collect/bin/arp_collector \
  -config /opt/arp-collect/config/config.yaml \
  -mode collector -server http://<server-ip>:19527 -proxy <proxy-name> -once
```

## 4. Batch deployment (optional)

`deploy/deploy_all_collectors.sh` pushes the binary and a generated systemd
unit to every node listed in `proxies.txt` over SSH:

```bash
cd deploy
cp proxies.txt.example proxies.txt   # fill in: name ip (one per line)
$EDITOR proxies.txt

SERVER_URL=http://<server-ip>:19527 SSH_USER=root ./deploy_all_collectors.sh deploy        # all nodes
SERVER_URL=http://<server-ip>:19527 ./deploy_all_collectors.sh deploy proxy1               # one node
./deploy_all_collectors.sh status     # fleet status
./deploy_all_collectors.sh test proxy1
```

## 5. Uninstall

```bash
sudo ./deploy/install.sh uninstall server      # or: uninstall collector
```

Stops/disables the service, removes the unit, and deletes the binary. Your
config is backed up to `config.yaml.bak.<ts>` and otherwise left in place.
To remove everything: `sudo rm -rf /opt/arp-collect && sudo userdel arp`.

## 6. Operations cheat-sheet

| Task | Command |
|---|---|
| Service status | `systemctl status arp-server` / `arp-collector` |
| Follow logs | `journalctl -u arp-server -f` |
| Restart after upgrade | replace binary, `systemctl restart arp-server` (then collectors) |
| Single-device debug | `arp_collector -mode standalone -device <ip> -config ...` |
| Health check | `curl http://<server>:19527/health` |

> **Note:** after deploying a new binary version, always restart the service -
> the running process keeps the old code until restarted.

## 7. Firewall / ports

| From | To | Port | Purpose |
|---|---|---|---|
| collector | server | 19527/tcp | task pull + result submission |
| collector | devices | 161/udp | SNMPv2c |
| server | NetBox | 443/80 tcp | device inventory |
| server | PostgreSQL | 5432/tcp | storage |
| server | Kafka / VictoriaMetrics | 9092 / 8428 tcp | optional outputs |

## 8. Manual / custom install

Prefer `install.sh`, but if you need full control (custom paths, non-systemd
init, containers), it just runs these primitives - replicate as needed:

```bash
sudo useradd -r -s /sbin/nologin arp
sudo mkdir -p /opt/arp-collect/bin /opt/arp-collect/config /etc/arp-collect
sudo cp bin/arp_server /opt/arp-collect/bin/
sudo cp config/config.example.yaml /opt/arp-collect/config/config.yaml
# edit config, write /etc/arp-collect/env (chmod 600), install your unit
```

The binary auto-discovers config at `./config/`, `/opt/arp-collect/config/`,
and `/etc/arp-collect/`, so `-config` is optional when installed there.
