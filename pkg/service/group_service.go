package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"emailbox/pkg/crypto"
	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"

	"github.com/google/uuid"
)

const maxGroupNameLen = 100

// groupColors 是新建分组时随机挑选的颜色池，与 ValidGroupColor 的取值集合一致。
var groupColors = []model.GroupColor{
	model.GroupColorBlue, model.GroupColorGreen, model.GroupColorAmber,
	model.GroupColorRed, model.GroupColorPurple, model.GroupColorGray,
}

// randomGroupColor 给新建分组挑一个颜色。颜色只是视觉区分，不值得让用户在
// 建分组这个高频操作里多做一次选择。
func randomGroupColor() model.GroupColor {
	return groupColors[rand.IntN(len(groupColors))]
}

// GroupService 管理分组。分组是平的一层：账号挂在某个分组下，分组之间没有关系。
type GroupService struct {
	store  *repo.Store
	cipher crypto.Cipher
	quota  *quota.Service
}

func NewGroupService(store *repo.Store, cipher crypto.Cipher, q *quota.Service) *GroupService {
	return &GroupService{store: store, cipher: cipher, quota: q}
}

// maskStoredProxy 解密分组代理后打码。分组代理与账号代理一样可能带认证口令，
// 因此同样加密存储，出接口前必须先解密再打码。
func (s *GroupService) maskStoredProxy(ciphertext string) string {
	if ciphertext == "" {
		return ""
	}
	plain, err := s.cipher.Decrypt(ciphertext)
	if err != nil {
		return "(无法解密)"
	}
	return MaskProxyURL(plain)
}

// Proxy 返回分组代理的明文，供编辑表单回填。
//
// 解密失败必须报错，不能像 maskStoredProxy 那样回落到一段占位文案：那个回落是给
// 「只读展示」用的，少显示一行没有后果；这里的返回值会落进输入框，再随下一次保存
// 原样写回库里。空串更糟——它和「本来就没配代理」在表单上长得一模一样，
// 用户按下保存就把原来那条代理静默清掉了，那批账号从此走服务器公网 IP 直连。
func (s *GroupService) Proxy(ctx context.Context, tenantID, groupID string) (*model.MailGroupProxy, error) {
	group, err := s.store.GetMailGroup(ctx, tenantID, groupID)
	if err != nil {
		return nil, err
	}
	out := &model.MailGroupProxy{}
	if group.ProxyURL != "" {
		plain, err := s.cipher.Decrypt(group.ProxyURL)
		if err != nil {
			return nil, errors.New("代理地址解密失败，请检查 CREDENTIAL_KEY 是否与写入时一致")
		}
		out.ProxyURL = plain
	}
	return out, nil
}

// encryptProxy 加密分组的代理地址。
func (s *GroupService) encryptProxy(raw string) (string, error) {
	enc, err := s.cipher.Encrypt(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("加密代理地址失败: %w", err)
	}
	return enc, nil
}

