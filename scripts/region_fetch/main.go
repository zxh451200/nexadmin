// Command region_fetch 把公开发布的扁平省市区表（code → name 的 JSON 对象）转成
// core/region 使用的三级树，输出交给 scripts/region_import 校验。
//
// 只读取调用方显式给出的地址/文件，不内置任何下载源；--source 支持 HTTPS 地址或本地路径。
// 拼音不写进文件：core/region 在加载时按 core/region/pinyin.go 的规则生成，
// 这样改读音表不需要重新生成数据。
//
// 用法（以 mumuy/data_location 的 list.json 为例）：
//
//	go run ./scripts/region_fetch -source https://raw.githubusercontent.com/mumuy/data_location/master/list.json -out /tmp/region/src.json
//	go run ./scripts/region_import -input /tmp/region/src.json -out-dir /tmp/region/out \
//	  -format tree -source-url <上面的地址> -version <版本> -data-date <YYYY-MM-DD> \
//	  -license MIT -authorization "<授权声明>" -direct-counties <脚本打印> -childless-prefectures <脚本打印>
//	cp /tmp/region/out/{regions.json,manifest.json} core/region/data/
//	go run ./scripts/region_fetch -from core/region/data/regions.json -web-mock web/src/framework/request/regions.mock.ts
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nexadmin/core/region"
)

const maxBytes = 16 << 20

func main() {
	source := flag.String("source", "", "必填：扁平 code→name JSON 的 HTTPS 地址或本地文件")
	out := flag.String("out", "", "必填：输出树 JSON 的路径（已存在则拒绝覆盖）")
	from := flag.String("from", "", "已校验的 regions.json；与 -web-mock 搭配重新生成前端 mock")
	webMock := flag.String("web-mock", "", "输出前端 mock TS 的路径（覆盖）")
	flag.Parse()
	if strings.TrimSpace(*webMock) != "" {
		if strings.TrimSpace(*from) == "" {
			fail(errors.New("-web-mock 需要配合 -from <已校验的 regions.json>"))
		}
		writeWebMock(*from, *webMock)
		return
	}
	if strings.TrimSpace(*source) == "" || strings.TrimSpace(*out) == "" {
		fail(errors.New("必须提供 -source 与 -out"))
	}
	raw, err := readSource(*source)
	if err != nil {
		fail(err)
	}
	var flat map[string]string
	if err := json.Unmarshal(raw, &flat); err != nil {
		fail(fmt.Errorf("输入应是 code→name 的 JSON 对象: %w", err))
	}
	if len(flat) == 0 {
		fail(errors.New("输入为空"))
	}
	tree, err := region.FromFlat(flat)
	if err != nil {
		fail(err)
	}
	if _, err := os.Lstat(*out); !os.IsNotExist(err) {
		fail(fmt.Errorf("输出文件已存在，请换个路径: %s", *out))
	}
	if err := os.WriteFile(*out, append(tree, '\n'), 0o644); err != nil {
		fail(err)
	}
	var roots []*region.Node
	if err := json.Unmarshal(tree, &roots); err != nil {
		fail(err)
	}
	provinces, prefectures, counties := count(roots)
	fmt.Printf("已写出 %s：省级 %d / 地级 %d / 县级 %d\n", *out, provinces, prefectures, counties)
	direct, childless := declarations(roots)
	fmt.Printf("-direct-counties %s\n", strings.Join(direct, ","))
	fmt.Printf("-childless-prefectures %s\n", strings.Join(childless, ","))
}

func readSource(source string) ([]byte, error) {
	if u, err := url.Parse(source); err == nil && u.Scheme != "" {
		if u.Scheme != "https" {
			return nil, fmt.Errorf("只接受 HTTPS 地址: %s", source)
		}
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Get(source)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("数据源返回 %d", resp.StatusCode)
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
		if err != nil {
			return nil, err
		}
		if len(raw) > maxBytes {
			return nil, fmt.Errorf("数据源超过 %d 字节上限", maxBytes)
		}
		return raw, nil
	}
	if filepath.Ext(source) == "" {
		return nil, fmt.Errorf("无法识别的来源: %s", source)
	}
	return os.ReadFile(source)
}

