package service

import (
	"context"
	"encoding/json"
	"log/slog"

	"emailbox/pkg/model"
	"emailbox/pkg/repo"

	"github.com/google/uuid"
)

// AuditService 负责写与读审计日志。
//
// 审计是旁路：写失败**绝不能**让业务失败。一个记不上日志的删除操作，
// 比一个因为日志表满了而拒绝服务的平台要好——前者留下告警，后者让所有人停工。
// 因此 Record 只记 WARN 不回传错误，真正需要断言写入结果的测试走 store 层。
type AuditService struct{ store *repo.Store }

func NewAuditService(store *repo.Store) *AuditService { return &AuditService{store: store} }

// Entry 是一次待记录的操作。Details 用 map 传入，由本服务序列化成 JSON。
type Entry struct {
	TenantID     string
	ActorUserID  string
	ActorName    string
	ActorKind    string
	Action       string
	ResourceType string
	ResourceID   string
	IP           string
	Details      map[string]any
}

// Record 写一条审计。调用方不需要处理返回值。
func (s *AuditService) Record(ctx context.Context, e Entry) {
	details := "{}"
	if len(e.Details) > 0 {
		if encoded, err := json.Marshal(e.Details); err == nil {
			details = string(encoded)
		} else {
			// 序列化失败不能把整条审计丢掉：动作本身比它的附带信息重要得多。
			slog.Warn("审计详情序列化失败，改记空对象", "action", e.Action, "error", err)
		}
	}
	if e.ActorKind == "" {
		e.ActorKind = model.ActorKindUser
	}

	// 写审计不跟随调用方的 context 取消：请求处理完就取消 context 是常态，
	// 而「操作已经生效、审计却因为 context 结束而没写上」正是最不能接受的一种丢失。
	err := s.store.CreateAuditLog(context.WithoutCancel(ctx), model.AuditLog{
		ID:           uuid.NewString(),
		TenantID:     e.TenantID,
		ActorUserID:  e.ActorUserID,
		ActorName:    e.ActorName,
		ActorKind:    e.ActorKind,
		Action:       e.Action,
		ResourceType: e.ResourceType,
		ResourceID:   e.ResourceID,
		IP:           e.IP,
		Details:      details,
	})
	if err != nil {
		slog.Warn("写审计日志失败",
			"action", e.Action, "tenant_id", e.TenantID,
			"actor_user_id", e.ActorUserID, "error", err)
	}
}

// List 按条件分页取审计日志，并把缺失的操作者邮箱补上。
func (s *AuditService) List(ctx context.Context, f model.AuditFilter) ([]model.AuditLog, int, error) {
	logs, total, err := s.store.ListAuditLogs(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	s.fillActorNames(ctx, logs)
	return logs, total, nil
}

// fillActorNames 给只有 actor_user_id 的记录补上用户名。
//
// 写入侧只有管理员操作才带用户名（普通用户那条路径上 context 里没有完整用户），
// 于是后台的「操作者」列会显示一串裸 UUID——一屏 UUID 等于没有审计，
// 谁做的还得再去用户表里一个个查。
//
// 按 ID 去重后逐个查：一页 20 条通常来自个位数的操作者，查询次数很少。
// 补不上的（用户已删除，外键被置空）留空，前端显示「(已删除)」——
// 这正是 actor_name 冗余存储要解决的场景，只是历史数据没有。
func (s *AuditService) fillActorNames(ctx context.Context, logs []model.AuditLog) {
	cache := map[string]string{}
	for i := range logs {
		id := logs[i].ActorUserID
		if logs[i].ActorName != "" || id == "" {
			continue
		}
		name, ok := cache[id]
		if !ok {
			user, err := s.store.GetUserByID(ctx, id)
			if err == nil {
				name = user.Username
			}
			cache[id] = name
		}
		logs[i].ActorName = name
	}
}
