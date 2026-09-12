package k8s

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"k8s.io/client-go/rest"
)

// Dialer 是可替换的网络拨号器接口,与 net.Dialer 的 DialContext 同构。
//
// Wave2-agent 挂载点:Agent 反连模式落地后,agent 会通过
// Manager.SetDialer(cluster, TunnelDialer) 为 Agent 接入的集群注入隧道拨号器;
// 构建该集群的 rest.Config 时以 rest.Config.Dial 挂载(TLS 端到端保持在
// Server 与目标 APIServer 之间,Agent 只透传字节,见 01-architecture §4.3)。
type Dialer interface {
	// DialContext 建立 network 到 addr 的连接。
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// NetDialer 将 *net.Dialer 适配为 Dialer(默认实现)。
type NetDialer struct {
	// Timeout 拨号超时;0 表示 net.Dialer 默认值(30s)。
	Timeout time.Duration
	// KeepAlive TCP keepalive 间隔;0 表示默认。
	KeepAlive time.Duration
}

// DialContext 实现 Dialer。
func (d NetDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	nd := &net.Dialer{Timeout: d.Timeout, KeepAlive: d.KeepAlive}
	conn, err := nd.DialContext(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s %s: %w", network, addr, err)
	}
	return conn, nil
}

// dialers 保存按集群注入的自定义拨号器;注册/重建客户端时经 rest.Config.Dial 挂载。
// 由 Manager 持锁访问,本文件仅供 Manager 内部使用。
type dialers struct {
	mu sync.RWMutex
	m  map[string]Dialer
}

func newDialers() *dialers { return &dialers{m: make(map[string]Dialer)} }

// set 记录集群自定义拨号器;nil 表示清除。
func (d *dialers) set(cluster string, dl Dialer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if dl == nil {
		delete(d.m, cluster)
		return
	}
	d.m[cluster] = dl
}

// get 返回集群自定义拨号器,未注入时返回 nil。
func (d *dialers) get(cluster string) Dialer {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.m[cluster]
}

// applyDialer 将自定义拨号器挂到 rest.Config(经 rest.Config.Dial);
// Wave2-agent 通过 Manager.SetDialer 注入后,重建客户端即自动生效。
func applyDialer(cluster string, cfg *rest.Config, dl Dialer) error {
	if cfg == nil {
		return errors.New("rest config is nil")
	}
	if dl == nil {
		return nil
	}
	cfg.Dial = dl.DialContext
	return nil
}
