package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"nexadmin/core/plugin"
)

const pluginCreateUsage = "用法: nexadmin plugin create <id> [--out <目录>] [--name <名称>] [--edition free|pro]"

// 模板以框架 0.2.0 的公开接口为基线；不依赖运行目录，独立 CLI 也可在项目外创作插件。
const pluginTemplateRequires = ">=0.2.0"

func cmdPluginCreate(args []string, out io.Writer) error {
	fl := newFlags("plugin create")
	dst := fl.String("out", "", "输出目录（默认 ./<id>，不覆盖）")
	name := fl.String("name", "", "显示名称（默认 id）")
	edition := fl.String("edition", "free", "free 或 pro")
	var pos []string
	for rest := args; len(rest) > 0; {
		if err := fl.Parse(rest); err != nil {
			return fmt.Errorf("%w\n%s", err, pluginCreateUsage)
		}
		if fl.NArg() == 0 {
			break
		}
		pos = append(pos, fl.Arg(0))
		rest = fl.Args()[1:]
	}
	if len(pos) != 1 {
		return errors.New(pluginCreateUsage)
	}
	id := pos[0]
	if !plugin.ValidID(id) {
		return fmt.Errorf("非法的插件 id %q（小写字母、数字、连字符，最长 48）", id)
	}
	if *edition != string(plugin.Free) && *edition != string(plugin.Pro) {
		return fmt.Errorf("edition 必须是 free 或 pro，当前为 %q", *edition)
	}
	if *name == "" {
		*name = id
	}
	if strings.TrimSpace(*name) == "" {
		return errors.New("name 不能为空白")
	}
	if *dst == "" {
		*dst = id
	}
	abs, err := filepath.Abs(*dst)
	if err != nil {
		return err
	}
	files, err := pluginCreateFiles(id, *name, *edition)
	if err != nil {
		return err
	}
	// Mkdir（不是 MkdirAll）独占目标：包括空目录、文件和悬空符号链接，均不覆盖。
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(abs, 0o755); err != nil {
		return fmt.Errorf("创建插件目标失败（已有目标不会覆盖）%s: %w", abs, err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(abs) // 只清理由本次调用独占创建的目录。
		}
	}()
	for rel, data := range files {
		path := filepath.Join(abs, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	complete = true
	fmt.Fprintf(out, "已创建插件 %s（%s，%d 个文件）: %s\n先运行 nexadmin plugin lint %q，再在派生项目中 plugin install 此目录；构建重启后生效。\n", id, *edition, len(files), abs, abs)
	return nil
}

func pluginCreateFiles(id, name, edition string) (map[string][]byte, error) {
	data := struct{ ID, Name, Package, Edition, Requires string }{
		id, name, "plugin_" + strings.ReplaceAll(id, "-", "_"), "Free", pluginTemplateRequires,
	}
	if edition == "pro" {
		data.Edition = "Pro"
	}
	files := map[string][]byte{}
	for rel, src := range pluginCreateTemplates {
		tpl, err := template.New(rel).Funcs(template.FuncMap{"quote": strconv.Quote}).Parse(src)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := tpl.Execute(&buf, data); err != nil {
			return nil, err
		}
		body := buf.Bytes()
		if strings.HasSuffix(rel, ".go") {
			body, err = format.Source(body)
			if err != nil {
				return nil, fmt.Errorf("模板 %s: %w", rel, err)
			}
		}
		files[strings.ReplaceAll(rel, "<id>", id)] = body
	}
	// JSON 编码名称，避免引号、换行等用户输入破坏清单或词条。
	man := fmt.Sprintf("id: %s\nname: %s\nversion: 0.1.0\nedition: %s\nrequires_nexadmin: %s\nmenus:\n  - type: menu\n    title: plugin_%s.menu\n    path: /plugins/%s\n    component: plugins/%s/PluginPage\n    perm: %s:list\n    icon: Puzzle\n    sort: 100\n", id, jsonQuote(name), edition, jsonQuote(data.Requires), id, id, id, id)
	files[plugin.ManifestFile] = []byte(man)
	for locale, terms := range map[string]map[string]string{
		"zh-CN": {"menu": name, "description": "插件状态示例，不包含业务数据。", "ready": "插件已就绪", "empty": "暂无业务数据", "loading": "正在连接插件…", "error": "无法读取插件状态"},
		"en-US": {"menu": name, "description": "Plugin status example. No business data is included.", "ready": "Plugin is ready", "empty": "No business data yet", "loading": "Connecting to the plugin…", "error": "Unable to load plugin status"},
	} {
		body, err := json.MarshalIndent(map[string]any{"plugin_" + id: terms}, "", "  ")
		if err != nil {
			return nil, err
		}
		files["web/locales/"+locale+".json"] = append(body, '\n')
	}
	return files, nil
}

var pluginCreateTemplates = map[string]string{
	"backend/plugin.go": `package {{.Package}}

import (
    "github.com/gin-gonic/gin"
    "nexadmin/core"
    "nexadmin/core/plugin"
    "nexadmin/core/response"
)

func init() {
    plugin.Register(plugin.Plugin{
        ID: {{quote .ID}}, Name: {{quote .Name}}, Version: "0.1.0", Edition: plugin.{{.Edition}},
        Module: &module{},
    })
}

type module struct{}

func (*module) Name() string { return {{quote .ID}} }

func (*module) Register(r *gin.RouterGroup, _ *core.Deps) {
    r.GET("/plugins/{{.ID}}/status", core.Perm("{{.ID}}:list"), status)
}

// status 只展示插件连通状态，不读取业务数据，不需要数据库迁移。
func status(c *gin.Context) {
    response.OK(c, gin.H{"ready": true})
}
`,
	"backend/plugin_test.go": `package {{.Package}}

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/gin-gonic/gin"
    "nexadmin/core/auth"
)

func TestStatusPermission(t *testing.T) {
    for _, tc := range []struct {
        name string
        claims *auth.Claims
        want int
    }{
        {"anonymous", nil, http.StatusUnauthorized},
        {"no permission", &auth.Claims{Principal: auth.PrincipalApp}, http.StatusForbidden},
        {"allowed", &auth.Claims{Principal: auth.PrincipalApp, Perms: []string{"{{.ID}}:list"}}, http.StatusOK},
    } {
        t.Run(tc.name, func(t *testing.T) {
            r := gin.New()
            r.Use(func(c *gin.Context) { if tc.claims != nil { auth.SetClaims(c, tc.claims) }; c.Next() })
            (&module{}).Register(r.Group("/api/v1"), nil)
            w := httptest.NewRecorder()
            r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/plugins/{{.ID}}/status", nil))
            if w.Code != tc.want { t.Fatalf("status = %d, want %d: %s", w.Code, tc.want, w.Body.String()) }
            if tc.want == http.StatusOK {
                var body struct { Data struct { Ready bool } }
                if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || !body.Data.Ready {
                    t.Fatalf("invalid status: %s (%v)", w.Body.String(), err)
                }
            }
        })
    }
}
`,
	"web/route.tsx": `import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'plugins/{{.ID}}',
    component: lazy(() => import('./PluginPage')),
    perm: '{{.ID}}:list',
    menu: { title: 'plugin_{{.ID}}.menu', icon: 'Puzzle', order: 100 },
  },
]
`,
	"web/PluginPage.tsx": `import { useTranslation } from 'react-i18next'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { EmptyState } from '@/framework/components/EmptyState'
import { usePluginStatus } from './api'

export default function PluginPage() {
  const { t } = useTranslation()
  const status = usePluginStatus()
  const title = status.isPending
    ? t('plugin_{{.ID}}.loading')
    : status.isError || !status.data?.ready
      ? t('plugin_{{.ID}}.error')
      : t('plugin_{{.ID}}.ready')

  return (
    <PageContainer>
      <PageHeader title={t('plugin_{{.ID}}.menu')} description={t('plugin_{{.ID}}.description')} />
      <div className="rounded-lg border border-border bg-card text-card-foreground">
        <EmptyState title={title} description={t('plugin_{{.ID}}.empty')} />
      </div>
    </PageContainer>
  )
}
`,
	"web/api.ts": `import { useQuery } from '@tanstack/react-query'
import { api } from '@/framework/request/http'

export interface PluginStatus { ready: boolean }

export function usePluginStatus() {
  return useQuery({
    queryKey: ['plugin_{{.ID}}', 'status'],
    queryFn: () => api.get<PluginStatus>('/plugins/{{.ID}}/status'),
  })
}
`,
	"web/<id>.mock.ts": `import { defineMock } from '@/framework/request/mock'
import type { PluginStatus } from './api'

// 与真实接口相同：仅返回连通状态，不伪造业务行。
defineMock([
  {
    method: 'GET',
    url: '/plugins/{{.ID}}/status',
    handler: (): PluginStatus => ({ ready: true }),
  },
])
`,
}
