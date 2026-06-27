package config

import "testing"

func TestLoadRequiresDatabaseURL(t *testing.T) {
	_, err := Load(func(string) string { return "" })
	if err == nil {
		t.Fatal("expected error when DATABASE_URL is missing")
	}
}

func TestLoadReadsValues(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL":        "postgres://localhost/pp",
		"SESSION_SIGNING_KEY": "supersecretkey",
		"GOOGLE_CLIENT_ID":    "gid",
		"APPLE_CLIENT_ID":     "aid",
		"COOKIE_SECURE":       "true",
	}
	cfg, err := Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DatabaseURL != "postgres://localhost/pp" || string(cfg.SessionSigningKey) != "supersecretkey" {
		t.Fatalf("config not parsed: %+v", cfg)
	}
	if !cfg.CookieSecure || cfg.Port != "8080" {
		t.Fatalf("defaults/bools wrong: %+v", cfg)
	}
}

func TestLoadReadsVapid(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL":        "postgres://localhost/pp",
		"SESSION_SIGNING_KEY": "k",
		"VAPID_PUBLIC_KEY":    "pub",
		"VAPID_PRIVATE_KEY":   "priv",
		"VAPID_SUBJECT":       "mailto:a@x.io",
	}
	cfg, err := Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.VapidPublicKey != "pub" || cfg.VapidPrivateKey != "priv" || cfg.VapidSubject != "mailto:a@x.io" {
		t.Fatalf("vapid not parsed: %+v", cfg)
	}
}
