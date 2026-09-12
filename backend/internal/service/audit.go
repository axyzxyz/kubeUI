package service

import (
	"context"
	"sync"

	"github.com/axyzxyz/kubeui/backend/internal/model"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/pagination"
	"github.com/axyzxyz/kubeui/backend/internal/store"
)

// AuditEntry 是待落库的审计事件(与中间件采集字段一一对应)。
type AuditEntry struct {
	RequestID    string
	UserID       int64
	Username     string
	Action       string
	Resource     string // 原始路由 path
	ResourceType string // 友好资源类型(auth/user/deployment...),未知路径为原始 path
	Cluster      string
	Namespace    string
	Name         string
	SourceIP     string
	UserAgent    string
	Result       string // allow|deny
}

// AuditService 负责审计事件落库与查询;审计记录不可修改、无删除路径。
// 同时向 watch hub 的 audit 订阅者广播事件流。
type AuditService struct {
	repo *store.AuditRepo

	mu      sync.Mutex
	subs    map[int64]chan model.AuditLog
	nextSub int64
}

// NewAuditService 构造 AuditService。
func NewAuditService(repo *store.AuditRepo) *AuditService {
	return &AuditService{repo: repo, subs: make(map[int64]chan model.AuditLog)}
}

// SubscribeStream 返回审计事件广播 channel 与退订函数;channel 容量有限,
// 消费方停滞时事件被丢弃(审计落库不受影响)。
func (s *AuditService) SubscribeStream() (<-chan model.AuditLog, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextSub
	s.nextSub++
	ch := make(chan model.AuditLog, 64)
	s.subs[id] = ch
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.subs, id)
	}
}

// broadcast 非阻塞分发审计事件给订阅者;发送方负责不阻塞(hub 侧消费)。
func (s *AuditService) broadcast(entry model.AuditLog) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- entry:
		default: // 订阅者停滞:丢弃,不阻塞审计主链路
		}
	}
}

// Record 追加一条审计记录并广播给 watch 订阅者。
func (s *AuditService) Record(ctx context.Context, e AuditEntry) error {
	stored := &store.AuditLog{
		RequestID:    e.RequestID,
		UserID:       e.UserID,
		Username:     e.Username,
		Action:       e.Action,
		Resource:     e.Resource,
		ResourceType: e.ResourceType,
		Cluster:      e.Cluster,
		Namespace:    e.Namespace,
		Name:         e.Name,
		SourceIP:     e.SourceIP,
		UserAgent:    e.UserAgent,
		Result:       e.Result,
	}
	if err := s.repo.CreateAuditLog(ctx, stored); err != nil {
		return err
	}
	s.broadcast(model.AuditLog{
		RequestID:    stored.RequestID,
		UserID:       stored.UserID,
		Username:     stored.Username,
		Action:       stored.Action,
		Resource:     stored.Resource,
		ResourceType: stored.ResourceType,
		Cluster:      stored.Cluster,
		Namespace:    stored.Namespace,
		Name:         stored.Name,
		SourceIP:     stored.SourceIP,
		UserAgent:    stored.UserAgent,
		Result:       stored.Result,
		CreatedAt:    stored.CreatedAt,
	})
	return nil
}

// AuditQuery 审计查询条件。
type AuditQuery struct {
	Username string
	Cluster  string
	Action   string
}

// List 按条件分页查询审计日志。
func (s *AuditService) List(ctx context.Context, q AuditQuery, p pagination.Pagination) (pagination.ResultBody[model.AuditLog], error) {
	logs, total, err := s.repo.ListAuditLogs(ctx, store.AuditFilter{
		Username: q.Username,
		Cluster:  q.Cluster,
		Action:   q.Action,
	}, p.Offset(), p.Size)
	if err != nil {
		return pagination.ResultBody[model.AuditLog]{}, err
	}
	items := make([]model.AuditLog, 0, len(logs))
	for i := range logs {
		l := &logs[i]
		items = append(items, model.AuditLog{
			ID:           l.ID,
			RequestID:    l.RequestID,
			UserID:       l.UserID,
			Username:     l.Username,
			Action:       l.Action,
			Resource:     l.Resource,
			ResourceType: l.ResourceType,
			Cluster:      l.Cluster,
			Namespace:    l.Namespace,
			Name:         l.Name,
			SourceIP:     l.SourceIP,
			UserAgent:    l.UserAgent,
			Result:       l.Result,
			CreatedAt:    l.CreatedAt,
		})
	}
	return pagination.Result(items, total, p), nil
}
