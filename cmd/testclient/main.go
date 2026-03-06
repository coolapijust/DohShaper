package main

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/miekg/dns"
)

func main() {
	// 从环境变量获取配置
	dohServer := getEnv("DOH_SERVER", "https://127.0.0.1:443")
	dohPath := getEnv("DOH_PATH", "/dns-query")
	targetDomain := getEnv("TARGET_DOMAIN", "www.google.com")
	skipVerify := getEnvBool("SKIP_VERIFY", true)

	fmt.Println("======================================")
	fmt.Println("Port-Shaper 测试客户端")
	fmt.Println("======================================")
	fmt.Printf("DoH服务器: %s%s\n", dohServer, dohPath)
	fmt.Printf("目标域名: %s\n", targetDomain)
	fmt.Println()

	// 测试 1: DoH 查询获取端口
	fmt.Println("[测试1] DoH查询获取动态端口...")
	serverIP, port, err := queryDoH(dohServer, dohPath, targetDomain, skipVerify)
	if err != nil {
		fmt.Printf("[失败] DoH查询失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[成功] 服务器: %s, 端口: %d\n", serverIP, port)
	fmt.Println()

	// 测试 2: 连接到动态端口
	fmt.Printf("[测试2] 连接动态端口 %s:%d...\n", serverIP, port)
	if err := testConnection(serverIP, port, targetDomain); err != nil {
		fmt.Printf("[失败] 连接失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("[成功] 连接测试通过")
	fmt.Println()

	fmt.Println("======================================")
	fmt.Println("所有测试通过!")
	fmt.Println("======================================")
}

// queryDoH 执行 DoH 查询
func queryDoH(server, path, domain string, skipVerify bool) (string, int, error) {
	// 构建 DNS 查询
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), dns.TypeSRV)
	msg.RecursionDesired = true

	data, err := msg.Pack()
	if err != nil {
		return "", 0, fmt.Errorf("打包DNS查询失败: %w", err)
	}

	// Base64URL 编码
	encoded := base64.RawURLEncoding.EncodeToString(data)

	// 构建 URL
	url := fmt.Sprintf("%s%s?dns=%s", server, path, encoded)

	// 创建 HTTP 客户端
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: skipVerify,
			},
		},
	}

	// 发送请求
	resp, err := client.Get(url)
	if err != nil {
		return "", 0, fmt.Errorf("HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("HTTP错误 %d: %s", resp.StatusCode, string(body))
	}

	// 读取响应
	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("读取响应失败: %w", err)
	}

	// 解析 DNS 响应
	respMsg := new(dns.Msg)
	if err := respMsg.Unpack(respData); err != nil {
		return "", 0, fmt.Errorf("解析DNS响应失败: %w", err)
	}

	// 提取 SRV 记录
	var serverIP string
	var port int

	for _, rr := range respMsg.Answer {
		if srv, ok := rr.(*dns.SRV); ok {
			serverIP = strings.TrimSuffix(srv.Target, ".")
			port = int(srv.Port)
			break
		}
	}

	if serverIP == "" || port == 0 {
		return "", 0, fmt.Errorf("响应中没有找到SRV记录")
	}

	return serverIP, port, nil
}

// testConnection 测试连接到动态端口
func testConnection(serverIP string, port int, domain string) error {
	addr := fmt.Sprintf("%s:%d", serverIP, port)

	// 建立 TCP 连接
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("TCP连接失败: %w", err)
	}
	defer conn.Close()

	fmt.Printf("[调试] TCP连接成功: %s\n", addr)

	// 发送 TLS Client Hello
	config := &tls.Config{
		ServerName:         domain,
		InsecureSkipVerify: true,
	}

	tlsConn := tls.Client(conn, config)
	tlsConn.SetDeadline(time.Now().Add(10 * time.Second))

	if err := tlsConn.Handshake(); err != nil {
		return fmt.Errorf("TLS握手失败: %w", err)
	}
	defer tlsConn.Close()

	fmt.Printf("[调试] TLS握手成功，协议: %s\n", tlsConn.ConnectionState().Version)

	// 发送 HTTP 请求
	httpReq := fmt.Sprintf("HEAD / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", domain)
	if _, err := tlsConn.Write([]byte(httpReq)); err != nil {
		return fmt.Errorf("发送HTTP请求失败: %w", err)
	}

	// 读取响应
	buf := make([]byte, 1024)
	n, err := tlsConn.Read(buf)
	if err != nil && err != io.EOF {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	response := string(buf[:n])
	if strings.Contains(response, "HTTP/1.1") || strings.Contains(response, "HTTP/2") {
		fmt.Printf("[调试] HTTP响应: %s\n", strings.Split(response, "\r\n")[0])
		return nil
	}

	return fmt.Errorf("无效的HTTP响应")
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	return strings.ToLower(val) == "true" || val == "1"
}
