package dict

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

func setup(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return NewService(db), mock
}

var bg = context.Background()

func statusOf(err error) int {
	var re *response.Error
	if errors.As(err, &re) {
		return re.Status
	}
	return 0
}

func count(n int) *sqlmock.Rows { return sqlmock.NewRows([]string{"c"}).AddRow(n) }

func typeRow(id int64, code, status string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "code", "sort", "status"}).AddRow(id, "N", code, 1, status)
}

func expectTypeCodeCount(mock sqlmock.Sqlmock, code string, exclude int64, n int) {
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_dict_type` WHERE code = \\? AND id <> \\?").
		WithArgs(code, exclude).WillReturnRows(count(n))
}

func expectGetType(mock sqlmock.Sqlmock, id int64, rows *sqlmock.Rows) {
	mock.ExpectQuery("FROM `sys_dict_type` WHERE id = \\?").WithArgs(id, 1).WillReturnRows(rows)
}

func expectTypeCodes(mock sqlmock.Sqlmock, codes ...string) {
	rows := sqlmock.NewRows([]string{"code"})
	for _, c := range codes {
		rows.AddRow(c)
	}
	mock.ExpectQuery("SELECT `code` FROM `sys_dict_type` WHERE id IN").WillReturnRows(rows)
}

func TestCreateTypeCodeUnique(t *testing.T) {
	t.Run("编码重复 409 且不写入", func(t *testing.T) {
		s, mock := setup(t)
		expectTypeCodeCount(mock, "sys_status", 0, 1)
		_, err := s.CreateType(bg, TypeInput{Name: "状态", Code: "sys_status"})
		if statusOf(err) != http.StatusConflict {
			t.Fatalf("期望 409，得到 %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("编码可用时创建，状态默认启用", func(t *testing.T) {
		s, mock := setup(t)
		expectTypeCodeCount(mock, "sys_status", 0, 0)
		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO `sys_dict_type`").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		got, err := s.CreateType(bg, TypeInput{Name: "状态", Code: "sys_status"})
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != "1" || got.ID == 0 {
			t.Errorf("status=%q id=%d", got.Status, got.ID)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}

func TestUpdateTypeCodeUniqueExcludesSelf(t *testing.T) {
	t.Run("改成别人的编码 409", func(t *testing.T) {
		s, mock := setup(t)
		expectGetType(mock, 5, typeRow(5, "a", "1"))
		expectTypeCodeCount(mock, "b", 5, 1) // 排除自己 (id <> 5) 后仍有命中
		_, err := s.UpdateType(bg, 5, TypeInput{Name: "x", Code: "b"})
		if statusOf(err) != http.StatusConflict {
			t.Fatalf("期望 409，得到 %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("不存在 404", func(t *testing.T) {
		s, mock := setup(t)
		expectGetType(mock, 5, sqlmock.NewRows([]string{"id"}))
		if _, err := s.UpdateType(bg, 5, TypeInput{Name: "x", Code: "b"}); statusOf(err) != http.StatusNotFound {
			t.Fatalf("期望 404，得到 %v", err)
		}
	})
}

func TestDeleteTypesCascadesDataInTransaction(t *testing.T) {
	s, mock := setup(t)
	expectTypeCodes(mock, "custom_a", "custom_b")
	mock.ExpectBegin()
	// 先删子（字典数据），再删父（字典类型），同一事务
	mock.ExpectExec("DELETE FROM `sys_dict_data` WHERE type_id IN \\(\\?,\\?\\)").WithArgs(int64(1), int64(2)).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec("DELETE FROM `sys_dict_type` WHERE id IN \\(\\?,\\?\\)").WithArgs(int64(1), int64(2)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()
	if err := s.DeleteTypes(bg, []idgen.ID{1, 2}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestDeleteTypesRollsBackWhenTypeDeleteFails(t *testing.T) {
	s, mock := setup(t)
	expectTypeCodes(mock, "custom_a")
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `sys_dict_data`").WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec("DELETE FROM `sys_dict_type`").WillReturnError(errors.New("boom"))
	mock.ExpectRollback()
	if err := s.DeleteTypes(bg, []idgen.ID{1}); err == nil {
		t.Fatal("应返回错误")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestCreateDataRules(t *testing.T) {
	in := DataInput{TypeID: 9, Label: "男", Value: "1"}
	t.Run("所属类型不存在 400", func(t *testing.T) {
		s, mock := setup(t)
		expectGetType(mock, 9, sqlmock.NewRows([]string{"id"}))
		if _, err := s.CreateData(bg, in); statusOf(err) != http.StatusBadRequest {
			t.Fatalf("期望 400，得到 %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("同类型下值重复 409", func(t *testing.T) {
		s, mock := setup(t)
		expectGetType(mock, 9, typeRow(9, "gender", "1"))
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_dict_data` WHERE type_id = \\? AND dict_value = \\? AND id <> \\?").
			WithArgs(int64(9), "1", int64(0)).WillReturnRows(count(1))
		if _, err := s.CreateData(bg, in); statusOf(err) != http.StatusConflict {
			t.Fatalf("期望 409，得到 %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("成功：tag 默认 default，状态默认启用", func(t *testing.T) {
		s, mock := setup(t)
		expectGetType(mock, 9, typeRow(9, "gender", "1"))
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_dict_data`").WillReturnRows(count(0))
		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO `sys_dict_data`").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		d, err := s.CreateData(bg, in)
		if err != nil {
			t.Fatal(err)
		}
		if d.Tag != "default" || d.Status != "1" {
			t.Errorf("tag=%q status=%q", d.Tag, d.Status)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}

func TestUpdateDataValueUniqueExcludesSelf(t *testing.T) {
	s, mock := setup(t)
	mock.ExpectQuery("FROM `sys_dict_data` WHERE id = \\?").WithArgs(20, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "type_id", "dict_value"}).AddRow(20, 9, "1"))
	expectGetType(mock, 9, typeRow(9, "gender", "1"))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_dict_data` WHERE type_id = \\? AND dict_value = \\? AND id <> \\?").
		WithArgs(int64(9), "1", int64(20)).WillReturnRows(count(0))
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `sys_dict_data` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery("FROM `sys_dict_data` WHERE id = \\?").WithArgs(20, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "type_id", "dict_value"}).AddRow(20, 9, "1"))
	if _, err := s.UpdateData(bg, 20, DataInput{TypeID: 9, Label: "男", Value: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestSortRewritesInSteps(t *testing.T) {
	s, mock := setup(t)
	mock.ExpectBegin()
	for i, id := range []int64{30, 10, 20} {
		mock.ExpectExec("UPDATE `sys_dict_type` SET `sort`=\\?").WithArgs((i+1)*10, sqlmock.AnyArg(), id).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectCommit()
	if err := s.SortTypes(bg, []idgen.ID{30, 10, 20}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestItems(t *testing.T) {
	dataCols := []string{"id", "dict_label", "label_en", "dict_value", "tag"}
	t.Run("字典不存在或停用返回空数组", func(t *testing.T) {
		s, mock := setup(t)
		mock.ExpectQuery("FROM `sys_dict_type` WHERE code = \\? AND status = \\?").WithArgs("gender", "1", 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))
		got, err := s.Items(bg, "gender", false)
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("got=%v err=%v", got, err)
		}
	})
	rows := func() *sqlmock.Rows {
		return sqlmock.NewRows(dataCols).AddRow(1, "男", "Male", "1", "info").AddRow(2, "女", "", "2", "")
	}
	for _, tc := range []struct {
		english bool
		want    []string
	}{{false, []string{"男", "女"}}, {true, []string{"Male", "女"}}} { // 英文缺 label_en 时回落中文
		s, mock := setup(t)
		mock.ExpectQuery("FROM `sys_dict_type` WHERE code = \\? AND status = \\?").WillReturnRows(typeRow(9, "gender", "1"))
		mock.ExpectQuery("FROM `sys_dict_data` WHERE type_id = \\? AND status = \\? ORDER BY sort, id").
			WithArgs(int64(9), "1").WillReturnRows(rows())
		got, err := s.Items(bg, "gender", tc.english)
		if err != nil || len(got) != 2 {
			t.Fatalf("got=%v err=%v", got, err)
		}
		for i, w := range tc.want {
			if got[i].Label != w {
				t.Errorf("english=%v 第 %d 项标签=%q 期望 %q", tc.english, i, got[i].Label, w)
			}
		}
		if got[0].Value != "1" || got[0].Tag != "info" {
			t.Errorf("值/标签色不对: %+v", got[0])
		}
	}
}

func TestListFilterEscapesKeywordAndSplitsStatus(t *testing.T) {
	s, mock := setup(t)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_dict_type` WHERE .*LOWER\\(name\\) LIKE \\? OR LOWER\\(code\\) LIKE \\?.* AND status IN \\(\\?,\\?\\)").
		WithArgs(`%a\%\_b%`, `%a\%\_b%`, "1", "0").WillReturnRows(count(0))
	mock.ExpectQuery("FROM `sys_dict_type` WHERE .*ORDER BY sort, id LIMIT \\?").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	list, total, err := s.ListTypes(bg, ListQuery{Keyword: " A%_B ", Status: "1,0"}, 1, 10)
	if err != nil || total != 0 || list == nil {
		t.Fatalf("list=%v total=%d err=%v", list, total, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestListDataBadTypeID(t *testing.T) {
	s, mock := setup(t)
	if _, _, err := s.ListData(bg, ListQuery{TypeID: "abc"}, 1, 10); statusOf(err) != http.StatusBadRequest {
		t.Fatalf("typeId 非法应 400，得到 %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func router(s *Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := &Handler{svc: s}
	r := gin.New()
	r.POST("/dict/types", h.CreateType)
	r.DELETE("/dict/types", h.DeleteTypes)
	r.DELETE("/dict/types/:id", h.DeleteType)
	r.GET("/dicts/:code/items", h.Items)
	return r
}

func TestHandlerValidation(t *testing.T) {
	s, mock := setup(t) // 校验失败不得触达数据库
	r := router(s)
	post := func(body string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/dict/types", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w.Code
	}
	for name, body := range map[string]string{
		"缺编码":     `{"name":"a"}`,
		"状态非法":    `{"name":"a","code":"c","status":"2"}`,
		"编码过长":    `{"name":"a","code":"` + string(bytes.Repeat([]byte("x"), 65)) + `"}`,
		"JSON 错误": `{`,
	} {
		if got := post(body); got != http.StatusBadRequest {
			t.Errorf("%s: 期望 400，得到 %d", name, got)
		}
	}
	for _, path := range []string{"/dict/types?ids=1,x", "/dict/types/abc"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, path, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("DELETE %s 期望 400，得到 %d", path, w.Code)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestHandlerItemsLanguageNegotiation(t *testing.T) {
	for header, wantLabel := range map[string]string{"en-US,en;q=0.9": "Male", "zh-CN": "男", "": "男"} {
		s, mock := setup(t)
		mock.ExpectQuery("FROM `sys_dict_type`").WillReturnRows(typeRow(9, "gender", "1"))
		mock.ExpectQuery("FROM `sys_dict_data`").WillReturnRows(
			sqlmock.NewRows([]string{"id", "dict_label", "label_en", "dict_value"}).AddRow(1, "男", "Male", "1"))
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/dicts/gender/items", nil)
		if header != "" {
			req.Header.Set("Accept-Language", header)
		}
		router(s).ServeHTTP(w, req)
		var body struct {
			Data []Item `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Data) != 1 || body.Data[0].Label != wantLabel {
			t.Errorf("Accept-Language=%q 期望标签 %q，响应 %s", header, wantLabel, w.Body.String())
		}
	}
}

// ---- 内置字典保护 ----

func TestBuiltinTypeProtection(t *testing.T) {
	t.Run("删除内置类型 409 且不写库", func(t *testing.T) {
		s, mock := setup(t)
		expectTypeCodes(mock, "custom_a", "sys_status")
		err := s.DeleteTypes(bg, []idgen.ID{1, 2})
		if statusOf(err) != http.StatusConflict {
			t.Fatalf("期望 409，得到 %v", err)
		}
		var re *response.Error
		if !errors.As(err, &re) || re.Key != "dict.builtin" {
			t.Errorf("期望 key dict.builtin，得到 %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("删除非内置类型成功", func(t *testing.T) {
		s, mock := setup(t)
		expectTypeCodes(mock, "custom_a")
		mock.ExpectBegin()
		mock.ExpectExec("DELETE FROM `sys_dict_data`").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("DELETE FROM `sys_dict_type`").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		if err := s.DeleteTypes(bg, []idgen.ID{1}); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("修改内置类型编码 409", func(t *testing.T) {
		s, mock := setup(t)
		expectGetType(mock, 5, typeRow(5, "sys_status", "1"))
		_, err := s.UpdateType(bg, 5, TypeInput{Name: "x", Code: "renamed"})
		if statusOf(err) != http.StatusConflict {
			t.Fatalf("期望 409，得到 %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("内置类型改名称/状态允许（编码不变）", func(t *testing.T) {
		s, mock := setup(t)
		expectGetType(mock, 5, typeRow(5, "sys_status", "1"))
		expectTypeCodeCount(mock, "sys_status", 5, 0)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE `sys_dict_type` SET").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		expectGetType(mock, 5, typeRow(5, "sys_status", "0"))
		got, err := s.UpdateType(bg, 5, TypeInput{Name: "新名称", Code: "sys_status", Status: "0"})
		if err != nil {
			t.Fatal(err)
		}
		if !got.Builtin {
			t.Error("内置类型应带 builtin=true")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}

func TestBuiltinDataProtection(t *testing.T) {
	dataRows := func(typeID int64, value string) *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "type_id", "dict_value"}).AddRow(30, typeID, value)
	}
	t.Run("删除内置数据项 409", func(t *testing.T) {
		s, mock := setup(t)
		mock.ExpectQuery("SELECT `id`,`type_id`,`dict_value` FROM `sys_dict_data` WHERE id IN").WillReturnRows(dataRows(9, "1"))
		mock.ExpectQuery("SELECT `id`,`code` FROM `sys_dict_type` WHERE id IN").
			WillReturnRows(sqlmock.NewRows([]string{"id", "code"}).AddRow(9, "sys_gender"))
		if err := s.DeleteData(bg, []idgen.ID{30}); statusOf(err) != http.StatusConflict {
			t.Fatalf("期望 409，得到 %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("内置类型下用户自加的数据项可删", func(t *testing.T) {
		s, mock := setup(t)
		mock.ExpectQuery("SELECT `id`,`type_id`,`dict_value` FROM `sys_dict_data` WHERE id IN").WillReturnRows(dataRows(9, "custom"))
		mock.ExpectQuery("SELECT `id`,`code` FROM `sys_dict_type` WHERE id IN").
			WillReturnRows(sqlmock.NewRows([]string{"id", "code"}).AddRow(9, "sys_gender"))
		mock.ExpectBegin()
		mock.ExpectExec("DELETE FROM `sys_dict_data` WHERE id IN").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		if err := s.DeleteData(bg, []idgen.ID{30}); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	t.Run("修改内置数据项的 value 409", func(t *testing.T) {
		s, mock := setup(t)
		mock.ExpectQuery("FROM `sys_dict_data` WHERE id = \\?").WithArgs(30, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "type_id", "dict_value"}).AddRow(30, 9, "1"))
		expectGetType(mock, 9, typeRow(9, "sys_gender", "1"))
		_, err := s.UpdateData(bg, 30, DataInput{TypeID: 9, Label: "男", Value: "changed"})
		if statusOf(err) != http.StatusConflict {
			t.Fatalf("期望 409，得到 %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}
