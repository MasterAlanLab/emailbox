package repo

import (
	"context"

	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
	"emailbox/pkg/model"
)

// CreateAuditLog 写一条审计记录。
//
// 调用方一律**不要**因为写审计失败而让业务失败：审计是旁路，
// 让它拖垮主流程等于给自己加了一个新的故障点。失败由上层记日志。
func (s *Store) CreateAuditLog(ctx context.Context, e model.AuditLog) error {
	var err error
	if s.driver == "sqlite" {
		err = s.sqlite.CreateAuditLog(ctx, sqlitedb.CreateAuditLogParams{
			ID:           e.ID,
			TenantID:     e.TenantID,
			ActorUserID:  nullStr(e.ActorUserID),
			ActorName:    e.ActorName,
			ActorKind:    e.ActorKind,
			Action:       e.Action,
			ResourceType: e.ResourceType,
			ResourceID:   e.ResourceID,
			Ip:           e.IP,
			Details:      e.Details,
		})
	} else {
		err = s.postgres.CreateAuditLog(ctx, postgresdb.CreateAuditLogParams{
			ID:           e.ID,
			TenantID:     e.TenantID,
			ActorUserID:  nullStr(e.ActorUserID),
			ActorName:    e.ActorName,
			ActorKind:    e.ActorKind,
			Action:       e.Action,
			ResourceType: e.ResourceType,
			ResourceID:   e.ResourceID,
			Ip:           e.IP,
			Details:      e.Details,
		})
	}
	return normalize(err)
}

// ListAuditLogs 按条件分页取审计记录，同时返回总条数。
func (s *Store) ListAuditLogs(ctx context.Context, f model.AuditFilter) ([]model.AuditLog, int, error) {
	f.Normalize()
	out := []model.AuditLog{}

	if s.driver == "sqlite" {
		rows, err := s.sqlite.ListAuditLogsPage(ctx, sqlitedb.ListAuditLogsPageParams{
			TenantID:    anyStr(f.TenantID),
			ActorUserID: anyStr(f.ActorUserID),
			ActorKind:   anyStr(f.ActorKind),
			Action:      anyStr(f.Action),
			RowLimit:    int64(f.Limit),
			RowOffset:   int64(f.Offset()),
		})
		if err != nil {
			return nil, 0, err
		}
		for _, r := range rows {
			out = append(out, mapSQLiteAuditLog(r))
		}
		total, err := s.sqlite.CountAuditLogs(ctx, sqlitedb.CountAuditLogsParams{
			TenantID:    anyStr(f.TenantID),
			ActorUserID: anyStr(f.ActorUserID),
			ActorKind:   anyStr(f.ActorKind),
			Action:      anyStr(f.Action),
		})
		return out, int(total), err
	}

	rows, err := s.postgres.ListAuditLogsPage(ctx, postgresdb.ListAuditLogsPageParams{
		TenantID:    nullStr(f.TenantID),
		ActorUserID: nullStr(f.ActorUserID),
		ActorKind:   nullStr(f.ActorKind),
		Action:      nullStr(f.Action),
		RowLimit:    int32(f.Limit),
		RowOffset:   int32(f.Offset()),
	})
	if err != nil {
		return nil, 0, err
	}
	for _, r := range rows {
		out = append(out, mapPostgresAuditLog(r))
	}
	total, err := s.postgres.CountAuditLogs(ctx, postgresdb.CountAuditLogsParams{
		TenantID:    nullStr(f.TenantID),
		ActorUserID: nullStr(f.ActorUserID),
		ActorKind:   nullStr(f.ActorKind),
		Action:      nullStr(f.Action),
	})
	return out, int(total), err
}

func mapSQLiteAuditLog(r sqlitedb.AuditLog) model.AuditLog {
	return model.AuditLog{
		ID: r.ID, TenantID: r.TenantID,
		ActorUserID: r.ActorUserID.String, ActorName: r.ActorName, ActorKind: r.ActorKind,
		Action: r.Action, ResourceType: r.ResourceType, ResourceID: r.ResourceID,
		IP: r.Ip, Details: r.Details, CreatedAt: r.CreatedAt,
	}
}

func mapPostgresAuditLog(r postgresdb.AuditLog) model.AuditLog {
	return model.AuditLog{
		ID: r.ID, TenantID: r.TenantID,
		ActorUserID: r.ActorUserID.String, ActorName: r.ActorName, ActorKind: r.ActorKind,
		Action: r.Action, ResourceType: r.ResourceType, ResourceID: r.ResourceID,
		IP: r.Ip, Details: r.Details, CreatedAt: r.CreatedAt,
	}
}
