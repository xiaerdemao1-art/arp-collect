#!/bin/bash
# =============================================================================
# install.sh - arp-collect 一键安装脚本 / one-shot installer
#
# 用法 / Usage:
#   sudo ./install.sh server   [--prefix /opt/arp-collect]
#   sudo ./install.sh collector [--prefix /opt/arp-collect] [--proxy <name>] [--server-url <url>]
#   sudo ./install.sh uninstall [server|collector]
#
# 装完后只需编辑配置文件并重启：
# After install, edit the config and restart:
#   sudo vi /opt/arp-collect/config/config.yaml
#   sudo systemctl restart arp-server   # 或 arp-collector
# =============================================================================

set -e

# ---------- 默认值 / defaults ----------
INSTALL_PREFIX="${PREFIX:-/opt/arp-collect}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
SERVICE_USER="arp"
ENV_FILE="/etc/arp-collect/env"

# 颜色 / colors
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1" >&2; }

usage() {
    cat <<EOF
arp-collect installer

Usage:
  sudo ./install.sh server   [--prefix DIR]
  sudo ./install.sh collector [--prefix DIR] [--proxy NAME] [--server-url URL]
  sudo ./install.sh uninstall [server|collector]

Options:
  --prefix DIR       安装目录 (default: /opt/arp-collect)
  --proxy NAME       collector 节点名称 (写入配置，default: proxy1)
  --server-url URL   Server API 地址 (default: http://127.0.0.1:19527)
  --binary PATH      指定二进制路径 (default: 自动在 ../bin/ 查找)

Examples:
  sudo ./install.sh server
  sudo ./install.sh collector --proxy gz-1 --server-url http://10.0.0.5:19527
  sudo ./install.sh uninstall collector
EOF
    exit 1
}

# ---------- 参数解析 / arg parsing ----------
ROLE="${1:-}"
shift || true
PROXY_NAME="proxy1"
SERVER_URL="http://127.0.0.1:19527"
BINARY_PATH=""

[[ -z "$ROLE" || "$ROLE" == "-h" || "$ROLE" == "--help" ]] && usage

while [[ $# -gt 0 ]]; do
    case "$1" in
        --prefix)     INSTALL_PREFIX="$2"; shift 2 ;;
        --proxy)      PROXY_NAME="$2"; shift 2 ;;
        --server-url) SERVER_URL="$2"; shift 2 ;;
        --binary)     BINARY_PATH="$2"; shift 2 ;;
        *) error "未知参数: $1"; usage ;;
    esac
done

if [[ "$EUID" -ne 0 ]]; then
    error "请用 root 运行 (try: sudo $0 $ROLE)"
    exit 1
fi

# ---------- 工具函数 / helpers ----------
ensure_user() {
    if id "$SERVICE_USER" &>/dev/null; then
        info "用户 $SERVICE_USER 已存在"
    else
        info "创建用户 $SERVICE_USER"
        useradd -r -s /sbin/nologin "$SERVICE_USER"
    fi
}

ensure_dirs() {
    info "创建目录 $INSTALL_PREFIX/{bin,config}"
    mkdir -p "$INSTALL_PREFIX/bin" "$INSTALL_PREFIX/config" "$(dirname "$ENV_FILE")"
}

pick_binary() {
    if [[ -n "$BINARY_PATH" ]]; then return; fi
    case "$ROLE" in
        server)    BINARY_PATH="$PROJECT_DIR/bin/arp_server" ;;
        collector) BINARY_PATH="$PROJECT_DIR/bin/arp_collector" ;;
    esac
}

install_config_template() {
    local dst="$INSTALL_PREFIX/config/config.yaml"
    local src="$PROJECT_DIR/config/config.example.yaml"
    if [[ -f "$dst" ]]; then
        info "配置已存在，保留不覆盖: $dst"
    else
        info "写入配置模板: $dst"
        cp "$src" "$dst"
        # 为 collector 模式预填 proxy_name / server_url，方便用户最少改动
        if [[ "$ROLE" == "collector" ]]; then
            sed -i "s|^  mode: \"standalone\"|  mode: \"collector\"|" "$dst"
            sed -i "s|^  proxy_name: \"proxy1\"|  proxy_name: \"$PROXY_NAME\"|" "$dst"
            sed -i "s|^  server_url: \"http://192.0.2.40:19527\"|  server_url: \"$SERVER_URL\"|" "$dst"
        fi
    fi
}

write_env_file() {
    if [[ -f "$ENV_FILE" ]]; then
        info "环境文件已存在，保留不覆盖: $ENV_FILE"
        return
    fi
    info "写入秘密环境文件: $ENV_FILE"
    cat > "$ENV_FILE" <<EOF
# arp-collect 秘密环境变量 (chmod 600, owner $SERVICE_USER)
# 取消注释并填入真实值；也可通过 config.yaml 的 \${VAR} 语法引用
# ARP_NETBOX_TOKEN=
# ARP_POSTGRES_PASSWORD=
# ARP_SNMP_COMMUNITY=
# ARP_SERVER_AUTHTOKEN=
# ARP_COLLECTOR_AUTHTOKEN=
# ARP_SERVER_WEBAUTH_PASSWORD=
EOF
    chmod 600 "$ENV_FILE"
}

