package portmanager

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// PortAllocation 端口分配记录
type PortAllocation struct {
	Port       int
	Domain     string
	CreatedAt  time.Time
	LastAccess time.Time
	AccessCount int
}

// Manager 端口管理器
type Manager struct {
	startPort int
	endPort   int
	ttl       time.Duration

	allocations map[int]*PortAllocation
	domainMap   map[string]int // domain -> port
	mu          sync.RWMutex
}

// NewManager 创建端口管理器
func NewManager(startPort, endPort int, ttl time.Duration) *Manager {
	return &Manager{
		startPort:   startPort,
		endPort:     endPort,
		ttl:         ttl,
		allocations: make(map[int]*PortAllocation),
		domainMap:   make(map[string]int),
	}
}

// Allocate 为域名分配端口
func (m *Manager) Allocate(domain string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 清理过期记录
	m.cleanup()

	// 检查是否已分配
	if port, exists := m.domainMap[domain]; exists {
		if alloc, ok := m.allocations[port]; ok {
			alloc.LastAccess = time.Now()
			alloc.AccessCount++
			return port, nil
		}
	}

	// 查找可用端口
	port := m.findAvailablePort()
	if port == 0 {
		return 0, fmt.Errorf("no available port")
	}

	// 创建分配记录
	now := time.Now()
	m.allocations[port] = &PortAllocation{
		Port:        port,
		Domain:      domain,
		CreatedAt:   now,
		LastAccess:  now,
		AccessCount: 0,
	}
	m.domainMap[domain] = port

	return port, nil
}

// GetDomainByPort 通过端口获取域名
func (m *Manager) GetDomainByPort(port int) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	alloc, exists := m.allocations[port]
	if !exists {
		return "", false
	}

	// 检查是否过期
	if time.Since(alloc.CreatedAt) > m.ttl {
		return "", false
	}

	alloc.LastAccess = time.Now()
	alloc.AccessCount++
	return alloc.Domain, true
}

// Release 释放端口
func (m *Manager) Release(port int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if alloc, exists := m.allocations[port]; exists {
		delete(m.domainMap, alloc.Domain)
		delete(m.allocations, port)
	}
}

// findAvailablePort 查找可用端口
func (m *Manager) findAvailablePort() int {
	for port := m.startPort; port <= m.endPort; port++ {
		if _, exists := m.allocations[port]; !exists {
			// 检查端口是否被系统占用
			if !m.isPortInUse(port) {
				return port
			}
		}
	}
	return 0
}

// isPortInUse 检查端口是否被占用
func (m *Manager) isPortInUse(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return true // 端口被占用
	}
	ln.Close()
	return false
}

// cleanup 清理过期记录
func (m *Manager) cleanup() {
	now := time.Now()
	for port, alloc := range m.allocations {
		if now.Sub(alloc.CreatedAt) > m.ttl {
			delete(m.domainMap, alloc.Domain)
			delete(m.allocations, port)
		}
	}
}

// GetStats 获取统计信息
func (m *Manager) GetStats() (total, expired int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	for _, alloc := range m.allocations {
		total++
		if now.Sub(alloc.CreatedAt) > m.ttl {
			expired++
		}
	}
	return
}
