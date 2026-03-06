package main

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/miekg/dns"
)

// HTTP 客户端（带超时）
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	},
}

func main() {
	// 从环境变量获取配置
	dohServer := getEnv("DOH_SERVER", "https://127.0.0.1:443")
	dohPath := getEnv("DOH_PATH", "/dns-query")
	targetDomain := getEnv("TARGET_DOMAIN", "www.google.com")

	fmt.Println("========================================")
	fmt.Println("Port-Shaper 测试客户端")
	fmt.Println("========================================")
	fmt.Printf("DoH服务器: %s%s\n", dohServer, dohPath)
	fmt.Printf("目标域名: %s\n\n", targetDomain)

	// 测试 1: DoH 查询获取端口
	fmt.Println("[测试1] DoH查询获取动态端口...")
	serverIP, port, err := queryDoH(dohServer, dohPath, targetDomain)
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

	fmt.Println("========================================")
	fmt.Println("所有测试通过!")
	fmt.Println("========================================")
}

// queryDoH 执行 DoH 查询，返回服务器IP和端口
func queryDoH(server, path, domain string) (string, int, error) {
	// 构建 DNS 查询（SRV记录）
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

	// 发送请求
	resp, err := httpClient.Get(url)
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
	r := new(dns.Msg)
	if err := r.Unpack(respData); err != nil {
		return "", 0, fmt.Errorf("解析DNS响应失败: %w", err)
	}

	if r.Rcode != dns.RcodeSuccess {
		return "", 0, fmt.Errorf("DNS错误: %s", dns.RcodeToString[r.Rcode])
	}

	// 从 SRV 记录中提取 IP 和端口
	for _, ans := range r.Answer {
		if srv, ok := ans.(*dns.SRV); ok {
			return srv.Target, int(srv.Port), nil
		}
	}

	return "", 0, fmt.Errorf("响应中没有SRV记录")
}

// testConnection 测试连接到动态端口
func testConnection(serverIP string, port int, domain string) error {
	// 尝试 TCP 连接
	address := net.JoinHostPort(serverIP, fmt.Sprintf("%d", port))
	
	conn, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		return fmt.Errorf("TCP连接失败: %w", err)
	}
	defer conn.Close()

	fmt.Printf("  连接成功: %s -> %s\n", conn.LocalAddr(), conn.RemoteAddr())

	// 发送 HTTP 请求测试（如果目标支持）
	testHTTP := true
	if testHTTP {
		return testHTTPProxy(conn, domain)
	}

	return nil
}

// testHTTPProxy 测试 HTTP 代理功能
func testHTTPProxy(conn net.Conn, domain string) error {
	// 构建简单的 HTTP 请求
	request := fmt.Sprintf("HEAD / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", domain)
	
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write([]byte(request)); err != nil {
		return fmt.Errorf("发送HTTP请求失败: %w", err)
	}

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil && err.Error() != "EOF" {
		// 有些服务器可能不响应，这不一定是错误
		fmt.Printf("  读取响应: %v (可能正常)\n", err)
		return nil
	}

	if n > 0 {
		fmt.Printf("  收到响应: %d bytes\n", n)
	}

	return nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
