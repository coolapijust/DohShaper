package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 应用配置
type Config struct {
	// DoH 配置
	DoHPort     string
	DoHPath     string
	DoHDomain   string
	AutoCert    bool
	AutoCertDir string
	AutoCertEmail string

	// TLS 配置
	EnableTLS bool
	TLSCert   string
	TLSKey    string

	// 上游 DoH
	UpstreamDoH string

	// 端口管理配置
	PortRangeStart int
	PortRangeEnd   int
	PortTTL        time.Duration

	// 服务器公网 IP
	ServerIP string

	// 记录 TTL
	RecordTTL int
}

// NewConfig 从环境变量创建配置
func NewConfig() *Config {
	cfg := &Config{
		DoHPort:       getEnv("SHAPER_DOH_PORT", "8053"),
		DoHPath:       getEnv("SHAPER_DOH_PATH", "/dns-query"),
		DoHDomain:     getEnv("SHAPER_DOH_DOMAIN", ""),
		AutoCert:      getEnvBool("SHAPER_AUTO_CERT", false),
		AutoCertDir:   getEnv("SHAPER_AUTO_CERT_DIR", "/etc/dns-shaper/certs"),
		AutoCertEmail: getEnv("SHAPER_AUTO_CERT_EMAIL", ""),
		EnableTLS:     getEnvBool("SHAPER_ENABLE_TLS", false),
		TLSCert:       getEnv("SHAPER_TLS_CERT", ""),
		TLSKey:        getEnv("SHAPER_TLS_KEY", ""),
		UpstreamDoH:   getEnv("SHAPER_UPSTREAM_DOH", "https://1.1.1.1/dns-query"),
		PortRangeStart: getEnvInt("SHAPER_PORT_START", 10000),
		PortRangeEnd:   getEnvInt("SHAPER_PORT_END", 65535),
		PortTTL:        getEnvDuration("SHAPER_PORT_TTL", "5m"),
		ServerIP:       getEnv("SHAPER_SERVER_IP", ""),
		RecordTTL:      getEnvInt("SHAPER_RECORD_TTL", 300),
	}

	// 如果启用了 AutoCert，自动设置 EnableTLS
	if cfg.AutoCert {
		cfg.EnableTLS = true
	}

	return cfg
}

// Validate 验证配置
func (c *Config) Validate() error {
	if c.ServerIP == "" {
		return fmt.Errorf("SHAPER_SERVER_IP is required")
	}
	if c.PortRangeStart >= c.PortRangeEnd {
		return fmt.Errorf("port range invalid: %d >= %d", c.PortRangeStart, c.PortRangeEnd)
	}
	if c.EnableTLS && !c.AutoCert {
		if c.TLSCert == "" || c.TLSKey == "" {
			return fmt.Errorf("TLS cert and key are required when TLS is enabled")
		}
	}
	return nil
}

// GetDoHAddress 返回 DoH 服务地址
func (c *Config) GetDoHAddress() string {
	return fmt.Sprintf("%s:%s", c.ServerIP, c.DoHPort)
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

func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return i
}

func getEnvDuration(key, defaultVal string) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		val = defaultVal
	}
	d, err := time.ParseDuration(val)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}
