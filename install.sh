#!/bin/bash
#
# Port-Shaper Linux 一键安装脚本
# 动态端口代理方案
#

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# 默认配置
GITHUB_REPO="https://github.com/coolapijust/Port-Shaper.git"
INSTALL_DIR="/opt/port-shaper"
CONFIG_DIR="/etc/port-shaper"
SERVICE_NAME="port-shaper"

# 日志函数
log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# 检查 root 权限
check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "请使用 root 权限运行此脚本"
        exit 1
    fi
}

# 检查系统类型
check_system() {
    if [[ -f /etc/os-release ]]; then
        . /etc/os-release
        OS=$NAME
        VERSION=$VERSION_ID
    else
        log_error "无法识别操作系统"
        exit 1
    fi
    log_info "检测到系统: $OS $VERSION"
}

# 安装依赖
install_dependencies() {
    log_info "安装依赖..."
    
    if command -v apt-get &> /dev/null; then
        apt-get update
        apt-get install -y curl wget git
    elif command -v yum &> /dev/null; then
        yum install -y curl wget git
    elif command -v dnf &> /dev/null; then
        dnf install -y curl wget git
    else
        log_warn "未知的包管理器，请手动安装 curl, wget, git"
    fi
    
    log_success "依赖安装完成"
}

# 安装 Go
install_go() {
    if command -v go &> /dev/null; then
        GO_VERSION=$(go version | awk '{print $3}')
        log_info "Go 已安装: $GO_VERSION"
        return 0
    fi
    
    log_info "安装 Go..."
    
    # 下载并安装 Go
    GO_VERSION="1.21.5"
    wget -q "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -O /tmp/go.tar.gz
    tar -C /usr/local -xzf /tmp/go.tar.gz
    rm /tmp/go.tar.gz
    
    # 设置环境变量
    export PATH=$PATH:/usr/local/go/bin
    echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/go.sh
    
    log_success "Go 安装完成"
}

