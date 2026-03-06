package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/miekg/dns"
)

// ECHConfig 缓存的 ECH 配置
type ECHConfig struct {
	ECHConfig []byte // ECH 配置数据 (ECHConfigList)
	ExpiresAt time.Time
}

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// 固定测试 IP - Cloudflare
const TEST_IP = "162.159.1.180"

func main() {
	fmt.Println("========================================")
	fmt.Println("ECH HTTPS 连接测试")
	fmt.Println("========================================")

	// 1. 获取 ECH 配置
	ech := fetchECHConfig()
	if ech == nil {
		fmt.Println("\n获取 ECH 配置失败!")
		return
	}

	fmt.Printf("\n成功获取 ECH 配置!\n")
	fmt.Printf("ECH 配置长度: %d bytes\n", len(ech.ECHConfig))
	fmt.Printf("ECH 配置 (hex): %x\n", ech.ECHConfig)

	// 2. 测试使用 ECH 连接到 linux.do
	fmt.Println("\n========================================")
	fmt.Println("测试: 使用 ECH 连接 linux.do")
	fmt.Println("========================================")

	testDomain := "linux.do"
	targetAddr := net.JoinHostPort(TEST_IP, "443")

	fmt.Printf("目标地址: %s\n", targetAddr)
	fmt.Printf("目标域名: %s\n", testDomain)
	fmt.Printf("ECH 配置: %d bytes\n\n", len(ech.ECHConfig))

	// 使用 ECH 连接
	testECHConnect(targetAddr, testDomain, ech.ECHConfig)
}

// fetchECHConfig 从 Cloudflare 获取 ECH 配置
func fetchECHConfig() *ECHConfig {
	fmt.Println("\n[1] 从 crypto.cloudflare.com 获取 ECH 配置...")

	msg := new(dns.Msg)
	msg.SetQuestion("crypto.cloudflare.com.", dns.TypeHTTPS)
	msg.RecursionDesired = true

	data, err := msg.Pack()
	if err != nil {
		fmt.Printf("打包 DNS 查询失败: %v\n", err)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", "https://dns.alidns.com/dns-query", bytes.NewReader(data))
	if err != nil {
		fmt.Printf("创建 HTTP 请求失败: %v\n", err)
		return nil
	}

	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("DoH 返回错误: HTTP %d\n", resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("读取响应失败: %v\n", err)
		return nil
	}

	dnsResp := new(dns.Msg)
	if err := dnsResp.Unpack(body); err != nil {
		fmt.Printf("解析 DNS 响应失败: %v\n", err)
		return nil
	}

	// 提取 ECH 配置
	for _, ans := range dnsResp.Answer {
		if https, ok := ans.(*dns.HTTPS); ok {
			for _, opt := range https.Value {
				if ech, ok := opt.(*dns.SVCBECHConfig); ok && len(ech.ECH) > 0 {
					return &ECHConfig{
						ECHConfig: ech.ECH,
						ExpiresAt: time.Now().Add(30 * time.Minute),
					}
				}
			}
		}
	}

	return nil
}

// testECHConnect 使用 ECH 连接 (Go 1.26 API)
func testECHConnect(addr, domain string, echConfig []byte) {
	fmt.Println("[1] 尝试 TCP 连接...")

	// 先建立 TCP 连接
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		fmt.Printf("TCP 连接失败: %v\n", err)
		return
	}
	defer conn.Close()

	fmt.Printf("TCP 连接成功!\n")
	fmt.Printf("  本地: %s -> 远程: %s\n", conn.LocalAddr(), conn.RemoteAddr())

	fmt.Println("\n[2] 尝试 ECH TLS 握手...")

	// 创建 TLS 配置，启用 ECH (Go 1.26 API)
	config := &tls.Config{
		ServerName:                     domain,
		EncryptedClientHelloConfigList: echConfig,
	}

	tlsConn := tls.Client(conn, config)
	if err := tlsConn.Handshake(); err != nil {
		fmt.Printf("ECH TLS 握手失败: %v\n", err)
		fmt.Println("\n可能原因:")
		fmt.Println("  - 目标服务器不支持 ECH")
		fmt.Println("  - ECH 配置已过期")
		fmt.Println("  - 服务器拒绝了 ECH")
		return
	}
	defer tlsConn.Close()

	fmt.Printf("ECH TLS 握手成功!\n")
	state := tlsConn.ConnectionState()
	fmt.Printf("  协议版本: %s\n", tlsVersionName(state.Version))
	fmt.Printf("  密码套件: %s\n", tls.CipherSuiteName(state.CipherSuite))
	fmt.Printf("  服务器名称: %s\n", state.ServerName)

	if len(state.PeerCertificates) > 0 {
		cert := state.PeerCertificates[0]
		fmt.Printf("  证书域名: %s\n", cert.Subject.CommonName)
		fmt.Printf("  证书组织: %v\n", cert.Subject.Organization)
	}

	// 发送 HTTP 请求
	fmt.Println("\n[3] 发送 HTTP 请求...")
	req := fmt.Sprintf("GET / HTTP/1.1\r\nHost: %s\r\nUser-Agent: ECH-Test/1.0\r\nConnection: close\r\n\r\n", domain)
	if _, err := tlsConn.Write([]byte(req)); err != nil {
		fmt.Printf("发送请求失败: %v\n", err)
		return
	}

	// 读取响应
	buf := make([]byte, 4096)
	n, err := tlsConn.Read(buf)
	if err != nil {
		fmt.Printf("读取响应失败: %v\n", err)
		return
	}

	fmt.Printf("\n收到响应 (%d bytes):\n", n)
	fmt.Println(string(buf[:min(n, 500)]))
	if n > 500 {
		fmt.Println("... (截断)")
	}

	fmt.Println("\n✓ ECH 连接测试成功!")
}

func tlsVersionName(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("Unknown (0x%04x)", version)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
