# Distributed Deployment Guide

[中文](deployment.zh-CN.md)

This guide deploys arp-collect as a central **server** plus one or more
**collector** agents. All examples use systemd on Linux.

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
sudo useradd -r -s /sbin/nologin arp || true
sudo mkdir -p /opt/arp-collect/bin /opt/arp-collect/config /etc/arp-collect
sudo cp bin/arp_server /opt/arp-collect/bin/

# Config
sudo cp config/config.example.yaml /opt/arp-collect/config/config.yaml
sudoedit /opt/arp-collect/config/config.yaml
#   - server.mode: "server"
#   - netbox / postgres / snmp settings
#   - server.auth_token          (Collector API pre-shared token)
#   - server.web_auth            (Web UI credentials)
#   - server.allowed_proxies     (name + IP of every collector node)
#   - proxy_mappings + default_proxy

# Secrets via environment (recommended): put them in an env file instead of YAML
sudo tee /etc/arp-collect/env > /dev/null <<'EOF'
ARP_NETBOX_TOKEN=change-me
ARP_POSTGRES_PASSWORD=change-me
ARP_SERVER_AUTHTOKEN=change-me
ARP_SERVER_WEBAUTH_PASSWORD=change-me
EOF
sudo chmod 600 /etc/arp-collect/env
sudo chown -R arp:arp /opt/arp-collect /etc/arp-collect

# systemd
sudo cp deploy/arp-server.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now arp-server
systemctl status arp-server
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
sudo useradd -r -s /sbin/nologin arp || true
sudo mkdir -p /opt/arp-collect/bin /opt/arp-collect/config /etc/arp-collect
sudo cp bin/arp_collector /opt/arp-collect/bin/

sudo cp config/config.example.yaml /opt/arp-collect/config/config.yaml
sudoedit /opt/arp-collect/config/config.yaml
#   - server.mode: "collector"
#   - collector.proxy_name:  this node's name (must match allowed_proxies)
#   - collector.server_url:  http://<server-ip>:19527
#   - snmp settings

sudo tee /etc/arp-collect/env > /dev/null <<'EOF'
ARP_SNMP_COMMUNITY=change-me
ARP_COLLECTOR_AUTHTOKEN=change-me   # must equal server.auth_token
EOF
sudo chmod 600 /etc/arp-collect/env
sudo chown -R arp:arp /opt/arp-collect /etc/arp-collect

# systemd — use the template, replacing PROXY_NAME and the -server URL
sudo cp deploy/arp-collector.service /etc/systemd/system/
sudoedit /etc/systemd/system/arp-collector.service
sudo systemctl daemon-reload
sudo systemctl enable --now arp-collector
journalctl -u arp-collector -f
```

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

`deploy/generate_configs.sh` renders one `proxy_configs/<name>.service` per
entry in `proxies.txt`, if you prefer to distribute unit files yourself.

## 5. Operations cheat-sheet

| Task | Command |
|---|---|
| Service status | `systemctl status arp-server` / `arp-collector` |
| Follow logs | `journalctl -u arp-server -f` |
| Restart after upgrade | replace binary, `systemctl restart arp-server` (then collectors) |
| Single-device debug | `arp_collector -mode standalone -device <ip> -config ...` |
| Health check | `curl http://<server>:19527/health` |

> **Note:** after deploying a new binary version, always restart the service —
> the running process keeps the old code until restarted.

## 6. Firewall / ports

| From | To | Port | Purpose |
|---|---|---|---|
| collector | server | 19527/tcp | task pull + result submission |
| collector | devices | 161/udp | SNMPv2c |
| server | NetBox | 443/80 tcp | device inventory |
| server | PostgreSQL | 5432/tcp | storage |
| server | Kafka / VictoriaMetrics | 9092 / 8428 tcp | optional outputs |
