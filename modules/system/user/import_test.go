package user

import (
	"strings"
	"testing"

	"nexadmin/pkg/idgen"
)

func testLookups() importLookups {
	return importLookups{
		depts: map[string]idgen.ID{"研发部": 11},
		roles: map[string]idgen.ID{"运营": 21, "ops": 21, "审计": 22},
		posts: map[string]idgen.ID{"工程师": 31},
	}
}

func TestParseImportCSV(t *testing.T) {
	src := "\xef\xbb\xbf账号,姓名,手机,邮箱,性别,部门,角色,岗位,备注\r\n" +
		"alice,Alice,13800000001,a@x.com,女,研发部,运营;审计,工程师,\r\n" +
		",,,,,,,,\r\n" + // 全空行被跳过
		"bob,Bob\r\n" // 缺列视为空
	rows, err := parseImportCSV(strings.NewReader(src))
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if rows[0].Row != 2 || rows[0].Username != "alice" || rows[0].Roles != "运营;审计" || rows[1].Row != 4 || rows[1].Nickname != "Bob" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	if _, err := parseImportCSV(strings.NewReader("姓名,手机\r\nx,y\r\n")); err == nil {
		t.Fatal("missing username column must be rejected")
	}
	if _, err := parseImportCSV(strings.NewReader("")); err == nil {
		t.Fatal("empty file must be rejected")
	}
}

func TestPlanImportValidation(t *testing.T) {
	rows := []importRow{
		{Row: 2, Username: "alice", Nickname: "Alice", Gender: "女", Dept: "研发部", Roles: "运营;审计", Posts: "工程师"},
		{Row: 3, Username: "", Nickname: "x"},                        // 必填
		{Row: 4, Username: "ALICE"},                                  // 文件内重复（忽略大小写）
		{Row: 5, Username: "carol", Dept: "不存在的部门"},                  // 部门映射失败
		{Row: 6, Username: "dave", Roles: "运营;超级角色"},                 // 角色映射失败
		{Row: 7, Username: "erin", Posts: "厨师"},                      // 岗位映射失败
		{Row: 8, Username: "x"},                                      // 账号格式（至少 2 位）
		{Row: 9, Username: "frank", Phone: "abc"},                    // 手机号格式
		{Row: 10, Username: "gina", Gender: "?"},                     // 性别
		{Row: 11, Username: "henry", Roles: "ops", Email: "h@x.com"}, // 角色编码也可映射
	}
	got := planImport(rows, testLookups())
	wantFail := map[int]string{3: "账号不能为空", 4: "第 2 行重复", 5: "部门", 6: "角色", 7: "岗位", 8: "账号", 9: "手机", 10: "性别"}
	for _, p := range got {
		if sub, bad := wantFail[p.row]; bad {
			if p.reason == "" || !strings.Contains(p.reason, sub) {
				t.Errorf("row %d: reason %q should contain %q", p.row, p.reason, sub)
			}
		} else if p.reason != "" {
			t.Errorf("row %d should pass, got %q", p.row, p.reason)
		}
	}
	first := got[0].in
	if first.DeptID != "11" || len(first.RoleIDs) != 2 || first.PostIDs[0] != "31" || first.Gender != "2" {
		t.Fatalf("mapping wrong: %+v", first)
	}
	if got[9].in.RoleIDs[0] != "21" {
		t.Fatalf("role code mapping wrong: %+v", got[9].in)
	}
}
