// Package password 是全站共用的密码策略：sys_config 里的 user.password_min_length /
// user.password_require_mixed 两个参数（出厂种子见 modules/system/config 的 factoryRows）
// 折算成一条校验规则，供个人中心改密、用户新建与管理员重置密码三条链路共用。
// 之前只有「非空 ≤72 字节」一条线，弱口令和共享初始口令都能直接落库。
package password

import (
	"context"
	"strconv"
	"strings"
	"unicode"

	"gorm.io/gorm"

	"nexadmin/core/response"
)

// sys_config 里的策略键与参数页展示一致。
const (
	KeyMinLength    = "user.password_min_length"
	KeyRequireMixed = "user.password_require_mixed"
	KeyDefault      = "user.default_password"
)

// DefaultLen 是出厂最小长度：策略行缺失或值非法时按它执行——宁可保守也不放开。
const DefaultLen = 8

// Policy 是生效中的密码策略。
type Policy struct {
	MinLength    int
	RequireMixed bool // true = 必须同时包含字母和数字
}

// Load 从 sys_config 读策略；行缺失 / 读库失败 / 值非法都回退出厂默认。
// 每次现读而不缓存：参数页改完下一次改密即生效，读的是单行主键查询，代价可忽略。
func Load(ctx context.Context, db *gorm.DB) Policy {
	p := Policy{MinLength: DefaultLen}
	if db == nil {
		return p
	}
	var minStr, mixedStr string
	_ = db.WithContext(ctx).Table("sys_config").Select("config_value").Where("config_key = ?", KeyMinLength).Limit(1).Scan(&minStr).Error
	_ = db.WithContext(ctx).Table("sys_config").Select("config_value").Where("config_key = ?", KeyRequireMixed).Limit(1).Scan(&mixedStr).Error
	if n, err := strconv.Atoi(strings.TrimSpace(minStr)); err == nil && n >= 1 && n <= 72 {
		p.MinLength = n
	}
	p.RequireMixed = strings.TrimSpace(mixedStr) == "true"
	return p
}

// DefaultPassword 读 user.default_password（新用户与重置密码共享的初始口令）；
// 行缺失或空串返回 ""，调用方按「无初始密码」处理。
func DefaultPassword(ctx context.Context, db *gorm.DB) string {
	var pw string
	if db != nil {
		_ = db.WithContext(ctx).Table("sys_config").Select("config_value").Where("config_key = ?", KeyDefault).Limit(1).Scan(&pw).Error
	}
	return strings.TrimSpace(pw)
}

// Validate 校验新密码是否符合策略；defaultPw 非空时同时拒绝「与初始口令相同」。
// 返回 *response.Error（key 与 core/i18n 文案对应），nil = 通过。
func (p Policy) Validate(pw, defaultPw string) *response.Error {
	if r := []rune(pw); len(r) < p.MinLength {
		return response.BadRequest("password.too_short", p.MinLength)
	}
	if p.RequireMixed {
		var hasLetter, hasDigit bool
		for _, c := range pw {
			switch {
			case unicode.IsLetter(c):
				hasLetter = true
			case unicode.IsDigit(c):
				hasDigit = true
			}
		}
		if !hasLetter || !hasDigit {
			return response.BadRequest("password.require_mixed")
		}
	}
	if defaultPw != "" && pw == defaultPw {
		return response.BadRequest("password.same_as_default")
	}
	return nil
}

// ValidateInitial 校验「新用户的初始密码」（即 user.default_password 本身）是否满足策略。
// 初始口令是给用户首次登录用的，策略太弱等于出厂弱口令；不满足时提示先调参数再建号。
func (p Policy) ValidateInitial(pw string) *response.Error {
	if r := []rune(pw); len(r) < p.MinLength {
		return response.BadRequest("password.default_weak", p.MinLength)
	}
	if p.RequireMixed {
		var hasLetter, hasDigit bool
		for _, c := range pw {
			switch {
			case unicode.IsLetter(c):
				hasLetter = true
			case unicode.IsDigit(c):
				hasDigit = true
			}
		}
		if !hasLetter || !hasDigit {
			return response.BadRequest("password.default_weak", p.MinLength)
		}
	}
	return nil
}