# 获取服务器 IP
get_server_ip() {
    log_info "获取服务器 IP..."
    
    # 方法1: 从默认网卡获取（绕过 WARP）
    DEFAULT_IFACE=$(ip -4 route show default | awk '{print $5}' | head -n1)
    if [[ -n "$DEFAULT_IFACE" ]]; then
        SERVER_IP=$(ip -4 addr show "$DEFAULT_IFACE" 2>/dev/null | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -n1)
    fi
    
    # 方法2: 从所有网卡中查找公网 IP
    if [[ -z "$SERVER_IP" ]]; then
        SERVER_IP=$(ip -4 addr show | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | grep -vE '^(10\.|172\.(1[6-9]|2[0-9]|3[01])\.|192\.168\.|127\.)' | head -n1)
    fi
    
    # 方法3: 使用 hostname
    if [[ -z "$SERVER_IP" ]]; then
        SERVER_IP=$(hostname -I 2>/dev/null | awk '{print $1}')
    fi
    
    # 方法4: 通过网络请求（可能被 WARP 影响）
    if [[ -z "$SERVER_IP" ]]; then
        SERVER_IP=$(curl -s -4 --interface "$DEFAULT_IFACE" https://api.ip.sb/ip 2>/dev/null || \
                    curl -s -4 https://api.ipify.org 2>/dev/null)
    fi
    
    if [[ -z "$SERVER_IP" ]]; then
        log_error "无法自动获取服务器 IP，请手动设置"
        exit 1
    fi
    
    log_info "检测到服务器 IP: $SERVER_IP"
}

# 克隆和编译项目
build_project() {
    log_info "克隆项目..."
    
    # 清理旧版本
    if [[ -d "$INSTALL_DIR" ]]; then
        log_warn "发现已存在的安装目录，备份中..."
        mv "$INSTALL_DIR" "${INSTALL_DIR}.bak.$(date +%s)"
    fi
    
    # 克隆项目
    git clone --depth 1 "$GITHUB_REPO" "$INSTALL_DIR" 2>/dev/null || {
        log_warn "Git 克隆失败，使用本地构建..."
        mkdir -p "$INSTALL_DIR"
    }
    
    log_info "编译项目..."
    cd "$INSTALL_DIR"
    
    # 设置 Go 环境
    export PATH=$PATH:/usr/local/go/bin
    export GOPROXY=https://proxy.golang.org,direct
    
    # 编译
    go build -o port-shaper ./cmd/shaper
    
    if [[ ! -f "$INSTALL_DIR/port-shaper" ]]; then
        log_error "编译失败"
        exit 1
    fi
    
    log_success "编译完成"
}

# 创建配置文件
create_config() {
    log_info "创建配置文件..."
    
    mkdir -p "$CONFIG_DIR"
    
    cat > "$CONFIG_DIR/env" << EOF
# Port-Shaper 环境变量配置

# 服务器公网 IP（必需）
SHAPER_SERVER_IP=$SERVER_IP

# DoH 服务端口
SHAPER_DOH_PORT=443

# DoH 路径
SHAPER_DOH_PATH=/dns-query

# 动态端口范围
SHAPER_PORT_START=10000
SHAPER_PORT_END=65535

# 端口分配 TTL
SHAPER_PORT_TTL=5m

# 记录 TTL
SHAPER_RECORD_TTL=300

# TLS 配置
SHAPER_ENABLE_TLS=false
# SHAPER_TLS_CERT=/path/to/cert.pem
# SHAPER_TLS_KEY=/path/to/key.pem

# AutoCert 配置（Let's Encrypt）
# SHAPER_AUTO_CERT=true
# SHAPER_DOH_DOMAIN=doh.example.com
# SHAPER_AUTO_CERT_EMAIL=admin@example.com
# SHAPER_AUTO_CERT_DIR=/etc/port-shaper/certs
EOF
    
    chmod 600 "$CONFIG_DIR/env"
    log_success "配置文件创建完成"
}

# 创建 systemd 服务
create_service() {
    log_info "创建 systemd 服务..."
    
    cat > "/etc/systemd/system/${SERVICE_NAME}.service" << EOF
[Unit]
Description=Port-Shaper Dynamic Port Proxy
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=$INSTALL_DIR
EnvironmentFile=$CONFIG_DIR/env
ExecStart=$INSTALL_DIR/port-shaper
ExecReload=/bin/kill -HUP \$MAINPID
Restart=always
RestartSec=5

# 资源限制
LimitNOFILE=65535
LimitNPROC=65535

[Install]
WantedBy=multi-user.target
EOF
    
    systemctl daemon-reload
    systemctl enable "$SERVICE_NAME"
    
    log_success "服务创建完成"
}

# 配置防火墙
configure_firewall() {
    log_info "配置防火墙..."
    
    # 获取端口范围
    PORT_START=$(grep SHAPER_PORT_START "$CONFIG_DIR/env" | cut -d= -f2)
    PORT_END=$(grep SHAPER_PORT_END "$CONFIG_DIR/env" | cut -d= -f2)
    DOH_PORT=$(grep SHAPER_DOH_PORT "$CONFIG_DIR/env" | cut -d= -f2)
    
    # 配置防火墙
    if command -v ufw &> /dev/null; then
        ufw allow "$DOH_PORT/tcp"
        ufw allow "$PORT_START:$PORT_END/tcp"
        log_success "UFW 防火墙配置完成"
    elif command -v firewall-cmd &> /dev/null; then
        firewall-cmd --permanent --add-port="${DOH_PORT}/tcp"
        firewall-cmd --permanent --add-port="${PORT_START}-${PORT_END}/tcp"
        firewall-cmd --reload
        log_success "Firewalld 配置完成"
    elif command -v iptables &> /dev/null; then
        iptables -I INPUT -p tcp --dport "$DOH_PORT" -j ACCEPT
        iptables -I INPUT -p tcp --dport "$PORT_START:$PORT_END" -j ACCEPT
        log_success "iptables 配置完成"
    else
        log_warn "未检测到防火墙，请手动开放端口"
    fi
}

# 启动服务
start_service() {
    log_info "启动服务..."
    
    systemctl start "$SERVICE_NAME"
    
    sleep 2
    
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        log_success "服务启动成功"
        
        # 显示状态
        echo ""
        echo "====================================="
        systemctl status "$SERVICE_NAME" --no-pager
        echo "====================================="
    else
        log_error "服务启动失败"
        echo "查看日志: journalctl -u $SERVICE_NAME -n 20"
        exit 1
    fi
}

# 显示安装信息
show_info() {
    echo ""
    echo "====================================="
    echo "Port-Shaper 安装完成"
    echo "====================================="
    echo ""
    echo "安装目录: $INSTALL_DIR"
    echo "配置文件: $CONFIG_DIR/env"
    echo "服务名称: $SERVICE_NAME"
    echo ""
    echo "服务器 IP: $SERVER_IP"
    echo "DoH 地址: https://$SERVER_IP/dns-query"
    echo ""
    echo "常用命令:"
    echo "  查看状态: systemctl status $SERVICE_NAME"
    echo "  查看日志: journalctl -u $SERVICE_NAME -f"
    echo "  重启服务: systemctl restart $SERVICE_NAME"
    echo "  停止服务: systemctl stop $SERVICE_NAME"
    echo ""
    echo "配置文件位置: $CONFIG_DIR/env"
    echo "修改配置后请重启服务"
    echo "====================================="
}

# 卸载函数
uninstall() {
    log_warn "开始卸载 Port-Shaper..."
    
    # 停止服务
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    systemctl disable "$SERVICE_NAME" 2>/dev/null || true
    
    # 删除文件
    rm -rf "$INSTALL_DIR"
    rm -rf "$CONFIG_DIR"
    rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
    
    # 重载 systemd
    systemctl daemon-reload
    
    log_success "Port-Shaper 已卸载"
}

# 主函数
main() {
    case "${1:-install}" in
        install)
            check_root
            check_system
            install_dependencies
            install_go
            get_server_ip
            build_project
            create_config
            create_service
            configure_firewall
            start_service
            show_info
            ;;
        uninstall)
            check_root
            uninstall
            ;;
        *)
            echo "用法: $0 [install|uninstall]"
            exit 1
            ;;
    esac
}

main "$@"
