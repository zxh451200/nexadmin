package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nexadmin/pkg/vrange"
)

// FrameworkManifestFile 是项目根的框架清单文件名，框架版本以其中的 framework_version 为准。
const FrameworkManifestFile = ".nexadmin.manifest.json"

// FrameworkVersion 解析当前框架版本：优先用 hint（运行时是 -ldflags 注入的 version，"dev" 之类不可解析的值会被忽略），
// 否则读 root/.nexadmin.manifest.json 的 framework_version。都取不到返回 ""，调用方应跳过版本校验而不是误拒。
func FrameworkVersion(root, hint string) string {
	if _, err := vrange.Parse(hint); err == nil {
		return strings.TrimSpace(hint)
	}
	raw, err := os.ReadFile(filepath.Join(root, FrameworkManifestFile))
	if err != nil {
		return ""
	}
	var m struct {
		FrameworkVersion string `json:"framework_version"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	if _, err := vrange.Parse(m.FrameworkVersion); err != nil {
		return ""
	}
	return strings.TrimSpace(m.FrameworkVersion)
}

// CheckRequires 按 plugin.yaml 的 requires_nexadmin 校验框架版本。requires 为空或 fwVersion 未知时通过；
// 范围写错（无法解析）视为不满足，免得一个拼错的约束被静默放行。错误信息直接展示给用户。
func CheckRequires(requires, fwVersion string) error {
	requires = strings.TrimSpace(requires)
	if requires == "" || fwVersion == "" {
		return nil
	}
	ok, err := vrange.Satisfies(fwVersion, requires)
	if err != nil {
		return fmt.Errorf("plugin.yaml 的 requires_nexadmin %q 无法解析: %v", requires, err)
	}
	if !ok {
		return fmt.Errorf("插件要求 NexAdmin %s，当前框架版本 %s 不满足", requires, fwVersion)
	}
	return nil
}
