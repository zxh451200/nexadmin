package user

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/event"
	"nexadmin/pkg/idgen"
)

var (
	usernameRe = regexp.MustCompile(`^[A-Za-z0-9_.@-]{2,64}$`)
	phoneRe    = regexp.MustCompile(`^\+?[0-9][0-9\- ]{3,19}$`)
	emailRe    = regexp.MustCompile(`^[^@\s]+@[^@\s]+$`)
	regionRe   = regexp.MustCompile(`^[0-9]{0,12}$`)
)

type parsedInput struct {
	dept  idgen.ID
	posts []idgen.ID
	roles []idgen.ID
}

func parseIDs(raw []string, what string) ([]idgen.ID, error) {
	seen := map[idgen.ID]bool{}
	var out []idgen.ID
	for _, s := range raw {
		id, err := idgen.Parse(s)
		if err != nil {
			return nil, bad(what + " ID 不正确")
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// check 校验并规整请求体。
func (in *userInput) check() (p parsedInput, err error) {
	in.Username, in.Nickname = strings.TrimSpace(in.Username), strings.TrimSpace(in.Nickname)
	if !usernameRe.MatchString(in.Username) {
		return p, bad("账号为 2-64 位字母、数字或 _ . @ -")
	}
	if in.Nickname == "" {
		in.Nickname = in.Username
	}
	if utf8.RuneCountInString(in.Nickname) > 64 {
		return p, bad("昵称不超过 64 个字符")
	}
	if in.Phone = strings.TrimSpace(in.Phone); in.Phone != "" && !phoneRe.MatchString(in.Phone) {
		return p, bad("手机号格式不正确")
	}
	if in.Email = strings.TrimSpace(in.Email); in.Email != "" && (len(in.Email) > 100 || !emailRe.MatchString(in.Email)) {
		return p, bad("邮箱格式不正确")
	}
	if len(in.Avatar) > 500 || utf8.RuneCountInString(in.Remark) > 500 {
		return p, bad("头像地址或备注过长")
	}
	switch in.Gender {
	case "":
		in.Gender = "0"
	case "0", "1", "2":
	default:
		return p, bad("性别不正确")
	}
	switch in.Status {
	case "":
		in.Status = statusOn
	case statusOn, statusOff:
	default:
		return p, bad("状态不正确")
	}
	if !regionRe.MatchString(in.Region.Province) || !regionRe.MatchString(in.Region.City) || !regionRe.MatchString(in.Region.District) {
		return p, bad("地区编码不正确")
	}
	if strings.TrimSpace(in.DeptID) != "" {
		if p.dept, err = idgen.Parse(in.DeptID); err != nil {
			return p, bad("部门 ID 不正确")
		}
	}
	if p.posts, err = parseIDs(in.PostIDs, "岗位"); err != nil {
		return p, err
	}
	p.roles, err = parseIDs(in.RoleIDs, "角色")
	return p, err
}

// usernameTaken 判断当前租户内（未删除的）账号是否已存在。
func (s *service) usernameTaken(ctx context.Context, name string, exclude idgen.ID) (bool, error) {
	var n int64
	tx := s.db.WithContext(ctx).Model(&User{}).Where("username = ?", name)
	if exclude != 0 {
		tx = tx.Where("id <> ?", int64(exclude))
	}
	err := tx.Count(&n).Error
	return n > 0, err
}

// checkRoles 角色必须属于当前租户，防止把别的租户的角色挂到本租户用户上。
func (s *service) checkRoles(ctx context.Context, roles []idgen.ID) error {
	if len(roles) == 0 {
		return nil
	}
	var n int64
	err := s.db.WithContext(ctx).Table("sys_role").Where("id IN ? AND tenant_id = ?", roles, int64(tenantOf(ctx))).Count(&n).Error
	if err != nil {
		return err
	}
	if int(n) != len(roles) {
		return bad("所选角色不存在")
	}
	return nil
}

// replaceRelations 整体替换用户的角色与岗位。
func replaceRelations(tx *gorm.DB, uid idgen.ID, roles, posts []idgen.ID) error {
	if err := tx.Where("user_id = ?", int64(uid)).Delete(&userRole{}).Error; err != nil {
		return err
	}
	if err := tx.Where("user_id = ?", int64(uid)).Delete(&userPost{}).Error; err != nil {
		return err
	}
	if len(roles) > 0 {
		rows := make([]userRole, len(roles))
		for i, r := range roles {
			rows[i] = userRole{UserID: uid, RoleID: r}
		}
		if err := tx.Create(&rows).Error; err != nil {
			return err
		}
	}
	if len(posts) > 0 {
		rows := make([]userPost, len(posts))
		for i, p := range posts {
			rows[i] = userPost{UserID: uid, PostID: p}
		}
		return tx.Create(&rows).Error
	}
	return nil
}

func (s *service) create(ctx context.Context, in userInput, me actor) (*userDTO, error) {
	p, err := in.check()
	if err != nil {
		return nil, err
	}
	if taken, err := s.usernameTaken(ctx, in.Username, 0); err != nil {
		return nil, err
	} else if taken {
		return nil, bad("账号已存在")
	}
	if err := s.checkRoles(ctx, p.roles); err != nil {
		return nil, err
	}
	if err := s.guardUserWrite(ctx, me, nil, p); err != nil {
		return nil, err
	}
	// 初始密码取 sys_config 的 user.default_password；没配置则留空（账号不可登录，需先重置密码）。
	// 外部供号（NoPassword）永不使用共享默认口令：写入随机且无人知晓的强口令哈希。
	hash := ""
	if in.NoPassword {
		var err error
		if hash, err = unusablePasswordHash(); err != nil {
			return nil, err
		}
	} else {
		var pw string
		if err := s.db.WithContext(ctx).Table("sys_config").Select("config_value").Where("config_key = ?", "user.default_password").Limit(1).Scan(&pw).Error; err == nil && pw != "" {
			if hash, err = auth.HashPassword(pw); err != nil {
				return nil, err
			}
		}
	}
	u := &User{
		ExternalID: in.ExternalID, Username: in.Username, Password: hash, Nickname: in.Nickname, Avatar: in.Avatar, Phone: in.Phone, Email: in.Email,
		Gender: in.Gender, DeptID: p.dept, Status: in.Status, Remark: in.Remark,
		Province: in.Region.Province, City: in.Region.City, District: in.Region.District,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureUserQuota(ctx, tx, 1); err != nil {
			return err
		}
		if err := tx.Create(u).Error; err != nil {
			return err
		}
		return replaceRelations(tx, u.ID, p.roles, p.posts)
	})
	if err != nil {
		return nil, err
	}
	event.Publish(ctx, event.UserCreated, userEvent(u))
	s.mailAccount(ctx, u, "created")
	return s.get(ctx, u.ID)
}

func (s *service) update(ctx context.Context, id idgen.ID, in userInput, me actor) (*userDTO, error) {
	p, err := in.check()
	if err != nil {
		return nil, err
	}
	if err := s.ensureInScope(ctx, me, id); err != nil {
		return nil, err
	}
	u, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.guardTarget(ctx, me, u); err != nil {
		return nil, err
	}
	if in.Status != statusOn && (u.IsSuper || u.ID == me.ID) {
		return nil, bad("超级管理员和自己不能被停用")
	}
	if in.Username != u.Username {
		if taken, err := s.usernameTaken(ctx, in.Username, id); err != nil {
			return nil, err
		} else if taken {
			return nil, bad("账号已存在")
		}
	}
	if err := s.checkRoles(ctx, p.roles); err != nil {
		return nil, err
	}
	if err := s.guardUserWrite(ctx, me, u, p); err != nil {
		return nil, err
	}
	u.Username, u.Nickname, u.Avatar, u.Phone, u.Email, u.Gender = in.Username, in.Nickname, in.Avatar, in.Phone, in.Email, in.Gender
	if in.ExternalIDSet || in.ExternalID != "" {
		u.ExternalID = in.ExternalID
	}
	u.DeptID, u.Status, u.Remark = p.dept, in.Status, in.Remark
	u.Province, u.City, u.District = in.Region.Province, in.Region.City, in.Region.District
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(u).Error; err != nil {
			return err
		}
		return replaceRelations(tx, id, p.roles, p.posts)
	})
	if err != nil {
		return nil, err
	}
	event.Publish(ctx, event.UserUpdated, userEvent(u))
	return s.get(ctx, id)
}

// unusablePasswordHash 返回 32 字节随机口令的 bcrypt 哈希，口令本身立即丢弃。
func unusablePasswordHash() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return auth.HashPassword(hex.EncodeToString(b))
}
