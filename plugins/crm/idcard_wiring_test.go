package crm

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/mask"
)

// 证件号写侧：归一化 + 合法性判定；脱敏串不当输入校验，交给 Create/Update 分别处理。
func TestNormalizeIDCardInput(t *testing.T) {
	ok := CustomerInput{Name: "张三", Type: TypePerson, IdType: IdTypeIDCard, IdNo: " 410181 1989 0415 451x "}
	if err := ok.normalize(); err != nil {
		t.Fatalf("合法号码被拒: %v", err)
	}
	if ok.IdNo != "41018119890415451X" {
		t.Errorf("IdNo 未归一化成 18 位大写 X：%q", ok.IdNo)
	}

	bad := CustomerInput{Name: "张三", Type: TypePerson, IdType: IdTypeIDCard, IdNo: "410181198904154511"}
	err := bad.normalize()
	if err == nil {
		t.Fatal("校验位不符的号码应被拒绝，而不是存进去")
	}
	var re *response.Error
	if !asResponseError(err, &re) || re.Key != "crm.id_no_invalid" {
		t.Errorf("错误 key = %v，want crm.id_no_invalid", err)
	}

	masked := CustomerInput{Name: "张三", Type: TypePerson, IdType: IdTypeIDCard, IdNo: "410181********451X"}
	if err := masked.normalize(); err != nil {
		t.Errorf("脱敏串不该在这里报错（应由沿用原值/拒绝两条路处理）：%v", err)
	}

	// 非居民身份证（护照等）没有格式规则可套，保持原样，不被身份证规则误伤。
	passport := CustomerInput{Name: "张三", Type: TypePerson, IdType: IdTypePassport, IdNo: "E12345678"}
	if err := passport.normalize(); err != nil {
		t.Errorf("护照号被身份证规则拒绝：%v", err)
	}
	if passport.IdNo != "E12345678" {
		t.Errorf("护照号被改写：%q", passport.IdNo)
	}
}

// 新建时没有"原值"可沿用，脱敏串必须拒绝。
func TestCreateCustomerRejectsMaskedID(t *testing.T) {
	db, _ := mockDB(t)
	svc := NewService(db, nopLogger{})
	_, err := svc.CreateCustomer(tenantCtx, CustomerInput{
		Name: "张三", Type: TypePerson, IdType: IdTypeIDCard, IdNo: "410181********451X",
	})
	var re *response.Error
	if !asResponseError(err, &re) || re.Key != "crm.id_no_masked" {
		t.Fatalf("应拒绝脱敏串，实际 %v", err)
	}
}

// 读侧：没有 realname:reveal 权限（测试里连身份都没有）就必须是脱敏串。
func TestMaskIDRowsWithoutReveal(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)

	rows := []Customer{{Name: "张三", IdNo: "41018119890415451X"}, {Name: "李四", IdNo: ""}}
	maskIDRows(c, rows)
	if rows[0].IdNo != "410181********451X" {
		t.Errorf("列表未脱敏：%q", rows[0].IdNo)
	}
	if rows[1].IdNo != "" {
		t.Errorf("空证件号不该被写成星号：%q", rows[1].IdNo)
	}

	cu := &Customer{Name: "张三", IdNo: "41018119890415451X"}
	if !canRevealID(c) {
		cu.IdNo = mask.IDCard(cu.IdNo)
	}
	if cu.IdNo != "410181********451X" {
		t.Errorf("详情未脱敏：%q", cu.IdNo)
	}
}

func asResponseError(err error, target **response.Error) bool {
	e, ok := err.(*response.Error)
	if ok {
		*target = e
	}
	return ok
}
