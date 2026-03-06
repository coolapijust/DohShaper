package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"portshaper/internal/config"
	"portshaper/internal/server"
)

func main() {
	// 加载配置
	cfg := config.NewConfig()

	// 验证配置
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "[Error] Configuration error: %v\n", err)
		fmt.Fprintf(os.Stderr, "\nRequired environment variables:\n")
		fmt.Fprintf(os.Stderr, "  SHAPER_SERVER_IP    - Server public IP address\n")
		fmt.Fprintf(os.Stderr, "\nOptional environment variables:\n")
		fmt.Fprintf(os.Stderr, "  SHAPER_DOH_PORT     - DoH service port (default: 8053)\n")
		fmt.Fprintf(os.Stderr, "  SHAPER_DOH_PATH     - DoH path (default: /dns-query)\n")
		fmt.Fprintf(os.Stderr, "  SHAPER_PORT_START   - Dynamic port range start (default: 10000)\n")
		fmt.Fprintf(os.Stderr, "  SHAPER_PORT_END     - Dynamic port range end (default: 65535)\n")
		fmt.Fprintf(os.Stderr, "  SHAPER_PORT_TTL     - Port allocation TTL (default: 5m)\n")
		fmt.Fprintf(os.Stderr, "  SHAPER_ENABLE_TLS   - Enable TLS (default: false)\n")
		fmt.Fprintf(os.Stderr, "  SHAPER_TLS_CERT     - TLS certificate path\n")
		fmt.Fprintf(os.Stderr, "  SHAPER_TLS_KEY      - TLS key path\n")
		fmt.Fprintf(os.Stderr, "  SHAPER_AUTO_CERT    - Enable AutoCert (default: false)\n")
		fmt.Fprintf(os.Stderr, "  SHAPER_DOH_DOMAIN   - Domain for AutoCert\n")
		os.Exit(1)
	}

	// 打印配置
	fmt.Println("=====================================")
	fmt.Println("Port-Shaper Server")
	fmt.Println("=====================================")
	fmt.Printf("Server IP:    %s\n", cfg.ServerIP)
	fmt.Printf("DoH Port:     %s\n", cfg.DoHPort)
	fmt.Printf("DoH Path:     %s\n", cfg.DoHPath)
	fmt.Printf("Port Range:   %d-%d\n", cfg.PortRangeStart, cfg.PortRangeEnd)
	fmt.Printf("Port TTL:     %v\n", cfg.PortTTL)
	fmt.Printf("TLS Enabled:  %v\n", cfg.EnableTLS)
	if cfg.AutoCert {
		fmt.Printf("AutoCert:     enabled for %s\n", cfg.DoHDomain)
	}
	fmt.Println("=====================================")

	// 创建服务器
	srv := server.NewServer(cfg)

	// 启动服务器
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[Error] Failed to start server: %v\n", err)
		os.Exit(1)
	}

	// 等待中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("\nServer is running. Press Ctrl+C to stop.")
	<-sigChan

	fmt.Println("\nShutting down...")
	srv.Stop()
	fmt.Println("Server stopped.")
}
