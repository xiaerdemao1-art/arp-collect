# 分布式部署指南

[English](deployment.md)

本指南把 arp-collect 部署为一个中心 **server** 加若干 **collector** 采集节点，示例均基于 Linux + systemd。[`deploy/install.sh`](../deploy/install.sh) 安装脚本用一条命令完成建用户、建目录、systemd unit、配置模板等重复工作。

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
sudo ./deploy/install.sh server
```

这一条命令会：建 `arp` 用户、铺好 `/opt/arp-collect/`、拷贝二进制 + 配置模板、写 `/etc/arp-collect/env`、装 `arp-server` systemd unit 并启动。

然后改配置、填秘密、重启：

```bash
sudo vi /opt/arp-collect/config/config.yaml
#   - device_source: netbox 或 file（见 README）
#   - postgres / snmp 配置
#   - server.auth_token          （Collector API 预共享密钥）
#   - server.web_auth            （Web UI 账号密码）
#   - server.allowed_proxies     （每个采集节点的名称 + IP）
#   - proxy_mappings + default_proxy

sudo vi /etc/arp-collect/env        # 取消注释并填入 ARP_* 秘密变量
sudo systemctl restart arp-server
```

验证：

```bash
curl http://localhost:19527/health
curl -u admin http://localhost:19527/   # 交互式输入 web_auth 密码
```

## 3. Collector 采集节点

在每个采集节点上（需要能 SNMP 访问目标设备、能 HTTP 访问 Server）：

```bash
sudo ./deploy/install.sh collector --proxy <名称> --server-url http://<server-ip>:19527
```

安装脚本会预填 `collector.proxy_name` 和 `collector.server_url`；补全其余配置即可开始采集：

```bash
sudo vi /opt/arp-collect/config/config.yaml   # 设置 snmp、collector.auth_token
sudo vi /etc/arp-collect/env                   # ARP_SNMP_COMMUNITY、ARP_COLLECTOR_AUTHTOKEN
sudo systemctl restart arp-collector
journalctl -u arp-collector -f
```

> `collector.auth_token` 必须与 Server 端 `server.auth_token` 一致。

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

## 5. 卸载

```bash
sudo ./deploy/install.sh uninstall server      # 或：uninstall collector
```

停止并禁用服务、删 unit、删二进制。配置会备份为 `config.yaml.bak.<时间戳>`，目录保留。彻底清理：`sudo rm -rf /opt/arp-collect && sudo userdel arp`。

## 6. 运维速查

| 操作 | 命令 |
|---|---|
| 查看状态 | `systemctl status arp-server` / `arp-collector` |
| 跟踪日志 | `journalctl -u arp-server -f` |
| 升级后重启 | 替换二进制，`systemctl restart arp-server`（然后重启各 collector） |
| 单设备调试 | `arp_collector -mode standalone -device <ip> -config ...` |
| 健康检查 | `curl http://<server>:19527/health` |

> **注意**：更新二进制版本后必须重启服务--运行中的进程在重启前仍执行旧代码。

## 7. 防火墙 / 端口

| 源 | 目标 | 端口 | 用途 |
|---|---|---|---|
| collector | server | 19527/tcp | 拉任务 + 上报结果 |
| collector | 网络设备 | 161/udp | SNMPv2c |
| server | NetBox | 443/80 tcp | 设备清单 |
| server | PostgreSQL | 5432/tcp | 存储 |
| server | Kafka / VictoriaMetrics | 9092 / 8428 tcp | 可选输出 |

## 8. 手动 / 自定义安装

推荐用 `install.sh`；如需完全掌控（自定义路径、非 systemd init、容器），它本质就是下面这些原语，按需复制即可：

```bash
sudo useradd -r -s /sbin/nologin arp
sudo mkdir -p /opt/arp-collect/bin /opt/arp-collect/config /etc/arp-collect
sudo cp bin/arp_server /opt/arp-collect/bin/
sudo cp config/config.example.yaml /opt/arp-collect/config/config.yaml
# 改配置、写 /etc/arp-collect/env（chmod 600）、装你自己的 unit
```

二进制会自动从 `./config/`、`/opt/arp-collect/config/`、`/etc/arp-collect/` 查找配置，所以装到这些位置时 `-config` 参数可省。