generate_unit() {
    local svc bin
    case "$ROLE" in
        server)
            svc="arp-server"
            bin="$INSTALL_PREFIX/bin/arp_server"
            cat > "/etc/systemd/system/${svc}.service" <<EOF
[Unit]
Description=ARP Collect Server
After=network.target postgresql.service
Wants=network-online.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_USER
WorkingDirectory=$INSTALL_PREFIX
EnvironmentFile=-$ENV_FILE
ExecStart=$bin -config $INSTALL_PREFIX/config/config.yaml
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=arp-server

[Install]
WantedBy=multi-user.target
EOF
            ;;
        collector)
            svc="arp-collector"
            bin="$INSTALL_PREFIX/bin/arp_collector"
            cat > "/etc/systemd/system/${svc}.service" <<EOF
[Unit]
Description=ARP Collector Agent ($PROXY_NAME)
After=network.target
Wants=network-online.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_USER
WorkingDirectory=$INSTALL_PREFIX
EnvironmentFile=-$ENV_FILE
ExecStart=$bin -config $INSTALL_PREFIX/config/config.yaml -mode collector
Restart=always
RestartSec=30
StandardOutput=journal
StandardError=journal
SyslogIdentifier=arp-collector

[Install]
WantedBy=multi-user.target
EOF
            ;;
    esac
    info "生成 systemd unit: ${svc}.service"
}

enable_start() {
    local svc
    [[ "$ROLE" == "server" ]] && svc="arp-server" || svc="arp-collector"
    info "启用并启动 $svc"
    systemctl daemon-reload
    systemctl enable "$svc"
    systemctl restart "$svc"
    sleep 2
    systemctl --no-pager --lines=3 status "$svc" || true
}

print_next_steps() {
    local svc
    [[ "$ROLE" == "server" ]] && svc="arp-server" || svc="arp-collector"
    echo
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}✅ $ROLE 安装完成${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo "下一步 / Next steps:"
    echo "  1. 编辑配置 / edit config:"
    echo "       sudo vi $INSTALL_PREFIX/config/config.yaml"
    echo "  2. 填入秘密 / set secrets (取消注释 $ENV_FILE 里的变量)"
    echo "       sudo vi $ENV_FILE"
    echo "  3. 重启服务 / restart:"
    echo "       sudo systemctl restart $svc"
    echo
    echo "常用命令 / useful:"
    echo "  systemctl status $svc"
    echo "  journalctl -u $svc -f"
}

# ---------- 安装主流程 / install ----------
do_install() {
    pick_binary
    if [[ ! -f "$BINARY_PATH" ]]; then
        error "二进制不存在: $BINARY_PATH"
        info "请先编译: cd $PROJECT_DIR && make build-static"
        exit 1
    fi

    ensure_user
    ensure_dirs

    info "拷贝二进制 -> $INSTALL_PREFIX/bin/"
    cp "$BINARY_PATH" "$INSTALL_PREFIX/bin/"

    install_config_template
    write_env_file
    generate_unit
    chown -R "$SERVICE_USER":"$SERVICE_USER" "$INSTALL_PREFIX" "$ENV_FILE"

    enable_start
    print_next_steps
}

# ---------- 卸载 / uninstall ----------
do_uninstall() {
    local role="${1:-}"
    if [[ -z "$role" ]]; then
        error "请指定要卸载的角色: uninstall server|collector"
        usage
    fi
    local svc bin
    case "$role" in
        server)    svc="arp-server";    bin="arp_server" ;;
        collector) svc="arp-collector"; bin="arp_collector" ;;
        *) error "未知角色: $role (server|collector)"; usage ;;
    esac

    info "停止并禁用 $svc"
    systemctl stop "$svc" 2>/dev/null || true
    systemctl disable "$svc" 2>/dev/null || true
    rm -f "/etc/systemd/system/${svc}.service"
    systemctl daemon-reload

    info "备份配置后删除二进制"
    [[ -f "$INSTALL_PREFIX/config/config.yaml" ]] && cp -n "$INSTALL_PREFIX/config/config.yaml" "$INSTALL_PREFIX/config/config.yaml.bak.$(date +%s)" 2>/dev/null || true
    rm -f "$INSTALL_PREFIX/bin/$bin"

    echo -e "${GREEN}✅ $role 已卸载（配置保留在 $INSTALL_PREFIX/config/）${NC}"
    echo "如需彻底清理: sudo rm -rf $INSTALL_PREFIX && sudo userdel $SERVICE_USER"
}

# ---------- 入口 / entry ----------
case "$ROLE" in
    server|collector) do_install ;;
    uninstall)        do_uninstall "$@" ;;
    *) error "未知命令: $ROLE"; usage ;;
esac
