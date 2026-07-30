#!/bin/bash
# ===================================================================
# ARP Collector 服务配置生成器 / systemd unit generator
# 从 proxies.txt 为每个 Proxy 生成独立的 systemd 服务配置文件
# Generate one systemd unit per proxy listed in proxies.txt
# ===================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROXIES_FILE="${PROXIES_FILE:-$SCRIPT_DIR/proxies.txt}"
OUTPUT_DIR="${OUTPUT_DIR:-./proxy_configs}"
SERVER_URL="${SERVER_URL:-http://server.example.com:19527}"
REMOTE_DIR="${REMOTE_DIR:-/opt/arp-collect}"

if [[ ! -f "$PROXIES_FILE" ]]; then
    echo "❌ Proxy 清单不存在: $PROXIES_FILE"
    echo "请先执行: cp $SCRIPT_DIR/proxies.txt.example $PROXIES_FILE 并填入你的节点"
    exit 1
fi

mkdir -p "$OUTPUT_DIR"
count=0

while read -r proxy_name proxy_ip; do
    [[ -z "$proxy_name" || "$proxy_name" == \#* ]] && continue
    config_file="${OUTPUT_DIR}/${proxy_name}.service"

    cat > "$config_file" << EOF
[Unit]
Description=ARP Collector Agent ($proxy_name)
After=network.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=-/etc/arp-collect/env
WorkingDirectory=$REMOTE_DIR
ExecStart=$REMOTE_DIR/bin/arp_collector -config $REMOTE_DIR/config/config.yaml -mode collector -server $SERVER_URL -proxy $proxy_name
Restart=always
RestartSec=30
StandardOutput=journal
StandardError=journal
SyslogIdentifier=arp-collector

[Install]
WantedBy=multi-user.target
EOF

    echo "✓ $proxy_name ($proxy_ip) -> $config_file"
    count=$((count + 1))
done < "$PROXIES_FILE"

echo ""
echo "已生成 $count 个配置文件到: $OUTPUT_DIR/"
echo ""
echo "使用方法 / Usage:"
echo "1. 在各 Proxy 执行: sudo mkdir -p $REMOTE_DIR/bin $REMOTE_DIR/config"
echo "2. 分发 arp_collector 二进制到各 Proxy 的 $REMOTE_DIR/bin/"
echo "3. 分发对应 .service 文件到 /etc/systemd/system/arp-collector.service"
echo "4. 在各 Proxy 执行: sudo systemctl daemon-reload && sudo systemctl enable --now arp-collector"
