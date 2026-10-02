package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"motocam/server/internal/appdist"
	"motocam/server/internal/config"
	"motocam/server/internal/httpapi"
	"motocam/server/internal/legal"
	"motocam/server/internal/store"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		os.Exit(healthcheck())
	}
	cfg := config.FromEnv()
	if cfg.AdminToken == "" {
		log.Println("警告：未设置 ADMIN_TOKEN，管理接口会全部拒绝")
	} else if cfg.AdminToken == "dev-token-change-me" {
		log.Println("警告：正在使用示例管理令牌，公网部署前请更换")
	}
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "images"), 0o755); err != nil {
		log.Fatal(err)
	}
	apps, err := appdist.Open(filepath.Join(cfg.DataDir, "app"))
	if err != nil {
		log.Fatal(err)
	}
	if cfg.PublicBaseURL == "" {
		log.Println("警告：未设置 PUBLIC_BASE_URL，安装包地址将使用请求里的 Host")
	}
	termsPath := filepath.Join(cfg.DataDir, "legal", "terms.md")
	if err := legal.Seed(termsPath); err != nil {
		log.Fatal(err)
	}
	if !cfg.Mail.Enabled() && !cfg.AuthDev {
		log.Println("警告：未配置 SMTP，且未打开 AUTH_DEV。获取激活码会失败")
	}
	if cfg.AuthDev {
		log.Println("警告：AUTH_DEV=1，激活码只写在日志里，不要在公网这样开")
	}
	if cfg.AmapKey == "" {
		log.Println("警告：未设置 AMAP_KEY，低置信度上报不会在服务器上重定位")
	}
	db, err := store.Open(filepath.Join(cfg.DataDir, "motocam.db"), filepath.Join(cfg.DataDir, "images"), store.Limits{
		PromoteMin:     cfg.PromoteMin,
		PromoteRadiusM: cfg.PromoteRadiusM,
		RatePerHour:    cfg.RatePerHour,
		RatePerDay:     cfg.RatePerDay,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	handler := httpapi.NewWithOptions(httpapi.HandlerOptions{
		AdminToken:   cfg.AdminToken,
		MaxImage:     cfg.MaxImageBytes,
		DB:           db,
		Apps:         apps,
		PublicBase:   cfg.PublicBaseURL,
		DevMode:      cfg.AuthDev,
		ReviewEmail:  cfg.ReviewEmail,
		ReviewCode:   cfg.ReviewCode,
		TermsPath:    termsPath,
		TermsVersion: cfg.TermsVersion,
		Mail:         cfg.Mail,
		AmapKey:      cfg.AmapKey,
	})
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Relocation runs off the request. This bound still has to cover a
		// photo upload and an install-package download on a slow link.
		WriteTimeout: 120 * time.Second,
	}
	log.Printf("小moto安全驾驶指南社区服务监听 %s，自动公开阈值 %d 条 / %.0f 米", cfg.Addr, cfg.PromoteMin, cfg.PromoteRadiusM)
	log.Fatal(server.ListenAndServe())
}

func healthcheck() int {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8787"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 1
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
