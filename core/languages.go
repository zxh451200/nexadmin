package core

import (
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/core/i18n"
)

// Language 是一门语言的完整档案。
//
// 进程内只有这里持有语言清单：界面文案协商、公开站按前缀注册路由、字典标签、
// 导出表头都从 Languages() 取。config.yaml 的 languages 只是首次启动的种子，
// 落库后以 sys_language 为准——避免出现两份互不可见的清单。
type Language = config.Language

// languageRow 是 sys_language 的落库形态。
type languageRow struct {
	database.PlatformModel
	Code       string `gorm:"column:code;size:16;not null"`
	Name       string `gorm:"column:name;size:64;not null"`
	Short      string `gorm:"column:short;size:8;not null;default:''"`
	Dir        string `gorm:"column:dir;size:3;not null;default:'ltr'"`
	HTMLLang   string `gorm:"column:html_lang;size:16;not null"`
	OGLocale   string `gorm:"column:og_locale;size:32;not null;default:''"`
	Prefix     string `gorm:"column:prefix;size:16;not null;default:''"`
	DateFormat string `gorm:"column:date_format;size:32;not null;default:''"`
	IsDefault  bool   `gorm:"column:is_default;not null;default:false"`
	Sort       int    `gorm:"column:sort;not null;default:0"`
}

func (languageRow) TableName() string { return "sys_language" }

func (r languageRow) toLanguage() Language {
	return Language{
		Code:       r.Code,
		Name:       r.Name,
		Short:      r.Short,
		Dir:        r.Dir,
		HTMLLang:   r.HTMLLang,
		OGLocale:   r.OGLocale,
		Prefix:     r.Prefix,
		DateFormat: r.DateFormat,
		Default:    r.IsDefault,
	}
}

var (
	languageCodeRe = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
	// rtlRe 与前端 framework/i18n 的判定保持一致：dir 缺省时按语基推出来。
	rtlRe = regexp.MustCompile(`^(ar|he|fa|ur|yi|ps|ku)(-|$)`)

	loadedLangs atomic.Pointer[[]Language]
)

// NormalizeLanguages 校验并补全一份语言清单：后台保存与启动加载共用同一套规则。
// 补全规则让 config.yaml 可以只写 code/name/dir/default，其余按语标推导。
func NormalizeLanguages(list []Language) ([]Language, error) {
	if len(list) == 0 {
		return nil, fmt.Errorf("至少需要一种语言")
	}
	out := make([]Language, 0, len(list))
	seenCode := map[string]bool{}
	seenPrefix := map[string]bool{}
	hasDefault := false
	for _, l := range list {
		l.Code = strings.TrimSpace(l.Code)
		l.Name = strings.TrimSpace(l.Name)
		l.Prefix = strings.Trim(strings.TrimSpace(l.Prefix), "/")
		l.HTMLLang = strings.TrimSpace(l.HTMLLang)
		l.Dir = strings.ToLower(strings.TrimSpace(l.Dir))
		if l.Code == "" || !languageCodeRe.MatchString(l.Code) {
			return nil, fmt.Errorf("语言代码不合法: %q（示例 zh-CN、en-US、ja-JP）", l.Code)
		}
		if seenCode[l.Code] {
			return nil, fmt.Errorf("语言代码重复: %s", l.Code)
		}
		seenCode[l.Code] = true
		if l.Name == "" {
			l.Name = l.Code
		}
		if l.Short == "" {
			// 语言切换器显示的是 Short；留空会渲染出一个没有文字的按钮，所以兜底成语基大写。
			l.Short = strings.ToUpper(prefixOf(l.Code))
		}
		if l.HTMLLang == "" {
			l.HTMLLang = l.Code
		}
		if l.OGLocale == "" {
			l.OGLocale = strings.ReplaceAll(l.Code, "-", "_")
		}
		if l.DateFormat == "" {
			l.DateFormat = "2006-01-02"
		}
		if l.Dir == "" {
			l.Dir = "rtl"
			if !rtlRe.MatchString(l.Code) {
				l.Dir = "ltr"
			}
		}
		if l.Dir != "ltr" && l.Dir != "rtl" {
			return nil, fmt.Errorf("语言 %s 的 dir 只能是 ltr 或 rtl，实得 %q", l.Code, l.Dir)
		}
		if l.Default {
			if hasDefault {
				return nil, fmt.Errorf("默认语言只能有一个")
			}
			hasDefault = true
			if l.Prefix != "" {
				return nil, fmt.Errorf("默认语言必须挂在根路径上，不能设置前缀")
			}
		} else if l.Prefix == "" {
			// 种子只写了 code 时按语基推导前缀（en-US → en）；撞车就退回完整码（en-GB → en_gb）。
			l.Prefix = prefixOf(l.Code)
			if seenPrefix[l.Prefix] {
				l.Prefix = strings.ToLower(strings.ReplaceAll(l.Code, "-", "_"))
			}
		}
		if !l.Default {
			if l.Prefix == "" {
				return nil, fmt.Errorf("非默认语言必须配置路径前缀: %s", l.Code)
			}
			if strings.ContainsAny(l.Prefix, "/?# ") {
				return nil, fmt.Errorf("语言路径前缀非法: %s", l.Prefix)
			}
		}
		if seenPrefix[l.Prefix] {
			return nil, fmt.Errorf("语言路径前缀重复: %s", l.Prefix)
		}
		seenPrefix[l.Prefix] = true
		out = append(out, l)
	}
	if !hasDefault {
		return nil, fmt.Errorf("必须有一个默认语言")
	}
	return out, nil
}