func count(roots []*region.Node) (provinces, prefectures, counties int) {
	for _, p := range roots {
		provinces++
		for _, c := range p.Children {
			if strings.HasSuffix(c.Code, "00") {
				prefectures++
			} else {
				counties++
			}
			counties += len(c.Children)
		}
	}
	return
}

// declarations 算出 region_import 需要显式声明的两类代码：
// 直辖市以外的省直辖县、以及没有县级下辖的地级市。
func declarations(roots []*region.Node) (direct, childless []string) {
	municipalities := map[string]bool{"110000": true, "120000": true, "310000": true, "500000": true}
	for _, p := range roots {
		for _, c := range p.Children {
			if strings.HasSuffix(c.Code, "00") {
				if len(c.Children) == 0 {
					childless = append(childless, c.Code)
				}
				continue
			}
			if !municipalities[p.Code] {
				direct = append(direct, c.Code)
			}
		}
	}
	sort.Strings(direct)
	sort.Strings(childless)
	return
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "region_fetch:", err)
	os.Exit(1)
}

// writeWebMock 用同一棵树重新生成前端 mock（web/src/framework/request/regions.mock.ts）。
// 拼音同样出自 region.Pinyin，所以 mock 与后端不会给出两套搜索结果。
func writeWebMock(from, target string) {
	raw, err := os.ReadFile(from)
	if err != nil {
		fail(err)
	}
	var roots []*region.Node
	if err := json.Unmarshal(raw, &roots); err != nil {
		fail(err)
	}
	var b strings.Builder
	b.WriteString(mockHeader)
	renderNodes(&b, roots, 1)
	b.WriteString(mockFooter)
	if err := os.WriteFile(target, []byte(b.String()), 0o644); err != nil {
		fail(err)
	}
	provinces, prefectures, counties := count(roots)
	fmt.Printf("已写出 %s：省级 %d / 地级 %d / 县级 %d\n", target, provinces, prefectures, counties)
}

func renderNodes(b *strings.Builder, list []*region.Node, depth int) {
	pad := strings.Repeat("  ", depth)
	for _, n := range list {
		fmt.Fprintf(b, "%s['%s', '%s', '%s'", pad, n.Code, jsText(n.Name), region.Pinyin(n.Name))
		if len(n.Children) > 0 {
			b.WriteString(", [\n")
			renderNodes(b, n.Children, depth+1)
			b.WriteString(pad + "]],\n")
			continue
		}
		b.WriteString("],\n")
	}
}

func jsText(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\`), "'", `\'`)
}

const mockHeader = `import { defineMock } from './mock'
import type { RegionNode } from './types'

// [code, name, pinyin, children?]
// 由 scripts/region_fetch 从 core/region/data/regions.json 生成，拼音与后端一致；改数据请重新生成而不是手改。
type Raw = [string, string, string, Raw[]?]

const raw: Raw[] = [
`

const mockFooter = `]

function build(list: Raw[], depth: number, level: number): RegionNode[] {
  return list.map(([code, name, pinyin, children]) => {
    const node: RegionNode = { code, name, pinyin }
    if (children && depth < level) node.children = build(children, depth + 1, level)
    return node
  })
}

function withCode(code: string): RegionNode | undefined {
  const stack = build(raw, 1, 3)
  const walk = (list: RegionNode[]): RegionNode | undefined => {
    for (const n of list) {
      if (n.code === code) return n
      const hit = n.children ? walk(n.children) : undefined
      if (hit) return hit
    }
    return undefined
  }
  return walk(stack)
}

defineMock([
  {
    method: 'GET',
    url: '/regions/tree',
    handler: ({ query }) => build(raw, 1, Math.min(3, Math.max(1, Number(query.level) || 3))),
  },
  {
    method: 'GET',
    url: '/regions',
    handler: ({ query }) => {
      const parent = String(query.parent ?? '')
      if (!parent) return build(raw, 1, 1)
      return (withCode(parent)?.children ?? []).map((c) => ({ code: c.code, name: c.name, pinyin: c.pinyin }))
    },
  },
])
`
