package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"portshaper/internal/config"
	"portshaper/internal/portmanager"
	"portshaper/internal/resolver"

	"github.com/miekg/dns"
	"golang.org/x/crypto/acme/autocert"
)

// ECHConfig 缓存的 ECH 配置
type ECHConfig struct {
	ECHConfig []byte    // ECH 配置数据
	ExpiresAt time.Time
}

// Server 主服务器
type Server struct {
	cfg            *config.Config
	portManager    *portmanager.Manager
	cache          *resolver.Cache
	listeners      map[int]*DynamicListener
	listenersMu    sync.RWMutex
	dohListener    net.Listener
	pool           *sync.Pool
	echConfig      *ECHConfig
	echMu          sync.RWMutex
	httpClient     *http.Client
}

// NewServer 创建服务器
func NewServer(cfg *config.Config) *Server {
	return &Server{
		cfg:         cfg,
		portManager: portmanager.NewManager(cfg.PortRangeStart, cfg.PortRangeEnd, cfg.PortTTL),
		cache:       resolver.NewCache(time.Duration(cfg.RecordTTL) * time.Second),
		listeners:   make(map[int]*DynamicListener),
		pool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, 32*1024)
			},
		},
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Start 启动服务器
func (s *Server) Start() error {
	// 启动缓存清理
	s.cache.StartCleanup(1 * time.Minute)

	// 获取 ECH 配置
	s.fetchECHConfig()

	// 启动 ECH 配置刷新定时器（每30分钟）
	go s.startECHRefreshTimer()

	// 启动 DoH 服务
	go s.startDoH()

	fmt.Printf("[Server] Started on %s:%s (DoH) + dynamic ports %d-%d\n",
		s.cfg.ServerIP, s.cfg.DoHPort, s.cfg.PortRangeStart, s.cfg.PortRangeEnd)

	return nil
}

// fetchECHConfig 从 Cloudflare 获取 ECH 配置
func (s *Server) fetchECHConfig() {
	fmt.Println("[ECH] Fetching ECH config from Cloudflare...")

	// 查询 crypto.cloudflare.com 的 HTTPS 记录
	msg := new(dns.Msg)
	msg.SetQuestion("crypto.cloudflare.com.", dns.TypeHTTPS)
	msg.RecursionDesired = true

	data, err := msg.Pack()
	if err != nil {
		fmt.Printf("[ECH] Failed to pack DNS query: %v\n", err)
		return
	}

	// 使用 Cloudflare DoH
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", "https://dns.alidns.com/dns-query", bytes.NewReader(data))
	if err != nil {
		fmt.Printf("[ECH] Failed to create request: %v\n", err)
		return
	}

	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		fmt.Printf("[ECH] Failed to query Cloudflare: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[ECH] Cloudflare returned HTTP %d\n", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("[ECH] Failed to read response: %v\n", err)
		return
	}

	dnsResp := new(dns.Msg)
	if err := dnsResp.Unpack(body); err != nil {
		fmt.Printf("[ECH] Failed to unpack DNS response: %v\n", err)
		return
	}

	// 查找 HTTPS 记录并提取 ECH 配置
	for _, ans := range dnsResp.Answer {
		if https, ok := ans.(*dns.HTTPS); ok {
			// 提取 ECH 配置
			for _, opt := range https.Value {
				if ech, ok := opt.(*dns.SVCBECHConfig); ok && len(ech.ECH) > 0 {
					s.echMu.Lock()
					s.echConfig = &ECHConfig{
						ECHConfig: ech.ECH,
						ExpiresAt: time.Now().Add(30 * time.Minute),
					}
					s.echMu.Unlock()
					fmt.Println("[ECH] ECH config fetched successfully")
					return
				}
			}
		}
	}

	fmt.Println("[ECH] No ECH config found in response")
}

// startECHRefreshTimer 启动 ECH 配置刷新定时器
func (s *Server) startECHRefreshTimer() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.fetchECHConfig()
	}
}

