package repo

import (
	"context"
	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
	"emailbox/pkg/model"
)

func (s *Store) CreateUser(ctx context.Context, u *model.User) error {
	var err error
	if s.driver == "sqlite" {
		err = s.sqlite.CreateUser(ctx, sqlitedb.CreateUserParams{ID: u.ID, Username: u.Username, Email: u.Email, PasswordHash: u.PasswordHash, Status: string(u.Status)})
	} else {
		err = s.postgres.CreateUser(ctx, postgresdb.CreateUserParams{ID: u.ID, Username: u.Username, Email: u.Email, PasswordHash: u.PasswordHash, Status: string(u.Status)})
	}
	return normalize(err)
}
func mapSQLiteUser(u sqlitedb.User) *model.User {
	return &model.User{ID: u.ID, Username: u.Username, Email: u.Email, PasswordHash: u.PasswordHash, Status: model.UserStatus(u.Status), PlatformRole: model.PlatformRole(u.PlatformRole), CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt, DeletedAt: timePtr(u.DeletedAt)}
}
func mapPostgresUser(u postgresdb.User) *model.User {
	return &model.User{ID: u.ID, Username: u.Username, Email: u.Email, PasswordHash: u.PasswordHash, Status: model.UserStatus(u.Status), PlatformRole: model.PlatformRole(u.PlatformRole), CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt, DeletedAt: timePtr(u.DeletedAt)}
}
func (s *Store) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetUserByID(ctx, id)
		return mapSQLiteUser(v), normalize(e)
	}
	v, e := s.postgres.GetUserByID(ctx, id)
	return mapPostgresUser(v), normalize(e)
}
func (s *Store) GetUserByUsername(ctx context.Context, name string) (*model.User, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetUserByUsername(ctx, name)
		return mapSQLiteUser(v), normalize(e)
	}
	v, e := s.postgres.GetUserByUsername(ctx, name)
	return mapPostgresUser(v), normalize(e)
}
func (s *Store) UpdateUserProfile(ctx context.Context, u *model.User) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.UpdateUserProfile(ctx, sqlitedb.UpdateUserProfileParams{Username: u.Username, Email: u.Email, ID: u.ID})
	} else {
		n, e = s.postgres.UpdateUserProfile(ctx, postgresdb.UpdateUserProfileParams{Username: u.Username, Email: u.Email, ID: u.ID})
	}
	if e != nil {
		return normalize(e)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) UpdateUserPassword(ctx context.Context, id, hash string) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.UpdateUserPassword(ctx, sqlitedb.UpdateUserPasswordParams{PasswordHash: hash, ID: id})
	} else {
		n, e = s.postgres.UpdateUserPassword(ctx, postgresdb.UpdateUserPasswordParams{PasswordHash: hash, ID: id})
	}
	if e != nil {
		return e
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateUserPlatformRole 修改用户的平台角色（授予/撤销管理员）。
func (s *Store) UpdateUserPlatformRole(ctx context.Context, id string, role model.PlatformRole) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.UpdateUserPlatformRole(ctx, sqlitedb.UpdateUserPlatformRoleParams{PlatformRole: string(role), ID: id})
	} else {
		n, e = s.postgres.UpdateUserPlatformRole(ctx, postgresdb.UpdateUserPlatformRoleParams{PlatformRole: string(role), ID: id})
	}
	if e != nil {
		return normalize(e)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountPlatformAdmins 用于「最后一个管理员不可降级/删除」的校验，
// 以及启动时「系统里一个管理员都没有」的告警。
func (s *Store) CountPlatformAdmins(ctx context.Context) (int, error) {
	if s.driver == "sqlite" {
		n, e := s.sqlite.CountPlatformAdmins(ctx)
		return int(n), e
	}
	n, e := s.postgres.CountPlatformAdmins(ctx)
	return int(n), e
}

// UpdateUserStatus 启用/禁用用户。调用方在禁用时必须同时清空该用户的全部会话
// （DeleteUserSessions），否则他手里已有的会话仍然有效。
func (s *Store) UpdateUserStatus(ctx context.Context, id string, status model.UserStatus) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.UpdateUserStatus(ctx, sqlitedb.UpdateUserStatusParams{Status: string(status), ID: id})
	} else {
		n, e = s.postgres.UpdateUserStatus(ctx, postgresdb.UpdateUserStatusParams{Status: string(status), ID: id})
	}
	if e != nil {
		return normalize(e)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
