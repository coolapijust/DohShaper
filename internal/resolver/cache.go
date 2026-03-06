package resolver

import (
	"net"
	"sync"
	"time"
)

// ResolveRecord 解析记录
type ResolveRecord struct {
	Domain     string
	ClientIP   net.IP
	Port       int
	CreatedAt  time.Time
	LastAccess time.Time
	AccessCount int
}

// Cache 解析缓存
type Cache struct {
	records map[string]*ResolveRecord // domain -> record
	mu      sync.RWMutex
	ttl     time.Duration
}

// NewCache 创建缓存
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		records: make(map[string]*ResolveRecord),
		ttl:     ttl,
	}
}

// Add 添加记录
func (c *Cache) Add(domain string, clientIP net.IP, port int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.records[domain] = &ResolveRecord{
		Domain:      domain,
		ClientIP:    clientIP,
		Port:        port,
		CreatedAt:   time.Now(),
		LastAccess:  time.Now(),
		AccessCount: 0,
	}
}

// Get 获取记录
func (c *Cache) Get(domain string) (*ResolveRecord, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	record, exists := c.records[domain]
	if !exists {
		return nil, false
	}

	// 检查是否过期
	if time.Since(record.CreatedAt) > c.ttl {
		return nil, false
	}

	record.LastAccess = time.Now()
	record.AccessCount++
	return record, true
}

// GetByPort 通过端口获取记录
func (c *Cache) GetByPort(port int) (*ResolveRecord, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, record := range c.records {
		if record.Port == port {
			// 检查是否过期
			if time.Since(record.CreatedAt) > c.ttl {
				return nil, false
			}
			record.LastAccess = time.Now()
			record.AccessCount++
			return record, true
		}
	}
	return nil, false
}

// cleanup 清理过期记录
func (c *Cache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for domain, record := range c.records {
		if now.Sub(record.CreatedAt) > c.ttl {
			delete(c.records, domain)
		}
	}
}

// StartCleanup 启动定期清理
func (c *Cache) StartCleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			c.cleanup()
		}
	}()
}
