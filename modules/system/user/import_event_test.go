package user

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/event"
)

// 导入走 s.create，每成功创建一个用户就发布 user.created（与单个创建同一份载荷，不含敏感字段）；失败行不发。
// 说明：事件队列满时 Publish 会丢弃并限频告警（已知行为，见 import.go 注释）；此处队列远未满。
func TestImportPublishesUserCreatedPerSuccessRow(t *testing.T) {
	useSource(t, limitSource{}) // 租户无额度限制：不加锁、不统计
	s, mock, ctx := newScopeSvc(t)

	var mu sync.Mutex
	var got []event.UserData
	event.Subscribe(event.SubscriberFunc(func(e event.Event) {
		if d, ok := e.Data.(event.UserData); ok && e.Name == event.UserCreated && strings.HasPrefix(d.Username, "impevt_") {
			mu.Lock()
			got = append(got, d)
			mu.Unlock()
		}
	}))

	for _, tbl := range []string{"sys_role", "sys_dept", "sys_post"} { // loadLookups
		mock.ExpectQuery(tbl).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "code"}))
	}
	for _, name := range []string{"impevt_alice", "impevt_bob"} { // 第 2 行是无效行，不产生任何 SQL
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user`").WillReturnRows(countRows(0)) // usernameTaken
		mock.ExpectQuery("sys_config").WillReturnRows(sqlmock.NewRows([]string{"config_value"}))
		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO `sys_user`").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("DELETE FROM `sys_user_role`").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("DELETE FROM `sys_user_post`").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()
		mock.ExpectQuery("SELECT \\* FROM `sys_user` WHERE id").WillReturnRows(
			sqlmock.NewRows([]string{"id", "username", "nickname", "status"}).AddRow(1, name, name, "1"))
		mock.ExpectQuery("sys_user_role AS ur").WillReturnRows(sqlmock.NewRows([]string{"user_id", "role_id", "name"}))
		mock.ExpectQuery("FROM `sys_user_post`").WillReturnRows(sqlmock.NewRows([]string{"user_id", "post_id"}))
	}

	rows := []importRow{
		{Row: 2, Username: "impevt_alice", Nickname: "Alice", Phone: "13800000001", Email: "a@x.com"},
		{Row: 3, Username: "", Nickname: "bad"}, // 校验失败：不入库、不发事件
		{Row: 4, Username: "impevt_bob", Nickname: "Bob"},
	}
	res, err := s.importUsers(ctx, rows, nil, actor{ID: 1, Super: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success != 2 || len(res.Failed) != 1 || res.Failed[0].Row != 3 {
		t.Fatalf("result = %+v", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("期望 2 个 user.created（成功行各一个），得到 %d：%+v", len(got), got)
	}
	names := map[string]bool{}
	for _, d := range got {
		names[d.Username] = true
		if d.ID == 0 || d.TenantID != 1 {
			t.Errorf("载荷应带用户 id 与租户：%+v", d)
		}
	}
	if !names["impevt_alice"] || !names["impevt_bob"] {
		t.Fatalf("事件对应的账号不对：%v", names)
	}
	// 载荷类型 event.UserData 只有 id/租户/账号/昵称/状态/部门，结构上不含手机号、邮箱、密码哈希。
	_ = context.Background()
}
