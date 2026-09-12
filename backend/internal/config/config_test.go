package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsZeroConfig(t *testing.T) {
	t.Setenv("KUBEUI_MASTER_KEY", "")
	t.Setenv("KUBEUI_JWT_SECRET", "")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != ":8080" {
		t.Fatalf("addr = %q, want :8080", cfg.Server.Addr)
	}
	if cfg.Database.Driver != "sqlite" || cfg.Database.DSN != "data/kubeui.db" {
		t.Fatalf("db default = %+v", cfg.Database)
	}
	if cfg.Security.MasterKey == "" || cfg.Security.JWTSecret == "" {
		t.Fatal("dev master key and jwt secret must be generated")
	}
	if _, err := base64.StdEncoding.DecodeString(cfg.Security.MasterKey); err != nil {
		t.Fatalf("master key must be base64: %v", err)
	}
}

func TestLoadYAMLAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
server:
  addr: ":9090"
database:
  driver: sqlite
  dsn: /tmp/kubeUI-test.db
auth:
  jwtSecret: yaml-secret
security:
  masterKey: AAAA
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KUBEUI_MASTER_KEY", "BBBB")
	t.Setenv("KUBEUI_SERVER__ADDR", ":7070")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != ":7070" {
		t.Fatalf("env should override yaml addr, got %q", cfg.Server.Addr)
	}
	if cfg.Security.MasterKey != "BBBB" {
		t.Fatalf("KUBEUI_MASTER_KEY should win, got %q", cfg.Security.MasterKey)
	}
	if cfg.Security.JWTSecret != "yaml-secret" {
		t.Fatalf("jwt secret from yaml, got %q", cfg.Security.JWTSecret)
	}
}
