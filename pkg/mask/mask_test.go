package mask

import (
	"testing"
)

func TestPhone(t *testing.T) {
	if got := Phone("13812341234"); got != "138****1234" {
		t.Fatalf("Phone = %q", got)
	}
	if got := Phone(""); got != "" {
		t.Fatalf("empty = %q", got)
	}
	if got := Phone("138"); got != "***" {
		t.Fatalf("too short = %q", got)
	}
}

func TestEmail(t *testing.T) {
	if got := Email("zhang@example.com"); got != "zh***@example.com" {
		t.Fatalf("Email = %q", got)
	}
	if got := Email("a@b.com"); got != "a***@b.com" {
		t.Fatalf("short = %q", got)
	}
	if got := Email("nodomain"); got != "***" {
		t.Fatalf("no @ = %q", got)
	}
}

func TestIDCard(t *testing.T) {
	// 18 位身份证：前 6 + 8 星 + 后 4。
	if got := IDCard("110101199001011234"); got != "110101********1234" {
		t.Fatalf("IDCard = %q", got)
	}
}

func TestBankCard(t *testing.T) {
	if got := BankCard("6222021234567890123"); got != "***************0123" {
		t.Fatalf("BankCard = %q", got)
	}
}

func TestName(t *testing.T) {
	if got := Name("张三丰"); got != "张**" {
		t.Fatalf("Chinese = %q", got)
	}
	if got := Name("Alice Smith"); got != "Al*********" {
		t.Fatalf("English = %q", got)
	}
	if got := Name("李"); got != "李" {
		t.Fatalf("single = %q", got)
	}
}

func TestCustom(t *testing.T) {
	// "hello world" 11 字符：前 2 + 6 星 + 后 3。
	if got := Custom("hello world", 2, 3); got != "he******rld" {
		t.Fatalf("Custom = %q", got)
	}
	if got := Custom("ab", 1, 1); got != "***" {
		t.Fatalf("too short = %q", got)
	}
}

type testUser struct {
	Name     string `json:"name" mask:"name"`
	Phone    string `json:"phone" mask:"phone"`
	Email    string `json:"email" mask:"email"`
	IDCard   string `json:"idCard" mask:"id_card"`
	BankCard string `json:"bankCard" mask:"bank_card"`
	Public   string `json:"public"`
	Nested   SubStruct
	Items    []testItem
}

type SubStruct struct {
	Secret string `json:"secret" mask:"phone"`
	Open   string `json:"open"`
}

type testItem struct {
	SKU   string `json:"sku"`
	Price string `json:"price" mask:"custom:1:2"`
}

func TestApply(t *testing.T) {
	u := testUser{
		Name:     "张三丰",
		Phone:    "13812341234",
		Email:    "zhang@example.com",
		IDCard:   "110101199001011234",
		BankCard: "6222021234567890123",
		Public:   "public info",
		Nested:   SubStruct{Secret: "13899990000", Open: "visible"},
		Items: []testItem{
			{SKU: "SKU-001", Price: "123.45"},
		},
	}
	r := Apply(&u).(*testUser)

	if r.Name != "张**" {
		t.Fatalf("Name = %q", r.Name)
	}
	if r.Phone != "138****1234" {
		t.Fatalf("Phone = %q", r.Phone)
	}
	if r.Email != "zh***@example.com" {
		t.Fatalf("Email = %q", r.Email)
	}
	if r.IDCard != "110101********1234" {
		t.Fatalf("IDCard = %q", r.IDCard)
	}
	if r.BankCard != "***************0123" {
		t.Fatalf("BankCard = %q", r.BankCard)
	}
	if r.Public != "public info" {
		t.Fatalf("Public should not be masked")
	}
	if r.Nested.Secret != "138****0000" {
		t.Fatalf("Nested.Secret = %q", r.Nested.Secret)
	}
	if r.Nested.Open != "visible" {
		t.Fatalf("Nested.Open should not be masked")
	}
	if r.Items[0].Price != "1***45" {
		t.Fatalf("Items[0].Price = %q", r.Items[0].Price)
	}
	if r.Items[0].SKU != "SKU-001" {
		t.Fatalf("Items[0].SKU should not be masked")
	}
}

// Apply 不修改原始数据（返回副本）。
func TestApplyNoMutation(t *testing.T) {
	u := testUser{Phone: "13812341234"}
	_ = Apply(u)
	if u.Phone != "13812341234" {
		t.Fatalf("original mutated: %q", u.Phone)
	}
}

// Apply 处理切片和指针。
func TestApplySliceAndPtr(t *testing.T) {
	users := []testUser{{Phone: "13812341234"}, {Phone: "13999999999"}}
	r := Apply(users).([]testUser)
	if r[0].Phone != "138****1234" || r[1].Phone != "139****9999" {
		t.Fatalf("slice apply failed: %+v", r)
	}

	p := &testUser{Phone: "13812341234"}
	rp := Apply(p).(*testUser)
	if rp.Phone != "138****1234" {
		t.Fatalf("ptr apply failed: %q", rp.Phone)
	}
}