// getECHRecord 获取 ECH 记录（带缓存检查）
func (s *Server) getECHRecord(domain string) dns.RR {
	s.echMu.RLock()
	defer s.echMu.RUnlock()

	if s.echConfig == nil || time.Now().After(s.echConfig.ExpiresAt) {
		return nil
	}

	// 创建新的 HTTPS 记录，使用提取的 ECH 配置
	https := &dns.HTTPS{
		SVCB: dns.SVCB{
			Hdr: dns.RR_Header{
				Name:   domain,
				Rrtype: dns.TypeHTTPS,
				Class:  dns.ClassINET,
				Ttl:    1800, // 30分钟 TTL
			},
			Priority: 1, // 默认优先级
			Target:   ".", // 使用当前域名
			Value: []dns.SVCBKeyValue{
				&dns.SVCBECHConfig{ECH: s.echConfig.ECHConfig},
			},
		},
	}
	return https
}

// startDoH 启动 DoH 服务
func (s *Server) startDoH() {
	addr := net.JoinHostPort("", s.cfg.DoHPort)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("[DoH] Failed to listen: %v\n", err)
		return
	}
	s.dohListener = listener

	// 如果启用 TLS
	if s.cfg.EnableTLS {
		if s.cfg.AutoCert {
			s.startAutoCert(listener)
		} else {
			s.startTLS(listener)
		}
		return
	}

	// 无 TLS 模式（仅用于测试）
	fmt.Printf("[DoH] Warning: Running without TLS\n")
	httpServer := &http.Server{
		Handler: s.createDoHHandler(),
	}
	httpServer.Serve(listener)
}

// startAutoCert 启动自动证书
func (s *Server) startAutoCert(listener net.Listener) {
	m := &autocert.Manager{
		Cache:      autocert.DirCache(s.cfg.AutoCertDir),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(s.cfg.DoHDomain),
		Email:      s.cfg.AutoCertEmail,
	}

	// HTTP 挑战服务器
	go func() {
		httpServer := &http.Server{
			Addr:    ":80",
			Handler: m.HTTPHandler(nil),
		}
		httpServer.ListenAndServe()
	}()

	tlsConfig := m.TLSConfig()
	tlsListener := tls.NewListener(listener, tlsConfig)

	httpServer := &http.Server{
		Handler: s.createDoHHandler(),
	}
	fmt.Printf("[DoH] AutoCert enabled for %s\n", s.cfg.DoHDomain)
	httpServer.Serve(tlsListener)
}

