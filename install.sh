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
    
    # 方法1: 从默认路由获取网卡（优先 IPv6，其次 IPv4）
    DEFAULT_IFACE=$(ip -6 route show default 2>/dev/null | grep -oP 'dev\s+\K[^\s]+' | head -n1)
    if [[ -z "$DEFAULT_IFACE" ]]; then
        DEFAULT_IFACE=$(ip -4 route show default 2>/dev/null | grep -oP 'dev\s+\K[^\s]+' | head -n1)
    fi
    
    # 方法2: 如果失败，从所有网卡中找有公网 IP 的
    if [[ -z "$DEFAULT_IFACE" ]]; then
        # 获取所有网卡名称
        for iface in $(ip link show | grep -oP '^\d+:\s+\K[^:@]+' | grep -v lo); do
            # 优先检查 IPv6 公网地址
            ip_addr=$(ip -6 addr show dev "$iface" 2>/dev/null | grep "scope global" | grep -v "temporary" | head -n1 | awk '{print $2}' | cut -d'/' -f1)
            if [[ -n "$ip_addr" ]] && [[ ! "$ip_addr" =~ ^fe80: ]] && [[ ! "$ip_addr" =~ ^::1$ ]]; then
                DEFAULT_IFACE="$iface"
                break
            fi
            # 其次检查 IPv4 公网地址
            ip_addr=$(ip -4 addr show dev "$iface" 2>/dev/null | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -n1)
            if [[ -n "$ip_addr" ]] && [[ ! "$ip_addr" =~ ^(10\.|172\.(1[6-9]|2[0-9]|3[01])\.|192\.168\.|127\.) ]]; then
                DEFAULT_IFACE="$iface"
                break
            fi
        done
    fi
    
    if [[ -z "$DEFAULT_IFACE" ]]; then
        log_error "无法找到默认网卡"
        exit 1
    fi
    
    log_info "默认网卡: $DEFAULT_IFACE"
    
    # 优先获取 IPv6 地址
    SERVER_IP=$(ip -6 addr show dev "$DEFAULT_IFACE" 2>/dev/null | grep "scope global" | grep -v "temporary" | head -n1 | awk '{print $2}' | cut -d'/' -f1)
    
    # 如果没有 IPv6，获取 IPv4
    if [[ -z "$SERVER_IP" ]]; then
        SERVER_IP=$(ip -4 addr show dev "$DEFAULT_IFACE" 2>/dev/null | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -n1)
    fi
    
    if [[ -z "$SERVER_IP" ]]; then
        log_error "网卡 $DEFAULT_IFACE 没有 IP 地址"
        exit 1
    fi
    
    log_info "检测到服务器 IP: $SERVER_IP"
}

# 下载预编译二进制
build_project() {
    log_info "下载 Port-Shaper..."
    
    # 清理旧版本
    if [[ -d "$INSTALL_DIR" ]]; then
        log_warn "发现已存在的安装目录，备份中..."
        mv "$INSTALL_DIR" "${INSTALL_DIR}.bak.$(date +%s)"
    fi
    
    # 创建目录
    mkdir -p "$INSTALL_DIR"
    cd "$INSTALL_DIR"
    
    # 下载预编译二进制（从 GitHub Releases）
    DOWNLOAD_URL="https://github.com/coolapijust/DohShaper/releases/download/1.0/port-shaper-linux-amd64"
    
    log_info "下载二进制文件..."
    if ! curl -fsSL -L "$DOWNLOAD_URL" -o port-shaper; then
        log_error "下载失败，请检查网络连接"
        log_info "手动下载地址: $DOWNLOAD_URL"
        exit 1
    fi
    
    # 添加执行权限
    chmod +x port-shaper
    
    if [[ ! -f "$INSTALL_DIR/port-shaper" ]]; then
        log_error "下载失败"
        exit 1
    fi
    
    log_success "下载完成"
}

# 配置变量（全局）
DOH_PATH="/dns-query"
ENABLE_TLS="false"
AUTO_CERT="false"
doh_domain=""
cert_email=""
tls_cert=""
tls_key=""

# 交互式配置向导
configure_wizard() {
    log_info "配置向导..."
    
    # 从 /dev/tty 读取（支持管道模式）
    exec 3<&0
    exec 0</dev/tty
    
    # DoH 路径
    echo ""
    read -rp "请输入 DoH 路径 [默认: /dns-query]: " input_path
    DOH_PATH=${input_path:-/dns-query}
    
    # 域名（用于 AutoCert）
    echo ""
    read -rp "请输入 DoH 域名（用于 Let's Encrypt 证书，可选）: " doh_domain
    
    # 证书配置
    if [[ -n "$doh_domain" ]]; then
        echo ""
        echo "证书选项:"
        echo "1) 使用 Let's Encrypt 自动申请证书 (AutoCert)"
        echo "2) 使用已有证书文件"
        echo "3) 不使用 TLS（仅用于测试）"
        read -rp "请选择 [1]: " cert_choice
        cert_choice=${cert_choice:-1}
        
        case $cert_choice in
            1)
                ENABLE_TLS="true"
                AUTO_CERT="true"
                read -rp "请输入邮箱地址（用于 Let's Encrypt）: " cert_email
                ;;
            2)
                ENABLE_TLS="true"
                AUTO_CERT="false"
                read -rp "请输入证书文件路径: " tls_cert
                read -rp "请输入密钥文件路径: " tls_key
                ;;
            *)
                ENABLE_TLS="false"
                AUTO_CERT="false"
                ;;
        esac
    else
        # 没有域名，询问是否使用自定义证书
        echo ""
        read -rp "是否使用自定义 TLS 证书? (y/N): " use_tls
        if [[ "$use_tls" =~ ^[Yy]$ ]]; then
            ENABLE_TLS="true"
            AUTO_CERT="false"
            read -rp "请输入证书文件路径: " tls_cert
            read -rp "请输入密钥文件路径: " tls_key
        else
            ENABLE_TLS="false"
            AUTO_CERT="false"
        fi
    fi
    
    # 恢复 stdin
    exec 0<&3
    
    # 导出变量供后续使用
    export DOH_PATH ENABLE_TLS AUTO_CERT doh_domain cert_email tls_cert tls_key
}

