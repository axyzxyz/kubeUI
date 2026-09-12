package handler

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"k8s.io/client-go/rest"

	"github.com/axyzxyz/kubeui/backend/internal/api/middleware"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/errcode"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/logx"
	"github.com/axyzxyz/kubeui/backend/internal/service"
)

// k8sProxyAuthAuditPrefix 是代理审计动作前缀。
const k8sProxyAction = "k8s-proxy"

// proxyTransportCache 按 runtime RestConfig 指针缓存反向代理的 http.Client,
// 避免 TLS/CA 每请求重复解析;运行时重建(重注册)后自动换新键。
type proxyTransportCache struct {
	mu sync.Mutex
	m  map[*rest.Config]*http.Client
}

var proxyClients = &proxyTransportCache{m: make(map[*rest.Config]*http.Client)}

// clientFor 返回指定 runtime 的 http.Client(带集群 TLS 与自定义 Dialer)。
func (c *proxyTransportCache) clientFor(cfg *rest.Config) (*http.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cl, ok := c.m[cfg]; ok {
		return cl, nil
	}
	cl, err := buildProxyClient(cfg)
	if err != nil {
		return nil, err
	}
	c.m[cfg] = cl
	return cl, nil
}

// buildProxyClient 从 rest.Config 构建 TLS/拨号已就绪的 http.Client:
// CA/Insecure/客户端证书与 rest.Config.Dial(Agent 隧道)全部生效;
// Authorization 由注入层显式设置为集群凭证(证书型集群靠 TLS 客户端证书认证)。
func buildProxyClient(cfg *rest.Config) (*http.Client, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.Insecure {
		tlsCfg.InsecureSkipVerify = true //nolint:gosec // 显式继承集群 kubeconfig 的 insecure 配置
	} else if pool, err := rootCertPool(cfg.CAFile, cfg.CAData); err != nil {
		return nil, err
	} else if pool != nil {
		tlsCfg.RootCAs = pool
	}
	// 客户端证书型凭证(k3s/自签集群常见):必须透传到上游 TLS 握手,
	// 否则上游 401 "provide credentials"。
	clientCert, err := clientCertificate(cfg)
	if err != nil {
		return nil, err
	}
	if clientCert != nil {
		tlsCfg.Certificates = []tls.Certificate{*clientCert}
	}
	transport := &http.Transport{
		TLSClientConfig:   tlsCfg,
		ForceAttemptHTTP2: true,
	}
	if cfg.Dial != nil {
		// Agent 模式集群:经隧道拨号(TunnelDialer),客户端全链路零改动。
		transport.DialContext = cfg.Dial
	}
	bearer := cfg.BearerToken
	if bearer == "" && cfg.BearerTokenFile != "" {
		b, err := os.ReadFile(cfg.BearerTokenFile)
		if err != nil {
			return nil, fmt.Errorf("read bearer token file: %w", err)
		}
		bearer = strings.TrimSpace(string(b))
	}
	return &http.Client{
		Transport: &authInjectingTransport{base: transport, bearer: bearer},
		// 流式响应(watch)不设整体超时;单请求超时由 client-go 侧 ctx 控制。
		Timeout: 0,
	}, nil
}

// authInjectingTransport 在转发前注入集群 kubeconfig 的 Bearer 凭证并剥离
// 客户端原 Authorization(平台 token 与集群 token 域不同,禁止透传)。
type authInjectingTransport struct {
	base   http.RoundTripper
	bearer string
}

// RoundTrip 实现 http.RoundTripper。
func (t *authInjectingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.Header.Del("Authorization")
	if t.bearer != "" {
		req2.Header.Set("Authorization", "Bearer "+t.bearer)
	}
	return t.base.RoundTrip(req2)
}

