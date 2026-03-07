package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/miekg/dns"
)

func main() {
	fmt.Println("========================================")
	fmt.Println("DoH 连接测试")
	fmt.Println("========================================")

	server := "https://play.softx.eu.org"
	path := "/dns-query"
	domain := "linux.do"

	fmt.Printf("服务器: %s\n", server)
	fmt.Printf("路径: %s\n", path)
	fmt.Printf("查询域名: %s\n\n", domain)

	// 创建自定义 HTTP 客户端，禁用 DNS 缓存
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// 强制使用原始服务器地址，不使用 DNS 解析
				if addr == "play.softx.eu.org:443" {
					addr = "162.159.1.180:443"
				}
				return net.Dial(network, addr)
			},
		},
	}

	// 测试1: GET 方法查询 AAAA 记录
	fmt.Println("[测试1] GET 方法查询 AAAA 记录...")
	testGet(client, server, path, domain, dns.TypeAAAA)

	// 测试2: GET 方法查询 SRV 记录
	fmt.Println("\n[测试2] GET 方法查询 SRV 记录...")
	testGet(client, server, path, domain, dns.TypeSRV)

	// 测试3: GET 方法查询 HTTPS 记录
	fmt.Println("\n[测试3] GET 方法查询 HTTPS 记录...")
	testGet(client, server, path, domain, dns.TypeHTTPS)

	// 测试4: POST 方法查询 AAAA 记录
	fmt.Println("\n[测试4] POST 方法查询 AAAA 记录...")
	testPost(client, server, path, domain, dns.TypeAAAA)
}

func testGet(client *http.Client, server, path, domain string, qtype uint16) {
	// 构建 DNS 查询
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), qtype)
	msg.RecursionDesired = true

	data, err := msg.Pack()
	if err != nil {
		fmt.Printf("打包失败: %v\n", err)
		return
	}

	// Base64URL 编码
	encoded := base64.RawURLEncoding.EncodeToString(data)
	fmt.Printf("DNS 查询 Base64URL: %s\n", encoded)

	// 构建 URL
	url := fmt.Sprintf("%s%s?dns=%s", server, path, encoded)
	fmt.Printf("请求 URL: %s\n", url)

	// 发送请求
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("创建请求失败: %v\n", err)
		return
	}
	req.Host = "play.softx.eu.org" // 设置 Host 头

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("HTTP 状态: %d\n", resp.StatusCode)
	fmt.Printf("Content-Type: %s\n", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("读取响应失败: %v\n", err)
		return
	}

	fmt.Printf("响应大小: %d bytes\n", len(body))

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("错误响应: %s\n", string(body))
		return
	}

	// 解析 DNS 响应
	parseDNSResponse(body)
}

func testPost(client *http.Client, server, path, domain string, qtype uint16) {
	// 构建 DNS 查询
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), qtype)
	msg.RecursionDesired = true

	data, err := msg.Pack()
	if err != nil {
		fmt.Printf("打包失败: %v\n", err)
		return
	}

	fmt.Printf("DNS 查询大小: %d bytes\n", len(data))

	// 构建请求
	url := server + path

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		fmt.Printf("创建请求失败: %v\n", err)
		return
	}
	req.Host = "play.softx.eu.org" // 设置 Host 头
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("HTTP 状态: %d\n", resp.StatusCode)
	fmt.Printf("Content-Type: %s\n", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("读取响应失败: %v\n", err)
		return
	}

	fmt.Printf("响应大小: %d bytes\n", len(body))

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("错误响应: %s\n", string(body))
		return
	}

	// 解析 DNS 响应
	parseDNSResponse(body)
}

func parseDNSResponse(data []byte) {
	msg := new(dns.Msg)
	if err := msg.Unpack(data); err != nil {
		fmt.Printf("解析 DNS 响应失败: %v\n", err)
		return
	}

	fmt.Printf("\nDNS 响应解析成功:\n")
	fmt.Printf("  响应码: %s\n", dns.RcodeToString[msg.Rcode])
	fmt.Printf("  问题数: %d\n", len(msg.Question))
	fmt.Printf("  回答数: %d\n", len(msg.Answer))
	fmt.Printf("  权威记录: %d\n", len(msg.Ns))
	fmt.Printf("  附加记录: %d\n", len(msg.Extra))

	if len(msg.Question) > 0 {
		fmt.Printf("\n  查询问题:\n")
		for i, q := range msg.Question {
			fmt.Printf("    %d: %s (类型: %s)\n", i+1, q.Name, dns.Type(q.Qtype))
		}
	}

	if len(msg.Answer) > 0 {
		fmt.Printf("\n  回答记录:\n")
		for i, ans := range msg.Answer {
			fmt.Printf("    %d: %s (类型: %s, TTL: %d)\n", i+1, ans.Header().Name, dns.Type(ans.Header().Rrtype), ans.Header().Ttl)

			switch rr := ans.(type) {
			case *dns.SRV:
				fmt.Printf("       目标: %s:%d\n", rr.Target, rr.Port)
			case *dns.HTTPS:
				fmt.Printf("       优先级: %d, 目标: %s\n", rr.Priority, rr.Target)
				for _, opt := range rr.Value {
					if _, ok := opt.(*dns.SVCBECHConfig); ok {
						fmt.Printf("       包含 ECH 配置\n")
					}
				}
			}
		}
	} else {
		fmt.Printf("\n  ⚠️  没有回答记录!\n")
	}
}