# 创建配置文件
create_config() {
    log_info "创建配置文件..."
    
    # 运行配置向导
    configure_wizard
    
    mkdir -p "$CONFIG_DIR"
    
    cat > "$CONFIG_DIR/env" << EOF
# Port-Shaper 环境变量配置

# 服务器公网 IP（必需）
SHAPER_SERVER_IP=$SERVER_IP

# DoH 服务端口
SHAPER_DOH_PORT=443

# DoH 路径
SHAPER_DOH_PATH=$DOH_PATH

# 动态端口范围
SHAPER_PORT_START=10000
SHAPER_PORT_END=65535

# 端口分配 TTL
SHAPER_PORT_TTL=5m

# 记录 TTL
SHAPER_RECORD_TTL=300

# TLS 配置
SHAPER_ENABLE_TLS=$ENABLE_TLS
EOF

    # 添加 TLS 证书配置
    if [[ "$ENABLE_TLS" == "true" ]]; then
        if [[ "$AUTO_CERT" == "true" ]]; then
            cat >> "$CONFIG_DIR/env" << EOF

# AutoCert 配置（Let's Encrypt）
SHAPER_AUTO_CERT=true
SHAPER_DOH_DOMAIN=$doh_domain
SHAPER_AUTO_CERT_EMAIL=$cert_email
SHAPER_AUTO_CERT_DIR=/etc/port-shaper/certs
EOF
        else
            cat >> "$CONFIG_DIR/env" << EOF

# TLS 证书文件
SHAPER_TLS_CERT=$tls_cert
SHAPER_TLS_KEY=$tls_key
EOF
        fi
    fi
    
    chmod 600 "$CONFIG_DIR/env"
    log_success "配置文件创建完成"
    
    # 如果启用了 AutoCert，立即申请证书
    if [[ "$AUTO_CERT" == "true" ]]; then
        request_certificate
    fi
}

# 申请 Let's Encrypt 证书
request_certificate() {
    log_info "申请 Let's Encrypt 证书..."
    
    # 检查是否已安装 certbot
    if ! command -v certbot &> /dev/null; then
        log_info "安装 certbot..."
        if command -v apt-get &> /dev/null; then
            apt-get update -qq
            apt-get install -y -qq certbot
        elif command -v yum &> /dev/null; then
            yum install -y certbot
        else
            log_error "无法自动安装 certbot，请手动安装"
            exit 1
        fi
    fi
    
    # 创建证书目录
    mkdir -p /etc/port-shaper/certs
    
    # 申请证书（使用 standalone 模式，需要暂时停止服务）
    log_info "正在为 $doh_domain 申请证书..."
    
    # 确保 80 端口可用
    if lsof -Pi :80 -sTCP:LISTEN -t >/dev/null 2>&1; then
        log_warn "80 端口被占用，尝试使用 webroot 模式..."
        # 创建临时 webroot
        mkdir -p /tmp/certbot-webroot
        certbot certonly --webroot -w /tmp/certbot-webroot \
            -d "$doh_domain" \
            --email "$cert_email" \
            --agree-tos \
            --non-interactive \
            --quiet
    else
        certbot certonly --standalone \
            -d "$doh_domain" \
            --email "$cert_email" \
            --agree-tos \
            --non-interactive \
            --quiet
    fi
    
    if [[ $? -eq 0 ]]; then
        # 创建证书链接
        ln -sf "/etc/letsencrypt/live/$doh_domain/fullchain.pem" /etc/port-shaper/certs/cert.pem
        ln -sf "/etc/letsencrypt/live/$doh_domain/privkey.pem" /etc/port-shaper/certs/key.pem
        
        # 更新配置文件中的证书路径
        sed -i 's|^# SHAPER_TLS_CERT=.*|SHAPER_TLS_CERT=/etc/port-shaper/certs/cert.pem|' "$CONFIG_DIR/env"
        sed -i 's|^# SHAPER_TLS_KEY=.*|SHAPER_TLS_KEY=/etc/port-shaper/certs/key.pem|' "$CONFIG_DIR/env"
        
        # 设置自动续期钩子
        mkdir -p /etc/letsencrypt/renewal-hooks/deploy
        cat > /etc/letsencrypt/renewal-hooks/deploy/port-shaper.sh << 'EOF'
#!/bin/bash
# 证书续期后重启服务
systemctl restart port-shaper
EOF
        chmod +x /etc/letsencrypt/renewal-hooks/deploy/port-shaper.sh
        
        log_success "证书申请成功！"
        log_info "证书路径: /etc/port-shaper/certs/"
        log_info "自动续期已配置"
    else
        log_error "证书申请失败"
        log_info "请检查:"
        log_info "  1. 域名 $doh_domain 是否已解析到本机"
        log_info "  2. 80 端口是否可访问"
        log_info "  3. 邮箱地址 $cert_email 是否有效"
        exit 1
    fi
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
