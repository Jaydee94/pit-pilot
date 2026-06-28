package config

import "fmt"

type Config struct {
	DatabaseURL       string
	SessionSigningKey []byte
	GoogleClientID    string
	AppleClientID     string
	Port              string
	CookieSecure      bool
	VapidPublicKey    string
	VapidPrivateKey   string
	VapidSubject      string
	AllowDevLogin     bool
}

// Load builds Config from a getenv function. DATABASE_URL and
// SESSION_SIGNING_KEY are required.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		DatabaseURL:       getenv("DATABASE_URL"),
		SessionSigningKey: []byte(getenv("SESSION_SIGNING_KEY")),
		GoogleClientID:    getenv("GOOGLE_CLIENT_ID"),
		AppleClientID:     getenv("APPLE_CLIENT_ID"),
		Port:              getenv("PORT"),
		CookieSecure:      getenv("COOKIE_SECURE") == "true",
		VapidPublicKey:    getenv("VAPID_PUBLIC_KEY"),
		VapidPrivateKey:   getenv("VAPID_PRIVATE_KEY"),
		VapidSubject:      getenv("VAPID_SUBJECT"),
		AllowDevLogin:     getenv("ALLOW_DEV_LOGIN") == "true",
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if len(cfg.SessionSigningKey) == 0 {
		return Config{}, fmt.Errorf("SESSION_SIGNING_KEY is required")
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	return cfg, nil
}
