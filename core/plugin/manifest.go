package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestFile 是清单文件名。
const ManifestFile = "plugin.yaml"

var idRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidID 判断插件 ID 是否合法：小写字母、数字、连字符，不以连字符开头结尾，最长 48。
func ValidID(id string) bool { return len(id) <= 48 && idRe.MatchString(id) }

// MenuSpec 是清单里 menus 的一项，安装/启动时写入 sys_menu。
// 业务键 code 与内置规则一致：目录用 "dir:"+path，菜单/按钮用权限码。
// Parent 写父节点的 code（目录可以直接写它的 path，例如 "/system"）；留空表示顶级。
type MenuSpec struct {
	Parent    string `yaml:"parent" json:"parent"`
	Type      string `yaml:"type" json:"type"` // dir | menu | button
	Title     string `yaml:"title" json:"title"`
	Icon      string `yaml:"icon" json:"icon"`
	Path      string `yaml:"path" json:"path"`
	Component string `yaml:"component" json:"component"`
	Perm      string `yaml:"perm" json:"perm"`
	Sort      int    `yaml:"sort" json:"sort"`
	KeepAlive bool   `yaml:"keep_alive" json:"keepAlive"`
	Hidden    bool   `yaml:"hidden" json:"hidden"`
}

// Code 返回菜单的稳定业务键。
func (m MenuSpec) Code() string {
	if m.Type == "dir" {
		return "dir:" + m.Path
	}
	return m.Perm
}

// Manifest 是 plugins/<id>/plugin.yaml。
type Manifest struct {
	ID               string     `yaml:"id"`
	Name             string     `yaml:"name"`
	Version          string     `yaml:"version"`
	Description      string     `yaml:"description"`
	Author           string     `yaml:"author"`
	Edition          Edition    `yaml:"edition"`
	RequiresNexadmin string     `yaml:"requires_nexadmin"`
	MigrationTarget  string     `yaml:"migration_target"` // empty defaults to platform; tenant explicitly opts into each business DB
	Menus            []MenuSpec `yaml:"menus"`
}

// ParseManifest 解析并校验清单内容。
func ParseManifest(raw []byte) (*Manifest, error) {
	var m Manifest
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("plugin.yaml 解析失败: %w", err)
	}
	// "parent: /" 表示顶级目录（payment/im/website 等自建顶级目录的插件这么写），
	// 归一化为留空；否则 seed 找不到 code 为 "/" 的父节点会整棵跳过。
	for i := range m.Menus {
		if m.Menus[i].Parent == "/" {
			m.Menus[i].Parent = ""
		}
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// LoadManifest 读取并校验 <dir>/plugin.yaml。
func LoadManifest(dir string) (*Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, fmt.Errorf("读取 plugin.yaml 失败: %w", err)
	}
	return ParseManifest(raw)
}

// Validate 校验清单字段。
func (m *Manifest) Validate() error {
	if !ValidID(m.ID) {
		return fmt.Errorf("plugin.yaml: id %q 不合法（小写字母、数字、连字符，最长 48）", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("plugin.yaml: name 不能为空")
	}
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("plugin.yaml: version 不能为空")
	}
	if m.Edition != Free && m.Edition != Pro {
		return fmt.Errorf("plugin.yaml: edition 必须是 free 或 pro，当前为 %q", m.Edition)
	}
	if m.MigrationTarget != "" && m.MigrationTarget != "platform" && m.MigrationTarget != "tenant" {
		return fmt.Errorf("plugin.yaml: migration_target 必须是 platform 或 tenant")
	}
	seen := map[string]bool{}
	for i, mu := range m.Menus {
		if err := m.validateMenu(mu); err != nil {
			return fmt.Errorf("plugin.yaml: menus[%d]: %w", i, err)
		}
		if seen[mu.Code()] {
			return fmt.Errorf("plugin.yaml: menus[%d]: 重复的菜单键 %q", i, mu.Code())
		}
		seen[mu.Code()] = true
	}
	return nil
}

func (m *Manifest) validateMenu(mu MenuSpec) error {
	if strings.TrimSpace(mu.Title) == "" {
		return fmt.Errorf("title 不能为空")
	}
	switch mu.Type {
	case "dir":
		if mu.Path == "" {
			return fmt.Errorf("目录必须有 path")
		}
	case "menu":
		if mu.Path == "" || mu.Perm == "" {
			return fmt.Errorf("菜单必须有 path 与 perm")
		}
	case "button":
		if mu.Perm == "" {
			return fmt.Errorf("按钮必须有 perm")
		}
	default:
		return fmt.Errorf("type 必须是 dir / menu / button，当前为 %q", mu.Type)
	}
	if mu.Perm != "" && !strings.HasPrefix(mu.Perm, m.ID+":") {
		return fmt.Errorf("权限码 %q 必须以 %q 开头", mu.Perm, m.ID+":")
	}
	if len(mu.Code()) > 100 {
		return fmt.Errorf("菜单键过长（最多 100 字符）")
	}
	return nil
}
