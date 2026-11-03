package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 批量删必须只动本租户点名的那几条：store.removeMany 与单条 remove 一样显式带 tenant_id，
// 去掉它就是「拿别人租户的身份源 ID 也能删」。这里还钉住整批只有一条 DELETE ——
// 单条删除不清理任何旁表（外部登录绑定 / 同步任务的 sourceId 都不回收），
// 批量版要是自作主张多打一条语句，期望对不上就会红。
func TestDeleteManyScopesToTenantAndIDs(t *testing.T) {
	db, mock := mockDB(t)
	svc := NewService(gormStore{db: db}, nil)

	const tid = idgen.ID(7)
	// Transaction 显式包裹：Begin / DELETE / Commit
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_identity_source` WHERE tenant_id = \\? AND id IN \\(\\?,\\?\\)").
		WithArgs(int64(tid), int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	n, err := svc.DeleteMany(plainCtx(tid), []idgen.ID{11, 12})
	if err != nil {
		t.Fatalf("DeleteMany: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 越权 / 已不存在的 id 落在租户条件外，删不到也不计入实删数：命中 1 条返回 1。
func TestDeleteManyCountsOnlyRealDeletes(t *testing.T) {
	db, mock := mockDB(t)
	svc := NewService(gormStore{db: db}, nil)

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_identity_source` WHERE tenant_id = \\? AND id IN \\(\\?,\\?,\\?\\)").
		WithArgs(int64(7), int64(11), int64(12), int64(13)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	n, err := svc.DeleteMany(plainCtx(7), []idgen.ID{11, 12, 13})
	if err != nil {
		t.Fatalf("DeleteMany: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted = %d, want 1", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 0 命中报 not found，不能静默返回 deleted=0 让界面以为删成功了。
func TestDeleteManyNotFoundWhenNothingMatches(t *testing.T) {
	db, mock := mockDB(t)
	svc := NewService(gormStore{db: db}, nil)

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_identity_source` WHERE tenant_id = \\? AND id IN \\(\\?\\)").
		WithArgs(int64(7), int64(99)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit() // 语句本身成功（0 行），not found 是服务层在事务外判的

	if _, err := svc.DeleteMany(plainCtx(7), []idgen.ID{99}); statusOf(err) != http.StatusNotFound {
		t.Fatalf("err = %v, want 404", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 整批一条事务：语句报错就整批回滚，不能留下「选中 3 条删掉 1 条」的中间状态。
func TestDeleteManyRollsBackWholeBatchOnSQLFailure(t *testing.T) {
	db, mock := mockDB(t)
	svc := NewService(gormStore{db: db}, nil)

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_identity_source`").WillReturnError(errors.New("boom"))
	mock.ExpectRollback() // 期望里没有 Commit：改成「提交半成品」这里就红

	if n, err := svc.DeleteMany(plainCtx(7), []idgen.ID{11, 12}); err == nil || n != 0 {
		t.Fatalf("deleted=%d err=%v, want 0 and an error", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 没有租户上下文就一条 SQL 都不许发：批量删除一旦退化成跨租户就是全然不可接受的。
// 三条互相独立的钉法——报的必须是「缺租户上下文」的 400（在触库之前拒绝，不是数据库报错）；
// store 一次都不该被调用；mock 里没排任何期望，真发了 SQL 就会拿到「不匹配的调用」错误。
func TestDeleteManyWithoutTenantDoesNoSQL(t *testing.T) {
	db, mock := mockDB(t)
	svc := NewService(gormStore{db: db}, nil)

	if _, err := svc.DeleteMany(context.Background(), []idgen.ID{11}); statusOf(err) != http.StatusBadRequest {
		t.Fatalf("err = %v, want 400（缺租户上下文应在触库前拒绝）", err)
	}

	st := &countingStore{store: newMemStore()}
	if _, err := NewService(st, nil).DeleteMany(context.Background(), []idgen.ID{11}); err == nil || st.calls != 0 {
		t.Fatalf("err = %v, removeMany calls = %d, want an error and 0 calls", err, st.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unexpected SQL: %v", err)
	}
}

// countingStore 只记录 removeMany 的调用次数，其余方法透传给内层 store。
type countingStore struct {
	store
	calls int
}

func (c *countingStore) removeMany(ctx context.Context, tid idgen.ID, ids []idgen.ID) (int64, error) {
	c.calls++
	return c.store.removeMany(ctx, tid, ids)
}

// 跨租户的 id 既删不到也不计数（memStore 与真实 store 一样按租户条件匹配行）：
// 租户 7 提交 [自己的, 租户 8 的]，只能删掉自己那条，别人那条必须原样留着。
func TestDeleteManyLeavesOtherTenantsRows(t *testing.T) {
	initTest(t)
	st := newMemStore()
	put := func(tid idgen.ID, name string) *Source {
		src := &Source{Type: TypeLDAP, Name: name, Enabled: true, Config: "{}"}
		src.TenantID = tid
		if err := st.create(plainCtx(tid), src); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		return src
	}
	mine := put(7, "mine")
	theirs := put(8, "theirs")

	n, err := NewService(st, nil).DeleteMany(plainCtx(7), []idgen.ID{mine.ID, theirs.ID})
	if err != nil {
		t.Fatalf("DeleteMany: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted = %d, want 1", n)
	}
	if _, err := st.get(plainCtx(7), 7, mine.ID); !errors.Is(err, errNotFound) {
		t.Errorf("own row should be gone, get err = %v", err)
	}
	if _, err := st.get(plainCtx(8), 8, theirs.ID); err != nil {
		t.Errorf("other tenant row must survive: %v", err)
	}
}

// ?ids= 缺失、整体为空或任一项非法一律 400，且在任何数据库访问之前拒绝：
// 「不带 ids 就删光本租户身份源」是最难发现的一种误删。svc 传 nil 是有意的——
// 一旦跳过校验就会走到服务层，当场炸给测试看。
func TestRemoveManyHandlerRejectsMissingOrBadIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, q := range []string{"", "?ids=", "?ids=abc", "?ids=11,abc", "?ids=0", "?ids=,"} {
		e := gin.New()
		e.DELETE("/plugins/identity/sources", (&handler{svc: nil}).removeMany)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/plugins/identity/sources"+q, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("query %q: code=%d want 400 body=%s", q, w.Code, w.Body.String())
		}
	}
}

func statusOf(err error) int {
	var re *response.Error
	if errors.As(err, &re) {
		return re.Status
	}
	return 0
}
