# 分布式部署指南

[English](deployment.md)

本指南把 arp-collect 部署为一个中心 **server** 加若干 **collector** 采集节点，示例均基于 Linux + systemd。

```
                 ┌──────────────────────────────┐
                 │ server 节点                   │
                 │  arp_server     (:19527)     │
                 │  PostgreSQL                  │
                 └───────────────▲──────────────┘
              HTTP（Token 认证） │
   ┌──────────────┐  ┌──────────┴───┐  ┌──────────────┐
   │ proxy 节点 1 │  │ proxy 节点 2 │  │ proxy 节点 N │
   │ arp_collector│  │ arp_collector│  │ arp_collector│
   └──────┬───────┘  └──────┬───────┘  └──────┬───────┘
          ▼ SNMP            ▼ SNMP            ▼ SNMP
       交换机 ...        交换机 ...        交换机 ...
```

## 1. 编译静态二进制

在任意装有 Go 1.23+ 的机器上：

```bash
make build-static   # 产出 bin/arp_server 和 bin/arp_collector（linux/amd64，CGO_ENABLED=0）
```

## 2. Server 节点

```bash
sudo useradd -r -s /sbin/nologin arp || true
sudo mkdir -p /opt/arp-collect/bin /opt/arp-collect/config /etc/arp-collect
sudo cp bin/arp_server /opt/arp-collect/bin/

# 配置
sudo cp config/config.example.yaml /opt/arp-collect/config/config.yaml
sudoedit /opt/arp-collect/config/config.yaml
#   - server.mode: "server"
#   - netbox / postgres / snmp 相关配置
#   - server.auth_token          （Collector API 预共享密钥）
#   - server.web_auth            （Web UI 登录账号密码）
#   - server.allowed_proxies     （每个采集节点的名称 + IP）
#   - proxy_mappings + default_proxy（设备 → 采集节点分配规则）

# 秘密建议放环境文件，而不是写进 YAML
sudo tee /etc/arp-collect/env > /dev/null <<'EOF'
ARP_NETBOX_TOKEN=改成你的
ARP_POSTGRES_PASSWORD=改成你的
ARP_SERVER_AUTHTOKEN=改成你的
ARP_SERVER_WEBAUTH_PASSWORD=改成你的
EOF
sudo chmod 600 /etc/arp-collect/env
sudo chown -R arp:arp /opt/arp-collect /etc/arp-collect

# systemd
sudo cp deploy/arp-server.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now arp-server
systemctl status arp-server
```

验证：

```bash
curl http://localhost:19527/health
curl -u admin http://localhost:19527/   # 交互式输入 web_auth 密码
```

## 3. Collector 采集节点

在每个采集节点上（需要能 SNMP 访问目标设备、能 HTTP 访问 Server）：

```bash
sudo useradd -r -s /sbin/nologin arp || true
sudo mkdir -p /opt/arp-collect/bin /opt/arp-collect/config /etc/arp-collect
sudo cp bin/arp_collector /opt/arp-collect/bin/

sudo cp config/config.example.yaml /opt/arp-collect/config/config.yaml
sudoedit /opt/arp-collect/config/config.yaml
#   - server.mode: "collector"
#   - collector.proxy_name:  本节点名称（必须与 allowed_proxies 对应）
#   - collector.server_url:  http://<server-ip>:19527
#   - snmp 相关配置

sudo tee /etc/arp-collect/env > /dev/null <<'EOF'
ARP_SNMP_COMMUNITY=改成你的
ARP_COLLECTOR_AUTHTOKEN=改成你的   # 必须与 server.auth_token 一致
EOF
sudo chmod 600 /etc/arp-collect/env
sudo chown -R arp:arp /opt/arp-collect /etc/arp-collect

# systemd —— 用模板，替换 PROXY_NAME 和 -server 地址
sudo cp deploy/arp-collector.service /etc/systemd/system/
sudoedit /etc/systemd/system/arp-collector.service
sudo systemctl daemon-reload
sudo systemctl enable --now arp-collector
journalctl -u arp-collector -f
```

不装服务先冒烟一次（执行一轮采集后退出）：

```bash
/opt/arp-collect/bin/arp_collector \
  -config /opt/arp-collect/config/config.yaml \
  -mode collector -server http://<server-ip>:19527 -proxy <proxy名称> -once
```

## 4. 批量部署（可选）

`deploy/deploy_all_collectors.sh` 通过 SSH 把二进制和生成的 systemd unit 推送到 `proxies.txt` 中的所有节点：

```bash
cd deploy
cp proxies.txt.example proxies.txt   # 填入：名称 IP（每行一个）
$EDITOR proxies.txt

SERVER_URL=http://<server-ip>:19527 SSH_USER=root ./deploy_all_collectors.sh deploy        # 全部节点
SERVER_URL=http://<server-ip>:19527 ./deploy_all_collectors.sh deploy proxy1               # 单个节点
./deploy_all_collectors.sh status     # 查看所有节点状态
./deploy_all_collectors.sh test proxy1
```

如果你想自己分发 unit 文件，可以用 `deploy/generate_configs.sh` 按 `proxies.txt` 批量生成 `proxy_configs/<名称>.service`。

## 5. 运维速查

| 操作 | 命令 |
|---|---|
| 查看状态 | `systemctl status arp-server` / `arp-collector` |
| 跟踪日志 | `journalctl -u arp-server -f` |
| 升级后重启 | 替换二进制，`systemctl restart arp-server`（然后重启各 collector） |
| 单设备调试 | `arp_collector -mode standalone -device <ip> -config ...` |
| 健康检查 | `curl http://<server>:19527/health` |

> **注意**：更新二进制版本后必须重启服务——运行中的进程在重启前仍执行旧代码。

## 6. 防火墙 / 端口

| 源 | 目标 | 端口 | 用途 |
|---|---|---|---|
| collector | server | 19527/tcp | 拉任务 + 上报结果 |
| collector | 网络设备 | 161/udp | SNMPv2c |
| server | NetBox | 443/80 tcp | 设备清单 |
| server | PostgreSQL | 5432/tcp | 存储 |
| server | Kafka / VictoriaMetrics | 9092 / 8428 tcp | 可选输出 |