// GET /k8s/:cluster/*path — 平台 K8s 反向代理:
// 认证(短期 token 或平台 JWT)→ 平台 RBAC → 经 ClusterManager Dialer 转发。
// 支持流式(watch)与升级(exec SPDY/WebSocket);Agent 模式集群同样经隧道。
func k8sProxy(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		cluster := c.Param("cluster")
		proxyUser := ""
		token, ok := bearerTokenOf(c)
		if !ok {
			proxyAudit(d, c, "", cluster, "deny")
			c.AbortWithStatusJSON(401, errBody(errcode.Unauthorized, "missing bearer token"))
			return
		}
		// 1) 短期签发 token:集群绑定校验;审计归因到签发者。
		if rec, err := d.Kubeconfigs.VerifyShortToken(c.Request.Context(), token); err == nil {
			owner := ""
			if d.Users != nil {
				owner = d.Users.UsernameByID(c.Request.Context(), rec.UserID)
			}
			if rec.Cluster != cluster {
				proxyAudit(d, c, owner, cluster, "deny")
				c.AbortWithStatusJSON(403, errBody(errcode.Forbidden, "credential is not valid for this cluster"))
				return
			}
			proxyUser = owner
		} else {
			// 2) 平台 JWT + 平台 RBAC(k8s-proxy:access:任一内置角色可访问,
			// 集群内细粒度权限由目标集群 RBAC 委托裁决)。
			ident, err := d.Auth.VerifyAccessToken(c.Request.Context(), token)
			if err != nil {
				proxyAudit(d, c, "", cluster, "deny")
				c.AbortWithStatusJSON(401, errBody(errcode.Unauthorized, "invalid or expired token"))
				return
			}
			if ident.Role != "admin" && ident.Role != "operator" && ident.Role != "viewer" {
				proxyAudit(d, c, ident.Username, cluster, "deny")
				c.AbortWithStatusJSON(403, errBody(errcode.Forbidden, "k8s-proxy access denied by platform RBAC"))
				return
			}
		}

		rt, err := d.Manager.Get(cluster)
		if err != nil {
			proxyAudit(d, c, "", cluster, "deny")
			c.AbortWithStatusJSON(404, errBody(errcode.ClusterNotFound, "cluster not found"))
			return
		}
		target, err := url.Parse(rt.RestConfig.Host)
		if err != nil {
			logx.Error(c.Request.Context(), "parse cluster host failed", "cluster", cluster, "err", err)
			c.AbortWithStatusJSON(502, errBody(errcode.UpstreamK8sError, "invalid cluster host"))
			return
		}
		cl, err := proxyClients.clientFor(rt.RestConfig)
		if err != nil {
			logx.Error(c.Request.Context(), "build proxy client failed", "cluster", cluster, "err", err)
			c.AbortWithStatusJSON(502, errBody(errcode.UpstreamK8sError, "build proxy transport failed"))
			return
		}
		proxyAudit(d, c, proxyUser, cluster, "allow")
		proxy := &httputil.ReverseProxy{
			Transport:     cl.Transport,
			FlushInterval: 100 * time.Millisecond, // watch 日志流式
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.Out.URL.Scheme = target.Scheme
				pr.Out.URL.Host = target.Host
				pr.Out.Host = target.Host
				// 去掉 /k8s/{cluster} 前缀,保留其后的 apiserver 路径。
				pr.Out.URL.Path = singleJoinSlash(target.Path, stripClusterPrefix(c.Param("path")))
				pr.Out.URL.RawPath = ""
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				logx.Warn(r.Context(), "k8s proxy upstream error", "cluster", cluster, "err", err)
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"code":50200,"message":"agent tunnel or apiserver unavailable"}`))
			},
		}
		proxy.ServeHTTP(&proxyWriter{ResponseWriter: c.Writer}, c.Request)
	}
}

// proxyWriter 是最小化的 ResponseWriter 包装:仅暴露 ResponseWriter 与
// Flush,剥离 gin 响应写入器的 CloseNotifier 能力,避免 ReverseProxy 在
// httptest/无底层连接场景触发其 panic。
type proxyWriter struct {
	http.ResponseWriter
}

// Flush 实现 http.Flusher(流式响应必需)。
func (w *proxyWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack 透传升级连接(exec SPDY / WebSocket 必需)。
func (w *proxyWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying response writer does not support hijack")
}

// proxyAudit 记录代理访问审计(读写均记,凭证类操作之外最敏感的入口)。
func proxyAudit(d Deps, c *gin.Context, username, cluster, result string) {
	var userID int64
	if ident, ok := middleware.IdentityOf(c); ok {
		userID = ident.UserID
		if username == "" {
			username = ident.Username
		}
	}
	if err := d.Audit.Record(c.Request.Context(), service.AuditEntry{
		Action:       k8sProxyAction,
		Resource:     c.FullPath(),
		ResourceType: "k8s-proxy",
		Cluster:      cluster,
		Name:         c.Request.Method + " " + c.Param("path"),
		SourceIP:     c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
		Result:       result,
		UserID:       userID,
		Username:     username,
	}); err != nil {
		logx.Warn(c.Request.Context(), "k8s proxy audit failed", "cluster", cluster, "err", err)
	}
}

// errBody 构造代理路径下的统一错误响应体(不走 gin response 中间件)。
func errBody(code int, message string) map[string]any {
	return map[string]any{"code": code, "message": message, "data": nil}
}

// bearerTokenOf 提取 Bearer token。
func bearerTokenOf(c *gin.Context) (string, bool) {
	h := c.GetHeader("Authorization")
	if t, ok := strings.CutPrefix(h, "Bearer "); ok && t != "" {
		return t, true
	}
	return "", false
}

// stripClusterPrefix 规整 *path 通配参数(保留前导斜杠)。
func stripClusterPrefix(p string) string {
	if p == "" || p == "/" {
		return ""
	}
	return p
}

// singleJoinSlash 拼接 base path 与子路径。
func singleJoinSlash(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	case strings.HasSuffix(a, "/") && strings.HasPrefix(b, "/"):
		return a + b[1:]
	case !strings.HasSuffix(a, "/") && !strings.HasPrefix(b, "/"):
		return a + "/" + b
	default:
		return a + b
	}
}

// rootCertPool 从 CAFile/CAData 构建根证书池;两者皆空返回 nil(系统池)。
// clientCertificate 从 rest.Config 解析 TLS 客户端证书(文件路径或内嵌 PEM 均支持);
// 无客户端证书时返回 nil。
func clientCertificate(cfg *rest.Config) (*tls.Certificate, error) {
	if cfg.CertData != nil || cfg.KeyData != nil {
		cert, err := tls.X509KeyPair(cfg.CertData, cfg.KeyData)
		if err != nil {
			return nil, fmt.Errorf("parse client cert/key data: %w", err)
		}
		return &cert, nil
	}
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client cert/key file: %w", err)
		}
		return &cert, nil
	}
	return nil, nil
}

func rootCertPool(caFile string, caData []byte) (*x509.CertPool, error) {
	var data []byte
	switch {
	case len(caData) > 0:
		data = caData
	case caFile != "":
		b, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		data = b
	default:
		return nil, nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("no valid certificates in CA data")
	}
	return pool, nil
}
