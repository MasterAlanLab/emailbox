package service

import (
	"context"
	"emailbox/pkg/model"
	"emailbox/pkg/repo"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type UserService struct{ store *repo.Store }

func NewUserService(s *repo.Store) *UserService { return &UserService{store: s} }
func (s *UserService) Get(ctx context.Context, id string) (*model.UserResponse, error) {
	u, e := s.store.GetUserByID(ctx, id)
	if e != nil {
		return nil, e
	}
	r := u.ToResponse()
	return &r, nil
}
func (s *UserService) Update(ctx context.Context, id string, req model.UpdateProfileRequest) (*model.UserResponse, error) {
	u, e := s.store.GetUserByID(ctx, id)
	if e != nil {
		return nil, e
	}
	if username := strings.TrimSpace(req.Username); username != "" {
		if err := validateUsername(username); err != nil {
			return nil, err
		}
		u.Username = username
	}
	// nil 表示没传，保持原值；空串表示用户主动清空。
	if req.Email != nil {
		email := strings.ToLower(strings.TrimSpace(*req.Email))
		if email != "" {
			if err := validateEmail(email); err != nil {
				return nil, err
			}
		}
		u.Email = email
	}
	// 只有显式提供 avatar_url 时才修改，否则局部更新会把已有头像清空。
	if req.AvatarURL != nil {
		avatar := strings.TrimSpace(*req.AvatarURL)
		if len(avatar) > maxAvatarURLLen {
			return nil, fmt.Errorf("头像地址长度不能超过 %d 个字符", maxAvatarURLLen)
		}
		u.AvatarURL = avatar
	}
	if e = s.store.UpdateUserProfile(ctx, u); e != nil {
		if errors.Is(e, repo.ErrConflict) {
			return nil, errors.New("用户名或邮箱已被使用")
		}
		return nil, e
	}
	r := u.ToResponse()
	return &r, nil
}
func (s *UserService) ChangePassword(ctx context.Context, id string, req model.ChangePasswordRequest) error {
	if len(req.NewPassword) < 6 {
		return errors.New("新密码长度不能少于6个字符")
	}
	if len(req.NewPassword) > maxPasswordLen {
		return fmt.Errorf("新密码长度不能超过 %d 个字节", maxPasswordLen)
	}
	u, e := s.store.GetUserByID(ctx, id)
	if e != nil {
		return e
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.OldPassword)) != nil {
		return errors.New("原密码错误")
	}
	h, e := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if e != nil {
		return e
	}
	if e = s.store.UpdateUserPassword(ctx, id, string(h)); e != nil {
		return e
	}
	return s.store.DeleteUserSessions(ctx, id)
}