// prefixOf 取语基做前缀：en-US → en，zh-Hans-CN → zh。
func prefixOf(code string) string {
	if base, _, found := strings.Cut(code, "-"); found {
		return strings.ToLower(base)
	}
	return strings.ToLower(code)
}

// SyncLanguageTable 在迁移之后调用：sys_language 为空就用 config.yaml 播种，
// 然后以表为准重建进程内的语言清单。返回归一化后的清单。
//
// 播种而不是报错，是为了让「只在 config.yaml 里加语言」这条既有路径继续可用；
// 一旦表里有了数据，改 config 不再影响清单（后台语言管理页成为唯一入口）。
func SyncLanguageTable(db *gorm.DB, cfg *config.Config) ([]Language, error) {
	if db == nil {
		return nil, fmt.Errorf("core: 数据库不可用")
	}
	var rows []languageRow
	if err := db.Order("sort ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("core: 读取 sys_language: %w", err)
	}
	if len(rows) == 0 {
		seed, err := NormalizeLanguages(cfg.Languages)
		if err != nil {
			return nil, fmt.Errorf("core: config.yaml 的 languages 不合法: %w", err)
		}
		newRows := make([]languageRow, 0, len(seed))
		for i, l := range seed {
			newRows = append(newRows, rowOf(l, (i+1)*10))
		}
		// OnConflict DoNothing：并发启动时另一实例可能刚播种完，不能让本实例报错退出。
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&newRows).Error; err != nil {
			return nil, fmt.Errorf("core: 播种 sys_language: %w", err)
		}
		rows = newRows
	}
	list := make([]Language, 0, len(rows))
	for _, r := range rows {
		list = append(list, r.toLanguage())
	}
	normalized, err := NormalizeLanguages(list)
	if err != nil {
		return nil, fmt.Errorf("core: sys_language 内容不合法: %w", err)
	}
	loadedLangs.Store(&normalized)
	return normalized, nil
}

func rowOf(l Language, sort int) languageRow {
	return languageRow{
		Code:       l.Code,
		Name:       l.Name,
		Short:      l.Short,
		Dir:        l.Dir,
		HTMLLang:   l.HTMLLang,
		OGLocale:   l.OGLocale,
		Prefix:     l.Prefix,
		DateFormat: l.DateFormat,
		IsDefault:  l.Default,
		Sort:       sort,
	}
}

// ReloadLanguages 把进程内的语言清单与文案 bundle 换成库里那份。
// 模块在保存清单后也要调用，否则界面文案的协商表还停在旧语言上。
func ReloadLanguages(db *gorm.DB, cfg *config.Config) error {
	list, err := SyncLanguageTable(db, cfg)
	if err != nil {
		return err
	}
	def := cfg.DefaultLanguage()
	for _, l := range list {
		if l.Default {
			def = l.Code
			break
		}
	}
	b, err := i18n.New(list, def, cfg.LocalesDir)
	if err != nil {
		return err
	}
	i18n.SetDefault(b)
	return nil
}

// SaveLanguageList 校验后整表替换 sys_language，并立即重建进程内清单。
// 返回落库后的规范清单（字段已补全）。
//
// 公开站按前缀注册的路由在引擎构建期生成，运行中不能加路由：调用方必须把
// 「需重启生效」告诉用户，这里不试图热改路由。
func SaveLanguageList(db *gorm.DB, cfg *config.Config, list []Language) ([]Language, error) {
	if db == nil {
		return nil, fmt.Errorf("core: 数据库不可用")
	}
	normalized, err := NormalizeLanguages(list)
	if err != nil {
		return nil, err
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id > 0").Delete(&languageRow{}).Error; err != nil {
			return err
		}
		rows := make([]languageRow, 0, len(normalized))
		for i, l := range normalized {
			rows = append(rows, rowOf(l, (i+1)*10))
		}
		return tx.Create(&rows).Error
	}); err != nil {
		return nil, err
	}
	if err := ReloadLanguages(db, cfg); err != nil {
		return nil, err
	}
	return Languages(), nil
}

// Languages 返回进程内已加载的语言清单；SyncLanguageTable 之前回落到 config 清单。
func Languages() []Language {
	if p := loadedLangs.Load(); p != nil {
		return *p
	}
	return nil
}

// reloadLanguages 是启动期的入口：迁移刚建好 sys_language，这里播种或读回，
// 并据此重建文案 bundle。调用点必须早于模块注册——官网要按清单里的前缀注册路由。
func (a *App) reloadLanguages(db *gorm.DB) error {
	return ReloadLanguages(db, a.cfg)
}

// DefaultLanguage 返回默认语言；没有加载清单时返回零值，调用方按空 Code 判断。
func DefaultLanguage() Language {
	for _, l := range Languages() {
		if l.Default {
			return l
		}
	}
	return Language{}
}

// LanguageByCode 按码精确取语言，未命中再按语基回退（en-GB → en → en-US），
// 与内容翻译取值时的回退口径保持一致。
func LanguageByCode(code string) (Language, bool) {
	want := strings.ToLower(strings.TrimSpace(code))
	all := Languages()
	for _, l := range all {
		if strings.ToLower(l.Code) == want {
			return l, true
		}
	}
	if base, _, found := strings.Cut(want, "-"); found {
		for _, l := range all {
			if strings.ToLower(prefixOf(l.Code)) == base {
				return l, true
			}
		}
	}
	return Language{}, false
}
