package formbuilder

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	"nexadmin/pkg/idgen"
)

// 批量删除的两条接口都必须是"整批或什么都不做"：语句形状由 sqlmock 钉住（作用域条件、连带清理），
// 语义（护栏违反时一条都不掉）由 memStore 的 service 用例钉住。

// ---- SQL 形状 ----

// subsOf 数出内存库里某表单名下的提交条数。
func subsOf(st *memStore, form idgen.ID) int {
	n := 0
	for _, s := range st.subs {
		if s.FormID == form {
			n++
		}
	}
	return n
}

// 批量删表单：三条 DELETE 都带 tenant_id，且前两条按 form_id IN 连带清理提交与版本。
func TestDeleteFormsSQLIsTenantScopedAndCascading(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_formbuilder_submission` WHERE tenant_id = \\? AND form_id IN \\(\\?,\\?\\)").
		WithArgs(int64(7), int64(42), int64(43)).WillReturnResult(sqlmock.NewResult(0, 5))
	mock.ExpectExec("DELETE FROM `plg_formbuilder_version` WHERE tenant_id = \\? AND form_id IN \\(\\?,\\?\\)").
		WithArgs(int64(7), int64(42), int64(43)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("DELETE FROM `plg_formbuilder_form` WHERE tenant_id = \\? AND id IN \\(\\?,\\?\\)").
		WithArgs(int64(7), int64(42), int64(43)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	n, err := NewStore(db).DeleteForms(tctx, []idgen.ID{42, 43})
	if err != nil {
		t.Fatalf("DeleteForms: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	met(t, mock)
}

// 护栏：点名的表单少了一条（不存在或不属于当前租户）就回滚，不提交部分结果。
func TestDeleteFormsRollsBackWhenAnyIDMissing(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_formbuilder_submission`").WillReturnResult(sqlmock.NewResult(0, 5))
	mock.ExpectExec("DELETE FROM `plg_formbuilder_version`").WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("DELETE FROM `plg_formbuilder_form` WHERE tenant_id = \\? AND id IN \\(\\?,\\?\\)").
		WithArgs(int64(7), int64(42), int64(43)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	n, err := NewStore(db).DeleteForms(tctx, []idgen.ID{42, 43})
	if !errors.Is(err, errNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
	if status(err) != http.StatusNotFound {
		t.Errorf("状态码 = %d, want 404", status(err))
	}
	if n != 0 {
		t.Errorf("整批失败时 deleted 必须是 0，实际 %d", n)
	}
	met(t, mock)
}

// 批量删提交：删除范围始终是 (tenant_id, form_id, id IN)，越界的 id 匹配不到行。
func TestDeleteSubmissionsSQLStaysInsideForm(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_formbuilder_submission` WHERE tenant_id = \\? AND form_id = \\? AND id IN \\(\\?,\\?\\)").
		WithArgs(int64(7), int64(42), int64(9), int64(10)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("UPDATE `plg_formbuilder_form` SET `submission_count`=CASE WHEN submission_count >= \\? THEN submission_count - \\? ELSE 0 END WHERE tenant_id = \\? AND id = \\?").
		WithArgs(int64(2), int64(2), int64(7), int64(42)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	n, err := NewStore(db).DeleteSubmissions(tctx, 42, []idgen.ID{9, 10})
	if err != nil {
		t.Fatalf("DeleteSubmissions: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	met(t, mock)
}

// 混进别的表单的提交 id：实删条数少于点名条数，事务回滚并报 404，名额也不动。
func TestDeleteSubmissionsRollsBackOnCrossFormID(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_formbuilder_submission` WHERE tenant_id = \\? AND form_id = \\? AND id IN \\(\\?,\\?\\)").
		WithArgs(int64(7), int64(42), int64(9), int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	n, err := NewStore(db).DeleteSubmissions(tctx, 42, []idgen.ID{9, 10})
	if !errors.Is(err, errNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
	if n != 0 {
		t.Errorf("整批失败时 deleted 必须是 0，实际 %d", n)
	}
	met(t, mock)
}

// ---- 语义（memStore）----

// 整批失败时一条都不能掉：表单、它的版本与提交都还在。
func TestDeleteFormsBatchIsAllOrNothing(t *testing.T) {
	s, st, _ := newSvc()
	a := publish(t, s, ctxT1, simpleDef("a", fa))
	b := publish(t, s, ctxT1, simpleDef("b", fa))
	keep := publish(t, s, ctxT1, simpleDef("keep", fa))
	for _, id := range []idgen.ID{a, b, keep} {
		if _, err := s.Submit(ctxT1, user(2), id, SubmitInput{Data: map[string]any{"a": "x"}}); err != nil {
			t.Fatal(err)
		}
	}

	// 第三个别人的表单不参与删除，先把它的行数记下来
	if _, err := s.DeleteForms(ctxT1, []idgen.ID{a, b, 999}); status(err) != http.StatusNotFound {
		t.Fatalf("含不存在的 id 应整批 404: %v", err)
	}
	for _, id := range []idgen.ID{a, b} {
		if _, err := s.GetForm(ctxT1, id); err != nil {
			t.Fatalf("整批失败后表单 %s 不该被动过: %v", id, err)
		}
		if n := subsOf(st, id); n != 1 {
			t.Errorf("整批失败后表单 %s 的提交不该被动过: %d 条", id, n)
		}
	}

	deleted, err := s.DeleteForms(ctxT1, []idgen.ID{a, b})
	if err != nil {
		t.Fatalf("DeleteForms: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}
	for _, id := range []idgen.ID{a, b} {
		if _, err := s.GetForm(ctxT1, id); status(err) != http.StatusNotFound {
			t.Errorf("表单 %s 应已删除: %v", id, err)
		}
		if n := subsOf(st, id); n != 0 {
			t.Errorf("表单 %s 的提交应连带清理: %d 条", id, n)
		}
		if vs, err := s.ListVersions(ctxT1, id); err == nil && len(vs) != 0 {
			t.Errorf("表单 %s 的版本应连带清理: %v", id, vs)
		}
	}
	// 未被点名的表单与数据一条都不能少
	if _, err := s.GetForm(ctxT1, keep); err != nil {
		t.Fatalf("keep 表单不应被删除: %v", err)
	}
	if n := subsOf(st, keep); n != 1 {
		t.Errorf("keep 表单的提交不该受影响: %d 条", n)
	}
}

// 去重：同一条被重复勾选不是错误，但也不能被数成两条（否则护栏会误判）。
func TestDeleteFormsDeduplicatesIDs(t *testing.T) {
	s, _, _ := newSvc()
	a := publish(t, s, ctxT1, simpleDef("a", fa))
	deleted, err := s.DeleteForms(ctxT1, []idgen.ID{a, a})
	if err != nil {
		t.Fatalf("重复 id 不应报错: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
}

// 提交是行级数据：整批失败时别人表单的行、本表单未点名的行都必须原样保留，名额不能少。
func TestDeleteSubmissionsBatchIsAllOrNothing(t *testing.T) {
	s, st, _ := newSvc()
	a := publish(t, s, ctxT1, simpleDef("a", fa))
	b := publish(t, s, ctxT1, simpleDef("b", fa))
	ids := func(form idgen.ID, n int) []idgen.ID {
		out := make([]idgen.ID, 0, n)
		for i := range n {
			r, err := s.Submit(ctxT1, user(int64(10+i)), form, SubmitInput{Data: map[string]any{"a": "x"}})
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, r.ID)
		}
		return out
	}
	sa, sb := ids(a, 2), ids(b, 1)
	if f, _ := st.form(1, a); f.SubmissionCount != 2 {
		t.Fatalf("前置数据不对: count=%d", f.SubmissionCount)
	}

	// a 的两条里混进 b 的一条：整批 404，三条提交一条都不能少
	if _, err := s.DeleteSubmissions(ctxT1, a, append(append([]idgen.ID{}, sa...), sb...)); status(err) != http.StatusNotFound {
		t.Fatalf("跨表单 id 应整批失败: %v", err)
	}
	for _, id := range append(append([]idgen.ID{}, sa...), sb...) {
		if _, ok := st.subs[int64(id)]; !ok {
			t.Fatalf("整批失败后提交 %s 不该被删", id)
		}
	}
	if f, _ := st.form(1, a); f.SubmissionCount != 2 {
		t.Errorf("整批失败后名额不该变: count=%d", f.SubmissionCount)
	}

	deleted, err := s.DeleteSubmissions(ctxT1, a, sa)
	if err != nil {
		t.Fatalf("DeleteSubmissions: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}
	for _, id := range sa {
		if _, ok := st.subs[int64(id)]; ok {
			t.Errorf("提交 %s 应已删除", id)
		}
	}
	if _, ok := st.subs[int64(sb[0])]; !ok {
		t.Error("别的表单的提交不该被删")
	}
	if f, _ := st.form(1, a); f.SubmissionCount != 0 {
		t.Errorf("名额应归还 2 条: count=%d", f.SubmissionCount)
	}
}

// ---- 入参校验（在任何 SQL 之前）----

// ?ids= 缺失、为空或任一项非法一律 400：退化成"删全部"或"删掉能删的"是最难发现的误删。
// svc 传 nil 就是这条用例的证据：校验一旦漏过去就会在触到数据库时 panic，而不是稳定返回 400。
func TestBatchDeleteHandlersRejectMissingOrBadIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &handler{svc: nil}
	cases := []struct {
		method, path string
	}{
		{http.MethodDelete, "/plugins/formbuilder/forms"},
		{http.MethodDelete, "/plugins/formbuilder/forms/42/submissions"},
	}
	for _, q := range []string{"", "?ids=", "?ids=abc", "?ids=11,abc", "?ids=0"} {
		for _, tc := range cases {
			e := gin.New()
			switch tc.method + " " + tc.path {
			case "DELETE /plugins/formbuilder/forms":
				e.DELETE("/plugins/formbuilder/forms", h.deleteForms)
			default:
				e.DELETE("/plugins/formbuilder/forms/:id/submissions", h.deleteSubmissions)
			}
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path+q, nil))
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s%s: code=%d want 400 body=%s", tc.path, q, w.Code, w.Body.String())
			}
		}
	}
}

// 非法的表单 id（路径参数）同样是 400，且在解析 ids 之前就被拒绝。
func TestDeleteSubmissionsRejectsBadFormID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.DELETE("/plugins/formbuilder/forms/:id/submissions", (&handler{svc: nil}).deleteSubmissions)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/plugins/formbuilder/forms/abc/submissions?ids=1", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("code=%d want 400 body=%s", w.Code, w.Body.String())
	}
}