// startTLS 启动固定证书 TLS
func (s *Server) startTLS(listener net.Listener) {
	cert, err := tls.LoadX509KeyPair(s.cfg.TLSCert, s.cfg.TLSKey)
	if err != nil {
		fmt.Printf("[DoH] Failed to load TLS certs: %v\n", err)
		return
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
	tlsListener := tls.NewListener(listener, tlsConfig)

	httpServer := &http.Server{
		Handler: s.createDoHHandler(),
	}
	fmt.Printf("[DoH] TLS enabled\n")
	httpServer.Serve(tlsListener)
}

// createDoHHandler 创建 DoH HTTP 处理器
func (s *Server) createDoHHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 健康检查
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok","service":"port-shaper"}`))
			return
		}

		// 只处理 DoH 路径
		if r.URL.Path != s.cfg.DoHPath {
			http.NotFound(w, r)
			return
		}

		// 处理 DNS 查询
		var msg *dns.Msg
		var err error

		switch r.Method {
		case http.MethodGet:
			msg, err = s.parseDNSQueryFromURL(r)
		case http.MethodPost:
			msg, err = s.parseDNSQueryFromBody(r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// 处理查询
		response := s.processDNSQuery(msg, r)

		// 打包响应
		data, err := response.Pack()
		if err != nil {
			http.Error(w, "Failed to pack response", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/dns-message")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	})
}

// parseDNSQueryFromURL 从 URL 解析 DNS 查询
func (s *Server) parseDNSQueryFromURL(r *http.Request) (*dns.Msg, error) {
	dnsParam := r.URL.Query().Get("dns")
	if dnsParam == "" {
		return nil, fmt.Errorf("missing dns parameter")
	}

	// Base64URL 解码
	data := make([]byte, len(dnsParam))
	n, err := decodeBase64URL(dnsParam, data)
	if err != nil {
		return nil, fmt.Errorf("invalid dns parameter")
	}

	msg := new(dns.Msg)
	if err := msg.Unpack(data[:n]); err != nil {
		return nil, fmt.Errorf("invalid DNS message")
	}

	return msg, nil
}

// parseDNSQueryFromBody 从 Body 解析 DNS 查询
func (s *Server) parseDNSQueryFromBody(r *http.Request) (*dns.Msg, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body")
	}
	defer r.Body.Close()

	msg := new(dns.Msg)
	if err := msg.Unpack(body); err != nil {
		return nil, fmt.Errorf("invalid DNS message")
	}

	return msg, nil
}

// processDNSQuery 处理 DNS 查询
func (s *Server) processDNSQuery(query *dns.Msg, r *http.Request) *dns.Msg {
	response := new(dns.Msg)
	response.SetReply(query)
	response.RecursionAvailable = true

	for _, q := range query.Question {
		switch q.Qtype {
		case dns.TypeAAAA, dns.TypeA, dns.TypeSRV:
			s.handleQuery(response, q.Name, r)
		case dns.TypeHTTPS:
			s.handleHTTPSQuery(response, q.Name)
		}
	}

	return response
}

// handleHTTPSQuery 处理 HTTPS 查询
func (s *Server) handleHTTPSQuery(response *dns.Msg, domain string) {
	domain = dns.Fqdn(domain)

	// 获取 ECH 记录
	echRecord := s.getECHRecord(domain)
	if echRecord != nil {
		response.Answer = append(response.Answer, echRecord)
		fmt.Printf("[DoH] HTTPS record for %s (with ECH)\n", domain)
	} else {
		fmt.Printf("[DoH] HTTPS record for %s (no ECH available)\n", domain)
	}
}

// handleQuery 处理查询
func (s *Server) handleQuery(response *dns.Msg, domain string, r *http.Request) {
	domain = dns.Fqdn(domain)

	// 提取客户端 IP
	clientIP := s.extractClientIP(r)

	// 分配端口
	port, err := s.portManager.Allocate(domain)
	if err != nil {
		fmt.Printf("[DoH] Failed to allocate port for %s: %v\n", domain, err)
		response.Rcode = dns.RcodeServerFailure
		return
	}

	// 启动动态监听器
	if err := s.startDynamicListener(port, domain); err != nil {
		fmt.Printf("[DoH] Failed to start listener for %s:%d: %v\n", domain, port, err)
		response.Rcode = dns.RcodeServerFailure
		return
	}

	// 记录到缓存
	s.cache.Add(domain, clientIP, port)

	// 创建 SRV 记录
	srv := &dns.SRV{
		Hdr: dns.RR_Header{
			Name:   domain,
			Rrtype: dns.TypeSRV,
			Class:  dns.ClassINET,
			Ttl:    uint32(s.cfg.RecordTTL),
		},
		Priority: 0,
		Weight:   0,
		Port:     uint16(port),
		Target:   dns.Fqdn(s.cfg.ServerIP),
	}
	response.Answer = append(response.Answer, srv)

	// 添加 A 记录（服务器 IP）
	a := &dns.A{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(s.cfg.ServerIP),
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    uint32(s.cfg.RecordTTL),
		},
		A: net.ParseIP(s.cfg.ServerIP).To4(),
	}
	if a.A != nil {
		response.Extra = append(response.Extra, a)
	}

	fmt.Printf("[DoH] Allocated %s -> %s:%d (client: %s)\n",
		domain, s.cfg.ServerIP, port, clientIP)
}

// startDynamicListener 启动动态监听器
func (s *Server) startDynamicListener(port int, domain string) error {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()

	// 检查是否已存在
	if _, exists := s.listeners[port]; exists {
		return nil
	}

	// 创建新监听器
	listener, err := NewDynamicListener(port, domain, s)
	if err != nil {
		return err
	}

	s.listeners[port] = listener
	return nil
}

// extractClientIP 提取客户端 IP
func (s *Server) extractClientIP(r *http.Request) net.IP {
	// 尝试从 X-Forwarded-For 获取
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ip := net.ParseIP(xff)
		if ip != nil {
			return ip
		}
	}

	// 从 RemoteAddr 获取
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		ip := net.ParseIP(host)
		if ip != nil {
			return ip
		}
	}

	return nil
}

// decodeBase64URL Base64URL 解码
func decodeBase64URL(s string, dst []byte) (int, error) {
	// 替换 Base64URL 字符
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")

	// 填充
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}

	return base64.StdEncoding.Decode(dst, []byte(s))
}

// Stop 停止服务器
func (s *Server) Stop() {
	if s.dohListener != nil {
		s.dohListener.Close()
	}

	s.listenersMu.Lock()
	for _, listener := range s.listeners {
		listener.Close()
	}
	s.listeners = make(map[int]*DynamicListener)
	s.listenersMu.Unlock()
}
