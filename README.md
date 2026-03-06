# Port-Shaper

动态端口代理方案 - 通过 DoH 分配临时端口实现透明代理

## 工作原理

```
客户端                    服务器                    目标网站
  |                        |                          |
  |--- DoH 查询 google.com -->|                        |
  |                        | 分配端口 15000           |
  |<-- 返回 SRV: IP:15000 ----|                        |
  |                        |                          |
  |--- TCP 连接 IP:15000 ---->|                        |
  |                        |--- 连接到 google.com:443 -->|
  |                        |<-- 建立连接 --------------|
  |<-- 建立连接 ------------|                          |
  |<---> 数据转发 <------->|<---> 数据转发 <--------->|
```

## 特性

- **DoH over HTTPS**: 使用标准 DNS over HTTPS 协议
- **动态端口分配**: 每个域名分配独立端口，支持 5.5 万+ 并发
- **自动端口回收**: 5 分钟空闲自动释放
- **TLS 支持**: 支持自定义证书或 Let's Encrypt 自动证书
- **零配置客户端**: 标准 DoH 客户端即可使用

## 快速开始

### 服务器安装

```bash
# 一键安装
curl -fsSL https://raw.githubusercontent.com/coolapijust/Port-Shaper/main/install.sh | bash

# 或手动安装
git clone https://github.com/coolapijust/Port-Shaper.git
cd Port-Shaper
./install.sh
```

### 配置

编辑 `/etc/port-shaper/env`:

```bash
# 必需: 服务器公网 IP
SHAPER_SERVER_IP=1.2.3.4

# 可选配置
SHAPER_DOH_PORT=443
SHAPER_PORT_START=10000
SHAPER_PORT_END=65535
SHAPER_PORT_TTL=5m

# TLS 配置
SHAPER_ENABLE_TLS=true
SHAPER_TLS_CERT=/path/to/cert.pem
SHAPER_TLS_KEY=/path/to/key.pem

# 或 AutoCert (Let's Encrypt)
SHAPER_AUTO_CERT=true
SHAPER_DOH_DOMAIN=doh.example.com
```

### 启动服务

```bash
systemctl start port-shaper
systemctl enable port-shaper
```

## 客户端使用

### 使用测试客户端

```bash
# 设置环境变量
export DOH_SERVER="https://your-server.com"
export TARGET_DOMAIN="www.google.com"

# 运行测试
./testclient
```

### 使用标准 DoH 客户端

```bash
# 使用 cloudflared
dig @https://your-server.com/dns-query www.google.com SRV

# 使用 dog
dog @https://your-server.com/dns-query www.google.com --type SRV
```

### 编程使用

```python
import dns.resolver

# 配置 DoH 解析器
resolver = dns.resolver.Resolver()
resolver.nameservers = ['your-server.com']

# 查询 SRV 记录
answers = resolver.resolve('www.google.com', 'SRV')
for rdata in answers:
    print(f"Server: {rdata.target}, Port: {rdata.port}")
    
# 连接到返回的端口
import socket
sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
sock.connect((str(rdata.target), rdata.port))
```

## DNS 响应格式

```dns
;; QUESTION SECTION:
;www.google.com.        IN  SRV

;; ANSWER SECTION:
www.google.com.  300  IN  SRV  0 0 15000  1.2.3.4.

;; ADDITIONAL SECTION:
1.2.3.4.  300  IN  A  1.2.3.4
```

## 命令参考

```bash
# 查看状态
systemctl status port-shaper

# 查看日志
journalctl -u port-shaper -f

# 重启服务
systemctl restart port-shaper

# 停止服务
systemctl stop port-shaper

# 卸载
./install.sh uninstall
```

## 防火墙配置

确保开放以下端口：
- DoH 端口（默认 443）
- 动态端口范围（默认 10000-65535）

```bash
# UFW
ufw allow 443/tcp
ufw allow 10000:65535/tcp

# Firewalld
firewall-cmd --permanent --add-port=443/tcp
firewall-cmd --permanent --add-port=10000-65535/tcp
firewall-cmd --reload

# iptables
iptables -I INPUT -p tcp --dport 443 -j ACCEPT
iptables -I INPUT -p tcp --dport 10000:65535 -j ACCEPT
```

## 项目结构

```
portshaper/
├── cmd/
│   ├── shaper/         # 主程序
│   └── testclient/     # 测试客户端
├── internal/
│   ├── config/         # 配置管理
│   ├── portmanager/    # 端口分配管理
│   ├── resolver/       # DNS 缓存
│   └── server/         # HTTP/DoH 服务器
├── install.sh          # 安装脚本
└── README.md
```

## 与 DNS-Shaper 的区别

| 特性 | DNS-Shaper | Port-Shaper |
|------|------------|-------------|
| 方案 | IPv6 AnyIP | 动态端口 |
| 网络要求 | 需要 /64 IPv6 子网 | 仅需公网 IP |
| 兼容性 | 需要 IPv6 支持 | 纯 IPv4/IPv6 都支持 |
| 端口占用 | 固定端口 | 动态分配 |
| VPS 兼容性 | 受限 | 通用 |

## License

MIT
