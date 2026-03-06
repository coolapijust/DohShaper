package server

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// DynamicListener 动态端口监听器
type DynamicListener struct {
	port      int
	domain    string
	listener  net.Listener
	server    *Server
	mu        sync.RWMutex
	connCount int
	lastConn  time.Time
	closed    bool
}

// NewDynamicListener 创建动态监听器
func NewDynamicListener(port int, domain string, server *Server) (*DynamicListener, error) {
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on port %d: %w", port, err)
	}

	dl := &DynamicListener{
		port:     port,
		domain:   domain,
		listener: ln,
		server:   server,
		lastConn: time.Now(),
	}

	// 启动监听循环
	go dl.serve()

	// 启动空闲检查
	go dl.idleChecker()

	return dl, nil
}

// serve 监听连接
func (dl *DynamicListener) serve() {
	fmt.Printf("[DynamicListener:%d] Started for %s\n", dl.port, dl.domain)

	for {
		conn, err := dl.listener.Accept()
		if err != nil {
			dl.mu.RLock()
			closed := dl.closed
			dl.mu.RUnlock()

			if closed {
				return
			}

			fmt.Printf("[DynamicListener:%d] Accept error: %v\n", dl.port, err)
			continue
		}

		dl.mu.Lock()
		dl.connCount++
		dl.lastConn = time.Now()
		dl.mu.Unlock()

		go dl.handleConnection(conn)
	}
}

// handleConnection 处理连接
func (dl *DynamicListener) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	fmt.Printf("[DynamicListener:%d] New connection from %s -> %s\n",
		dl.port, clientConn.RemoteAddr(), dl.domain)

	// 设置超时
	clientConn.SetDeadline(time.Now().Add(30 * time.Second))

	// 连接到目标
	targetAddr := dl.resolveTarget(dl.domain)
	serverConn, err := net.DialTimeout("tcp", targetAddr, 10*time.Second)
	if err != nil {
		fmt.Printf("[DynamicListener:%d] Failed to connect to %s: %v\n",
			dl.port, targetAddr, err)
		return
	}
	defer serverConn.Close()

	// 清除超时
	clientConn.SetDeadline(time.Time{})
	serverConn.SetDeadline(time.Time{})

	fmt.Printf("[DynamicListener:%d] Connected to %s, starting relay\n",
		dl.port, targetAddr)

	// 双向转发
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(serverConn, clientConn)
		serverConn.(*net.TCPConn).CloseWrite()
	}()

	go func() {
		defer wg.Done()
		io.Copy(clientConn, serverConn)
		clientConn.(*net.TCPConn).CloseWrite()
	}()

	wg.Wait()

	dl.mu.Lock()
	if dl.connCount > 0 {
		dl.connCount--
	}
	dl.mu.Unlock()

	fmt.Printf("[DynamicListener:%d] Connection closed\n", dl.port)
}

// resolveTarget 解析目标地址
func (dl *DynamicListener) resolveTarget(domain string) string {
	// 简化处理：使用域名:443
	return fmt.Sprintf("%s:443", domain)
}

// idleChecker 空闲检查器
func (dl *DynamicListener) idleChecker() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		dl.mu.RLock()
		connCount := dl.connCount
		lastConn := dl.lastConn
		closed := dl.closed
		dl.mu.RUnlock()

		if closed {
			return
		}

		// 空闲超过5分钟且无活跃连接则关闭
		if connCount == 0 && time.Since(lastConn) > 5*time.Minute {
			fmt.Printf("[DynamicListener:%d] Idle timeout, closing\n", dl.port)
			dl.Close()
			return
		}
	}
}

// Close 关闭监听器
func (dl *DynamicListener) Close() error {
	dl.mu.Lock()
	defer dl.mu.Unlock()

	if dl.closed {
		return nil
	}

	dl.closed = true
	return dl.listener.Close()
}

// GetPort 获取端口
func (dl *DynamicListener) GetPort() int {
	return dl.port
}

// GetDomain 获取域名
func (dl *DynamicListener) GetDomain() string {
	return dl.domain
}

// IsActive 是否活跃
func (dl *DynamicListener) IsActive() bool {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	return !dl.closed && dl.connCount > 0
}
