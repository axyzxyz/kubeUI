package k8s

import (
	"context"
	"fmt"
	"time"

	"github.com/v911/backend/internal/pkg/logx"
)

// runHealth 是单集群健康检查循环(受控生命周期 worker):周期探活 /readyz,
// 连续 3 次失败 → Degraded、连续 10 次 → Reconnecting(指数退避 1s→60s 封顶),
// 恢复即 Ready。状态变更经 notify 推送。退出路径:rt.stopCh 关闭(Unregister/Close)。
// 由 Manager.Register 启动,Unregister 负责 join(经 healthDone)。
func (m *Manager) runHealth(rt *ClusterRuntime) {
	defer close(rt.healthDone)
	ctx := context.Background()
	interval := m.opts.HealthInterval
	failCount := 0
	backoff := m.opts.BackoffStart
	status := ""
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-rt.stopCh:
			return
		case <-timer.C:
		}
		err := m.probe(ctx, rt)
		if err == nil {
			failCount = 0
			backoff = m.opts.BackoffStart
			if rt.setStatus("ready", "") {
				m.onTransition(rt)
				logx.Info(ctx, "cluster health recovered", "cluster", rt.Name)
			}
			if status != "" {
				// 从 Reconnecting 恢复时刷新 RESTMapper 缓存(01 §3.4 ③)。
				if r, ok := rt.Mapper.(interface{ Reset() }); ok {
					r.Reset()
				}
			}
			status = "ready"
			timer.Reset(interval)
			continue
		}
		failCount++
		delay := interval
		switch {
		case failCount >= reconnectThreshold:
			if rt.setStatus("reconnecting", fmt.Sprintf("probe failed %d times", failCount)) {
				m.onTransition(rt)
				logx.Warn(ctx, "cluster entering reconnecting", "cluster", rt.Name, "err", err)
			}
			status = "reconnecting"
			delay = backoff
			backoff = min(backoff*2, backoffMax)
		case failCount >= degradedThreshold:
			if rt.setStatus("degraded", "probe failing") {
				m.onTransition(rt)
				logx.Warn(ctx, "cluster degraded", "cluster", rt.Name, "err", err)
			}
			status = "degraded"
		default:
			// 未达阈值:保持当前状态,仅记录。
			logx.Debug(ctx, "cluster probe failed", "cluster", rt.Name, "err", err)
		}
		timer.Reset(delay)
	}
}

// onTransition 在状态迁移后推送监听器并确保事件 informer 常驻。
func (m *Manager) onTransition(rt *ClusterRuntime) {
	st, msg, lt := rt.Status()
	m.ensureEventInformer(rt)
	m.notify(StatusChange{Name: rt.Name, Status: st, Version: rt.Version, Message: msg, LastTransitionTime: lt})
}

// probe 按配置选择探活实现(默认 /readyz,测试可注入故障探针)。
func (m *Manager) probe(ctx context.Context, rt *ClusterRuntime) error {
	if m.opts.Probe != nil {
		return m.opts.Probe(ctx, rt, m.opts.ProbeTimeout)
	}
	return probeReady(ctx, rt, m.opts.ProbeTimeout)
}

// probeReady 探活目标集群 /readyz,5s 超时,失败不重试(等下个周期)。
func probeReady(ctx context.Context, rt *ClusterRuntime, timeout time.Duration) error {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result := rt.ClientSet.Discovery().RESTClient().Get().AbsPath("/readyz").Do(probeCtx)
	if err := result.Error(); err != nil {
		return fmt.Errorf("probe /readyz: %w", err)
	}
	var code int
	result.StatusCode(&code)
	if code < 200 || code >= 300 {
		return fmt.Errorf("probe /readyz: http status %d", code)
	}
	return nil
}