// List 返回租户的全部分组，每个带账号数，顺序即用户排好的顺序。
func (s *GroupService) List(ctx context.Context, tenantID string) ([]*model.MailGroupNode, error) {
	groups, err := s.store.ListMailGroups(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	counts, err := s.store.CountAccountsPerGroup(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	nodes := make([]*model.MailGroupNode, 0, len(groups))
	for _, g := range groups {
		nodes = append(nodes, &model.MailGroupNode{
			MailGroup:      g,
			ProxyURLMasked: s.maskStoredProxy(g.ProxyURL),
			AccountCount:   counts[g.ID],
		})
	}
	return nodes, nil
}

// EnsureDefaultGroup 在事务内创建租户的系统默认分组。注册流程会调用它，
// 因此它与用户、租户、成员、配额同生共死。
func EnsureDefaultGroup(ctx context.Context, tx *repo.Store, tenantID string) error {
	return tx.CreateMailGroup(ctx, &model.MailGroup{
		ID: uuid.NewString(), TenantID: tenantID, Name: model.DefaultGroupName,
		Color: model.GroupColorGray, IsSystem: true,
	})
}

func (s *GroupService) Create(ctx context.Context, tenantID string, req model.CreateMailGroupRequest) (*model.MailGroup, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("分组名称不能为空")
	}
	if len(name) > maxGroupNameLen {
		return nil, fmt.Errorf("分组名称长度不能超过 %d 个字符", maxGroupNameLen)
	}
	color := req.Color
	if color == "" {
		color = randomGroupColor()
	}
	if !model.ValidGroupColor(color) {
		return nil, errors.New("分组颜色取值非法")
	}

	limits, err := s.quota.Effective(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	current, err := s.store.CountMailGroups(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if err := quota.CheckCount(limits.MaxGroups, current, 1, "分组"); err != nil {
		return nil, err
	}

	group := &model.MailGroup{
		ID: uuid.NewString(), TenantID: tenantID,
		Name: name, Description: strings.TrimSpace(req.Description), Color: color,
	}
	enc, err := s.encryptProxy(req.ProxyURL)
	if err != nil {
		return nil, err
	}
	group.ProxyURL = enc
	if err := s.store.CreateMailGroup(ctx, group); err != nil {
		if errors.Is(err, repo.ErrConflict) {
			return nil, errors.New("同名分组已存在")
		}
		return nil, err
	}
	return s.store.GetMailGroup(ctx, tenantID, group.ID)
}

func (s *GroupService) Update(ctx context.Context, tenantID, groupID string, req model.UpdateMailGroupRequest) (*model.MailGroup, error) {
	group, err := s.store.GetMailGroup(ctx, tenantID, groupID)
	if err != nil {
		return nil, err
	}
	if err := s.applyGroupFields(group, req); err != nil {
		return nil, err
	}

	// 定时刷新的两列先算好，写库的部分和上面那些字段一起放进同一个事务。
	// 它们是两条独立的窄 UPDATE（见 repo.UpdateMailGroupSchedule 的说明），
	// 不放一个事务里的话，改名成功而改间隔失败会留下一个「界面显示已保存、
	// 实际只生效了一半」的分组。
	var (
		interval      int
		next          *time.Time
		writeSchedule bool
	)
	if req.RefreshIntervalMinutes != nil {
		interval = *req.RefreshIntervalMinutes
		if !model.ValidRefreshIntervalMinutes(interval) {
			// 按天说而不是按分钟：下限是 10080 分钟，直接把这个数字甩给用户
			// 等于让他自己去除。天数从常量算出来，不写死，免得改了边界忘了改文案。
			const minutesPerDay = 24 * 60
			return nil, fmt.Errorf("定时刷新间隔必须是 0（关闭）或 %d~%d 天",
				model.MinRefreshIntervalMinutes/minutesPerDay,
				model.MaxRefreshIntervalMinutes/minutesPerDay)
		}
		writeSchedule = true
		if interval > 0 {
			// 按新间隔立刻重算下次时间，而不是等旧周期跑完：用户把 30 天改成
			// 7 天，要的是「从现在起每周一次」，让他先等满原来的 30 天说不通。
			//
			// 从 now + interval 起算而不是立刻刷一次：打开开关只是配置动作，
			// 不该顺手往服务商发一批请求——真想马上刷，页面上就有手动按钮。
			t := time.Now().Add(time.Duration(interval) * time.Minute)
			next = &t
		}
	}

	if err := s.store.WithTx(ctx, func(tx *repo.Store) error {
		if err := tx.UpdateMailGroup(ctx, group); err != nil {
			return err
		}
		if !writeSchedule {
			return nil
		}
		return tx.UpdateMailGroupSchedule(ctx, tenantID, groupID, interval, next)
	}); err != nil {
		if errors.Is(err, repo.ErrConflict) {
			return nil, errors.New("同名分组已存在")
		}
		return nil, err
	}
	if writeSchedule {
		group.RefreshIntervalMinutes = interval
		group.NextRefreshAt = next
	}
	return group, nil
}

// applyGroupFields 把 PATCH 里提供的普通字段校验后写进 group。
// 指针为 nil 的字段保持原值——这是 PATCH 的语义，不是遗漏。
func (s *GroupService) applyGroupFields(group *model.MailGroup, req model.UpdateMailGroupRequest) error {
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return errors.New("分组名称不能为空")
		}
		if len(name) > maxGroupNameLen {
			return fmt.Errorf("分组名称长度不能超过 %d 个字符", maxGroupNameLen)
		}
		group.Name = name
	}
	if req.Description != nil {
		group.Description = strings.TrimSpace(*req.Description)
	}
	if req.Color != nil {
		if !model.ValidGroupColor(*req.Color) {
			return errors.New("分组颜色取值非法")
		}
		group.Color = *req.Color
	}
	if req.ProxyURL != nil {
		enc, err := s.encryptProxy(*req.ProxyURL)
		if err != nil {
			return err
		}
		group.ProxyURL = enc
	}
	return nil
}

// Reorder 按传入的 ID 顺序重排分组。
func (s *GroupService) Reorder(ctx context.Context, tenantID string, groupIDs []string) error {
	return s.store.WithTx(ctx, func(tx *repo.Store) error {
		for i, id := range groupIDs {
			if _, err := tx.GetMailGroup(ctx, tenantID, id); err != nil {
				return err
			}
			if err := tx.UpdateMailGroupSort(ctx, tenantID, id, i); err != nil {
				return err
			}
		}
		return nil
	})
}

// Delete 删除分组，其下账号回落到系统默认分组。
//
// 账号必须先搬走再删分组——mail_accounts.group_id 是 NOT NULL 外键，
// 直接删分组要么被外键拒绝，要么（若配成 CASCADE）连账号一起删掉，
// 而误删上万个账号是不可接受的。
func (s *GroupService) Delete(ctx context.Context, tenantID, groupID string) error {
	group, err := s.store.GetMailGroup(ctx, tenantID, groupID)
	if err != nil {
		return err
	}
	if group.IsSystem {
		return errors.New("默认分组不能删除")
	}
	fallback, err := s.store.GetSystemMailGroup(ctx, tenantID)
	if err != nil {
		return err
	}
	return s.store.WithTx(ctx, func(tx *repo.Store) error {
		if err := tx.MoveAccountsToGroup(ctx, tenantID, groupID, fallback.ID); err != nil {
			return err
		}
		return tx.DeleteMailGroup(ctx, tenantID, groupID)
	})
}
