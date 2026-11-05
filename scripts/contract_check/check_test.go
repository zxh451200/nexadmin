package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const tsAPI = "import { api } from '@/framework/request/http'\n" +
	"const BASE = '/things'\n" +
	"export const a = (id: string) => api.get<Thing>(`${BASE}/${id}`)\n" +
	"export const b = (id: string) => api.put<null>(`/things/${id}/state`, {})\n" +
	"export const c = () => api.post<{ a: string; b?: number }>(\n  '/things',\n  {},\n)\n" +
	"export const d = (id: string) => api.del('/things/' + id)\n" +
	"export const e = () => api.get('/typo/path')\n" +
	"export const f = () => api.get('/pub/hello?x=1')\n" +
	"// api.get('/in/comment')\n" +
	"const doc = `api.get('/in/string')`\n" +
	"export const g = (u: string) => api.get(u)\n"

const tsMock = "import { createCrudMock, defineMock } from '@/framework/request/mock'\n" +
	"defineMock([\n  { method: 'GET', url: '/mockonly', handler: () => ({ url: '/x' }) },\n" +
	"  { method: 'PUT', url: '/things/:id/state', handler: () => null },\n])\n" +
	"createCrudMock<Row>({ base: '/crud', seed: [], sortable: true })\n"

func tsFixture(t *testing.T) string {
	return writeTree(t, map[string]string{
		"modules/demo/module.go":                   goFixture,
		"web/src/framework/modules/demo/api.ts":    tsAPI,
		"web/src/framework/modules/demo/d.mock.ts": tsMock,
	})
}

func TestFrontendExtraction(t *testing.T) {
	files, err := loadFront(tsFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	calls, unres := frontCalls(files)
	got := map[string]bool{}
	for _, c := range calls {
		got[c.Method+" "+c.Path] = true
	}
	for _, w := range []string{
		"GET /api/v1/things/:param", "PUT /api/v1/things/:param/state", "POST /api/v1/things",
		"DELETE /api/v1/things/:param", "GET /api/v1/typo/path", "GET /api/v1/pub/hello",
	} {
		if !got[w] {
			t.Errorf("前端调用缺少 %q，实际 %v", w, got)
		}
	}
	if got["GET /api/v1/in/comment"] || got["GET /api/v1/in/string"] {
		t.Error("注释和字符串里的示例不应被当成调用")
	}
	if len(unres) != 1 || !strings.Contains(unres[0].Text, "api.get(u)") {
		t.Errorf("动态 URL 应进入无法解析: %+v", unres)
	}
	mocks, _ := frontMocks(files)
	mg := map[string]bool{}
	for _, m := range mocks {
		mg[m.Method+" "+m.Path] = true
	}
	// defineMock 2 条 + createCrudMock 展开 7 条
	for _, w := range []string{"GET /api/v1/mockonly", "GET /api/v1/crud", "PUT /api/v1/crud/sort", "GET /api/v1/crud/:id",
		"POST /api/v1/crud", "PUT /api/v1/crud/:id", "DELETE /api/v1/crud/:id", "DELETE /api/v1/crud"} {
		if !mg[w] {
			t.Errorf("mock 缺少 %q，实际 %v", w, mg)
		}
	}
	if len(mocks) != 9 {
		t.Errorf("mock 条数应为 9，实际 %d", len(mocks))
	}
}

// TestCrudMockSortRuleIsOptIn 钉住反向：没写 sortable: true 的资源不能展开出 PUT {base}/sort。
// 只有正向（tsMock 里的 sortable: true）时，把旗标判断退回「无条件给每个资源注册 /sort」
// 这套测试仍然全绿，多出来的假契约要等有人对真树跑一遍 contract_check 才照得出来。
func TestCrudMockSortRuleIsOptIn(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/framework/modules/demo/on.mock.ts":  "import { createCrudMock } from '@/framework/request/mock'\ncreateCrudMock<Row>({ base: '/on', seed: [], sortable: true })\n",
		"web/src/framework/modules/demo/off.mock.ts": "import { createCrudMock } from '@/framework/request/mock'\ncreateCrudMock<Row>({ base: '/off', seed: [], sortable: false })\n",
	})
	files, err := loadFront(root)
	if err != nil {
		t.Fatal(err)
	}
	mocks, _ := frontMocks(files)
	got := map[string]bool{}
	for _, m := range mocks {
		got[m.Method+" "+m.Path] = true
	}
	if !got["PUT /api/v1/on/sort"] {
		t.Error("sortable: true 应展开出 PUT /on/sort")
	}
	if got["PUT /api/v1/off/sort"] {
		t.Error("没声明 sortable 的资源不该有 PUT /off/sort")
	}
	// /off 的其余路由仍要展开，否则这条只是在测「根本没展开」
	for _, w := range []string{"GET /api/v1/off", "GET /api/v1/off/:id", "POST /api/v1/off", "PUT /api/v1/off/:id", "DELETE /api/v1/off/:id", "DELETE /api/v1/off"} {
		if !got[w] {
			t.Errorf("createCrudMock 的其余展开缺 %q", w)
		}
	}
}

func TestABCClassification(t *testing.T) {
	rep, err := Check(tsFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.A) != 4 {
		t.Fatalf("A 类应有 typo、POST /things、DELETE /things/:id 四项（含 GET /pub/hello 方法不符）: %+v", rep.A)
	}
	byPath := map[string]Issue{}
	for _, i := range rep.A {
		byPath[i.Method+" "+i.Path] = i
	}
	if i := byPath["GET /api/v1/typo/path"]; i.Kind != kindMissing || len(i.Suggest) == 0 {
		t.Errorf("typo 应为 missing 且带建议: %+v", i)
	}
	if i := byPath["DELETE /api/v1/things/:param"]; i.Kind != kindWeak && i.Kind != kindMissing && i.Kind != kindMethod {
		t.Errorf("DELETE 分类异常: %+v", i)
	}
	if i := byPath["GET /api/v1/pub/hello"]; i.Kind != kindMethod {
		t.Errorf("方法不符应分类为 method: %+v", i)
	}
	if len(rep.B) != 8 { // /mockonly + 7 个 crud 路由全部后端没有
		t.Errorf("B 类应有 8 项，实际 %d: %+v", len(rep.B), rep.B)
	}
	cs := map[string]bool{}
	for _, r := range rep.C {
		cs[r.Method+" "+r.Path] = true
	}
	if !cs["GET /api/v1/admin/ops"] || cs["GET /api/v1/things/:id"] {
		t.Errorf("C 类应含未调用的 admin/ops 且不含已调用的 things/:id: %v", cs)
	}
}

func TestExitCodesAndJSON(t *testing.T) {
	root := tsFixture(t)
	var out, errb bytes.Buffer
	if code := run([]string{"--root", root}, &out, &errb); code != 1 {
		t.Errorf("A 非空应返回 1，实际 %d", code)
	}
	out.Reset()
	if code := run([]string{"--root", root, "--warn-only", "--json"}, &out, &errb); code != 0 {
		t.Errorf("--warn-only 应返回 0，实际 %d", code)
	}
	var rep Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil || rep.Summary.A != 4 {
		t.Errorf("--json 输出应可解析且 A=4: %v %+v", err, rep.Summary)
	}
	if code := run([]string{"--bogus"}, &out, &errb); code != 2 {
		t.Errorf("未知参数应返回 2，实际 %d", code)
	}
}
