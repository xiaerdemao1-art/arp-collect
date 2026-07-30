#!/bin/bash
# deploy_collector.sh - 在单个 Proxy 节点上部署 ARP Collector
# Deploy ARP Collector on a single proxy node (run ON the proxy node)
#
# 用法 / Usage:
#   sudo ./deploy_collector.sh <proxy_name> [server_url]
# 示例 / Example:
#   sudo ./deploy_collector.sh proxy1 http://192.0.2.40:19527

set -e

PROXY_NAME="${1:-}"
SERVER_URL="${2:-http://server.example.com:19527}"
INSTALL_DIR="${INSTALL_PREFIX:-/opt/arp-collect}"
SERVICE_FILE="/etc/systemd/system/arp-collector.service"

if [ -z "$PROXY_NAME" ]; then
    echo "用法 / Usage: $0 <proxy_name> [server_url]"
    echo "示例 / Example: $0 proxy1 http://192.0.2.40:19527"
    exit 1
fi

echo "=========================================="
echo "部署 ARP Collector: $PROXY_NAME"
echo "Server: $SERVER_URL"
echo "=========================================="

# 检查二进制文件
if [ ! -x "$INSTALL_DIR/bin/arp_collector" ]; then
    echo "❌ 错误: $INSTALL_DIR/bin/arp_collector 不存在或不可执行"
    echo "请先将 arp_collector 二进制文件复制到 $INSTALL_DIR/bin/"
    exit 1
fi

# 创建 systemd 服务文件
echo "📝 创建 systemd 服务文件..."
cat > "$SERVICE_FILE" << EOF
[Unit]
Description=ARP Collector Agent ($PROXY_NAME)
After=network.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=-/etc/arp-collect/env
ExecStart=$INSTALL_DIR/bin/arp_collector -config $INSTALL_DIR/config/config.yaml -mode collector -server $SERVER_URL -proxy $PROXY_NAME
Restart=always
RestartSec=30
StandardOutput=journal
StandardError=journal
SyslogIdentifier=arp-collector

[Install]
WantedBy=multi-user.target
EOF

# 重新加载 systemd
echo "🔄 重新加载 systemd..."
systemctl daemon-reload

# 启用并启动服务
echo "🚀 启动 arp-collector 服务..."
systemctl enable arp-collector
systemctl restart arp-collector

sleep 2

echo ""
echo "=========================================="
echo "服务状态 / Service status:"
echo "=========================================="
systemctl status arp-collector --no-pager

echo ""
echo "✅ 部署完成!"
echo ""
echo "常用命令 / Useful commands:"
echo "  查看状态: systemctl status arp-collector"
echo "  查看日志: journalctl -u arp-collector -f"
echo "  重启服务: systemctl restart arp-collector"
