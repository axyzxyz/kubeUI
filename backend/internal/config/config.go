// Package config 加载平台配置:YAML 文件 + 环境变量覆盖(koanf)。
//
// 零配置启动:无配置文件时使用默认值(sqlite、内存 dev jwt secret 与 master key)。
package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Server HTTP 服务监听配置。
type Server struct {
	Addr         string `json:"addr"`
	ReadTimeout  int    `json:"readTimeout"`
	WriteTimeout int    `json:"writeTimeout"`
	// TLS 可选 HTTPS 证书;certFile/keyFile 都设置时以 HTTPS 监听。
	// kubectl/Lens 等客户端出于安全策略不会经明文 HTTP 发送 Bearer 凭证,
	// 客户端 kubeconfig 场景(01 §5.2)必须启用 HTTPS。
	TLS TLS `json:"tls"`
	// ExternalURL 平台对外可访问的基础地址(如 https://kubeUI.example.com),
	// 用于渲染 Agent manifest 的 serverUrl 与签发 kubeconfig 的 server 字段。
	ExternalURL string `json:"externalUrl"`
}

// Database 存储配置。
type Database struct {
	// Driver 支持 sqlite(默认)与 postgres。
	Driver string `json:"driver"`
	// DSN 为空时 sqlite 使用工作目录下 data/kubeui.db。
	DSN string `json:"dsn"`
}

// Security 安全相关配置。
type Security struct {
	// MasterKey 为 32 字节 base64,用于 kubeconfig AES-GCM 加密。
	MasterKey string `json:"-"`
	// JWTSecret 为 HS256 签名密钥。
	JWTSecret string `json:"-"`
}

// TLS 是可选的 HTTPS 证书配置。
type TLS struct {
	// CertFile 证书 PEM 路径;KeyFile 私钥 PEM 路径。
	CertFile string `json:"certFile"`
	KeyFile  string `json:"keyFile"`
}

// Log 日志配置。
type Log struct {
	Level  string `json:"level"`
	Format string `json:"format"`
}

// Config 平台总配置,字段与 deploy/config-example.yaml 对应。
type Config struct {
	Server   Server   `json:"server"`
	Database Database `json:"database"`
	Security Security `json:"-"`
	Log      Log      `json:"log"`
}

const envPrefix = "KUBEUI_"

// Load 加载配置:path 为空时跳过文件,环境变量 KUBEUI_* 始终覆盖。
func Load(path string) (*Config, error) {
	k := koanf.New(".")
	if path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("load config file %s: %w", path, err)
		}
	}
	// KUBEUI_SERVER__ADDR 风格:双下划线映射嵌套。
	if err := k.Load(env.Provider(envPrefix, ".", func(s string) string {
		return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(s, envPrefix)), "__", ".")
	}), nil); err != nil {
		return nil, fmt.Errorf("load env config: %w", err)
	}

	cfg := &Config{}
	if err := k.Unmarshal("", cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	applyDefaults(cfg)

	// 敏感项优先取环境变量,避免落入 YAML。
	cfg.Security.MasterKey = firstNonEmpty(
		os.Getenv("KUBEUI_MASTER_KEY"), k.String("security.masterKey"))
	cfg.Security.JWTSecret = firstNonEmpty(
		os.Getenv("KUBEUI_JWT_SECRET"), k.String("auth.jwtSecret"))

	if cfg.Security.MasterKey == "" {
		key, err := generateRandomKeyB64()
		if err != nil {
			return nil, fmt.Errorf("generate dev master key: %w", err)
		}
		cfg.Security.MasterKey = key
		slog.Warn("security.masterKey not set, using random dev key; encrypted data will be unreadable after restart")
	}
	if cfg.Security.JWTSecret == "" {
		key, err := generateRandomKeyB64()
		if err != nil {
			return nil, fmt.Errorf("generate dev jwt secret: %w", err)
		}
		cfg.Security.JWTSecret = key
		slog.Warn("auth.jwtSecret not set, using random dev secret; tokens will be invalid after restart")
	}
	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = ":8080"
	}
	if cfg.Server.ExternalURL == "" {
		// 派生规则:TLS 启用则 https;Addr 为 ":port" 形式补 localhost,
		// 否则原样使用(避免出现 "http://localhost127.0.0.1:8443" 类非法地址)。
		scheme := "http"
		if cfg.Server.TLS.CertFile != "" && cfg.Server.TLS.KeyFile != "" {
			scheme = "https"
		}
		host := cfg.Server.Addr
		if strings.HasPrefix(host, ":") {
			host = "localhost" + host
		}
		cfg.Server.ExternalURL = scheme + "://" + host
	}
	if cfg.Server.ReadTimeout == 0 {
		cfg.Server.ReadTimeout = 30
	}
	if cfg.Server.WriteTimeout == 0 {
		cfg.Server.WriteTimeout = 60
	}
	if cfg.Database.Driver == "" {
		cfg.Database.Driver = "sqlite"
	}
	if cfg.Database.DSN == "" && cfg.Database.Driver == "sqlite" {
		cfg.Database.DSN = "data/kubeui.db"
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.Format == "" {
		cfg.Log.Format = "json"
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func generateRandomKeyB64() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}
