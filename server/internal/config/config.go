package config

import (
	"os"
	"strconv"
	"strings"

	"motocam/server/internal/mail"
)

type Config struct {
	Addr           string
	DataDir        string
	AdminToken     string
	PromoteMin     int
	PromoteRadiusM float64
	RatePerHour    int
	RatePerDay     int
	MaxImageBytes  int
	PublicBaseURL  string
	AuthDev        bool
	ReviewEmail    string
	ReviewCode     string
	TermsVersion   string
	Mail           mail.Config
	AmapKey        string
}

func FromEnv() Config {
	cfg := Config{
		Addr:           env("ADDR", ":8787"),
		DataDir:        env("DATA_DIR", "data"),
		AdminToken:     os.Getenv("ADMIN_TOKEN"),
		PromoteMin:     envInt("PROMOTE_MIN_REPORTS", 1),
		PromoteRadiusM: envFloat("PROMOTE_RADIUS_M", 200),
		RatePerHour:    envInt("RATE_PER_HOUR", 200),
		RatePerDay:     envInt("RATE_PER_DAY", 1000),
		MaxImageBytes:  envInt("MAX_IMAGE_BYTES", 5<<20),
		PublicBaseURL:  strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")), "/"),
		AuthDev:        envBool("AUTH_DEV"),
		ReviewEmail:    strings.ToLower(strings.TrimSpace(os.Getenv("REVIEW_EMAIL"))),
		ReviewCode:     strings.TrimSpace(os.Getenv("REVIEW_CODE")),
		TermsVersion:   strings.TrimSpace(os.Getenv("TERMS_VERSION")),
		AmapKey:        strings.TrimSpace(os.Getenv("AMAP_KEY")),
		Mail: mail.Config{
			Host: os.Getenv("SMTP_HOST"),
			Port: os.Getenv("SMTP_PORT"),
			User: os.Getenv("SMTP_USER"),
			Pass: os.Getenv("SMTP_PASS"),
			From: os.Getenv("SMTP_FROM"),
			TLS:  os.Getenv("SMTP_TLS"),
		},
	}
	if cfg.PromoteMin < 1 {
		cfg.PromoteMin = 1
	}
	if cfg.PromoteRadiusM < 50 {
		cfg.PromoteRadiusM = 50
	}
	if cfg.RatePerHour < 1 {
		cfg.RatePerHour = 1
	}
	if cfg.RatePerDay < cfg.RatePerHour {
		cfg.RatePerDay = cfg.RatePerHour
	}
	if cfg.MaxImageBytes < 64<<10 {
		cfg.MaxImageBytes = 64 << 10
	}
	return cfg
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envFloat(key string, fallback float64) float64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return value
}
