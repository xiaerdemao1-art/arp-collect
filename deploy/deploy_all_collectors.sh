#!/bin/bash
# =============================================================================
# ARP Collector 批量部署脚本 / Batch deployment script
# 通过 SSH 把 arp_collector 部署到 proxies.txt 中的所有 Proxy 节点
# Deploy arp_collector to all proxy nodes listed in proxies.txt via SSH
#
# 准备 / Prerequisites:
#   1. cp proxies.txt.example proxies.txt  # 填入你的节点 / fill in your nodes
#   2. 配置好到各节点的 SSH 免密登录 / passwordless SSH to each node
#   3. make build-static                  # 编译 linux/amd64 静态二进制
# =============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROXIES_FILE="${PROXIES_FILE:-$SCRIPT_DIR/proxies.txt}"

# 配置 / Configuration（可用环境变量覆盖 / override via env）
SERVER_URL="${SERVER_URL:-http://server.example.com:19527}"
BINARY_PATH="${BINARY_PATH:-$SCRIPT_DIR/../bin/arp_collector}"
REMOTE_DIR="${REMOTE_DIR:-/opt/arp-collect}"
SSH_USER="${SSH_USER:-root}"

# 从 proxies.txt 加载节点列表（格式: name ip，# 开头为注释）
declare -A PROXIES=()
load_proxies() {
    if [[ ! -f "$PROXIES_FILE" ]]; then
        log_error "Proxy 清单不存在: $PROXIES_FILE"
        log_info "请先执行: cp $SCRIPT_DIR/proxies.txt.example $PROXIES_FILE 并填入你的节点"
        exit 1
    fi
    while read -r name ip; do
        [[ -z "$name" || "$name" == \#* ]] && continue
        PROXIES["$name"]="$ip"
    done < "$PROXIES_FILE"
    if [[ ${#PROXIES[@]} -eq 0 ]]; then
        log_error "Proxy 清单为空: $PROXIES_FILE"
        exit 1
    fi
}

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

usage() {
    echo "用法 / Usage: $0 <command> [proxy_name]"
    echo ""
    echo "命令 / Commands:"
    echo "  deploy [proxy]    - 部署到指定或全部 proxy / deploy to one or all proxies"
    echo "  status [proxy]    - 查看服务状态 / service status"
    echo "  start|stop|restart [proxy] - 服务控制 / service control"
    echo "  logs <proxy>      - 查看日志 / view logs"
    echo "  test <proxy>      - 测试单次采集 / run a single collection"
    echo "  list              - 列出所有 proxy / list proxies"
    echo ""
    echo "环境变量 / Environment variables:"
    echo "  SERVER_URL   (default: http://server.example.com:19527)"
    echo "  BINARY_PATH  (default: ../bin/arp_collector)"
    echo "  REMOTE_DIR   (default: /opt/arp-collect)"
    echo "  SSH_USER     (default: root)"
    exit 1
}

check_binary() {
    if [[ ! -f "$BINARY_PATH" ]]; then
        log_error "二进制文件不存在: $BINARY_PATH"
        log_info "请先编译: make build-static"
        exit 1
    fi
}

# 生成 systemd service 文件内容
generate_service() {
    local proxy_name=$1
    cat << EOF
[Unit]
Description=ARP Collector Agent ($proxy_name)
After=network.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=-/etc/arp-collect/env
ExecStart=$REMOTE_DIR/bin/arp_collector -config $REMOTE_DIR/config/config.yaml -mode collector -server $SERVER_URL -proxy $proxy_name
Restart=always
RestartSec=30
StandardOutput=journal
StandardError=journal
SyslogIdentifier=arp-collector

[Install]
WantedBy=multi-user.target
EOF
}

# 部署到单个 proxy
deploy_single() {
    local proxy_name=$1
    local proxy_ip=${PROXIES[$proxy_name]}

    if [[ -z "$proxy_ip" ]]; then
        log_error "未知的 proxy: $proxy_name（未在 $PROXIES_FILE 中定义）"
        return 1
    fi

    log_info "部署到 $proxy_name ($proxy_ip)..."

    log_info "  复制二进制文件..."
    if ! ssh -o ConnectTimeout=10 "${SSH_USER}@${proxy_ip}" "mkdir -p $REMOTE_DIR/bin $REMOTE_DIR/config" 2>/dev/null; then
        log_error "  SSH 连接失败，跳过 $proxy_name"
        return 1
    fi
    if ! scp -o ConnectTimeout=10 "$BINARY_PATH" "${SSH_USER}@${proxy_ip}:$REMOTE_DIR/bin/arp_collector" 2>/dev/null; then
        log_error "  复制失败，跳过 $proxy_name"
        return 1
    fi

    log_info "  配置 systemd 服务..."
    local service_content
    service_content=$(generate_service "$proxy_name")
    ssh -o ConnectTimeout=10 "${SSH_USER}@${proxy_ip}" << EOF
chmod +x $REMOTE_DIR/bin/arp_collector
cat > /etc/systemd/system/arp-collector.service << 'SERVICEEOF'
$service_content
SERVICEEOF
systemctl daemon-reload
systemctl enable arp-collector
systemctl restart arp-collector
EOF

    if [[ $? -eq 0 ]]; then
        log_info "  ✅ $proxy_name 部署成功"
    else
        log_error "  ❌ $proxy_name 部署失败"
        return 1
    fi
}

deploy_all() {
    check_binary
    log_info "开始批量部署到 ${#PROXIES[@]} 个 Proxy..."
    echo ""

    local success=0
    local failed=0

    for proxy_name in "${!PROXIES[@]}"; do
        if deploy_single "$proxy_name"; then
            ((success++))
        else
            ((failed++))
        fi
        echo ""
    done

    echo "======================================"
    log_info "部署完成: 成功 $success, 失败 $failed"
}

run_on_proxies() {
    local cmd=$1
    local target=$2

    if [[ -n "$target" ]]; then
        local proxy_ip=${PROXIES[$target]}
        if [[ -z "$proxy_ip" ]]; then
            log_error "未知的 proxy: $target"
            exit 1
        fi
        echo "[$target] $proxy_ip:"
        ssh -o ConnectTimeout=10 "${SSH_USER}@${proxy_ip}" "$cmd" 2>/dev/null || echo "  连接失败"
    else
        for proxy_name in "${!PROXIES[@]}"; do
            local proxy_ip=${PROXIES[$proxy_name]}
            echo "[$proxy_name] $proxy_ip:"
            ssh -o ConnectTimeout=10 "${SSH_USER}@${proxy_ip}" "$cmd" 2>/dev/null || echo "  连接失败"
            echo ""
        done
    fi
}

test_collect() {
    local proxy_name=$1
    local proxy_ip=${PROXIES[$proxy_name]}

    if [[ -z "$proxy_ip" ]]; then
        log_error "未知的 proxy: $proxy_name"
        exit 1
    fi

    log_info "在 $proxy_name ($proxy_ip) 上执行单次测试采集..."
    ssh -o ConnectTimeout=30 "${SSH_USER}@${proxy_ip}" \
        "$REMOTE_DIR/bin/arp_collector -mode collector -server $SERVER_URL -proxy $proxy_name -once" 2>&1
}

list_proxies() {
    echo "Proxy 列表 (共 ${#PROXIES[@]} 个):"
    echo "======================================"
    printf "%-30s %s\n" "名称" "IP 地址"
    echo "--------------------------------------"
    for proxy_name in $(echo "${!PROXIES[@]}" | tr ' ' '\n' | sort); do
        printf "%-30s %s\n" "$proxy_name" "${PROXIES[$proxy_name]}"
    done
}

load_proxies

case "${1:-}" in
    deploy)
        if [[ -n "${2:-}" ]]; then
            check_binary
            deploy_single "$2"
        else
            deploy_all
        fi
        ;;
    status)
        run_on_proxies "systemctl status arp-collector --no-pager 2>/dev/null | head -5 || echo '服务未安装'" "${2:-}"
        ;;
    start)
        run_on_proxies "systemctl start arp-collector && echo '已启动'" "${2:-}"
        ;;
    stop)
        run_on_proxies "systemctl stop arp-collector && echo '已停止'" "${2:-}"
        ;;
    restart)
        run_on_proxies "systemctl restart arp-collector && echo '已重启'" "${2:-}"
        ;;
    logs)
        if [[ -z "${2:-}" ]]; then
            log_error "请指定 proxy 名称"
            exit 1
        fi
        proxy_ip=${PROXIES[$2]}
        ssh "${SSH_USER}@${proxy_ip}" "journalctl -u arp-collector -n 50 --no-pager"
        ;;
    test)
        if [[ -z "${2:-}" ]]; then
            log_error "请指定 proxy 名称"
            exit 1
        fi
        test_collect "$2"
        ;;
    list)
        list_proxies
        ;;
    *)
        usage
        ;;
esac
