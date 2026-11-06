package config

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 有硬范围的参数必须在落库前拦住：core/auth 对越界值一律回退出厂值，
// 于是「界面存了 99、登录页出 5 位」——设置页显示的数和真正生效的数分家，
// 现场反馈永远对不上号。这条用例同时钉住「范围取自 core/auth 的常量」，
// 防止有人在这里另写一份 4/8 然后两边各自漂移。
func TestCheckKeyRangeRejectsOutOfRange(t *testing.T) {
	cases := []struct {
		key   string
		value string
		ok    bool
	}{
		{"login.captcha_length", "4", true},
		{"login.captcha_length", "5", true},
		{"login.captcha_length", "8", true},
		{"login.captcha_length", " 6 ", true},
		{"login.captcha_length", "3", false},
		{"login.captcha_length", "9", false},
		{"login.captcha_length", "99", false},
		{"login.captcha_length", "0", false},
		{"login.captcha_length", "-1", false},
		{"login.captcha_length", "6.5", false},
		{"login.captcha_length", "", false},
		{"login.captcha", "5", true},
		{"login.captcha_enabled", "true", true},
	}
	for _, tc := range cases {
		err := checkKeyRange(tc.key, tc.value)
		if tc.ok && err != nil {
			t.Errorf("checkKeyRange(%q, %q) 不该报错：%v", tc.key, tc.value, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("checkKeyRange(%q, %q) 应当拒绝", tc.key, tc.value)
		}
	}
}

// 范围校验必须挂在写入链路上，而不只是作为一个可调用到的函数：
// 只测纯函数的话，谁把 Create/Update 里那行调用删了都还是绿的。
func TestUpdateRejectsOutOfRangeBeforeWriting(t *testing.T) {
	s, mock := mockService(t)
	cols := []string{"id", "name", "config_key", "config_value", "config_type"}
	row := sqlmock.NewRows(cols).AddRow(int64(9), "登录验证码位数", "login.captcha_length", "5", TypeNumber)
	// Get（前置存在性检查）查一次；越界之后不该再有 UPDATE。
	mock.ExpectQuery("SELECT .+FROM `sys_config`").WillReturnRows(row)

	in := Input{Name: "登录验证码位数", Key: "login.captcha_length", Value: "99", Type: TypeNumber}
	_, err := s.Update(context.Background(), idgen.ID(9), in)
	var re *response.Error
	if !errors.As(err, &re) || re.Status != http.StatusBadRequest || re.Key != "config.value_out_of_range" {
		t.Fatalf("越界值应在落库前被拒（400 config.value_out_of_range），实际 %v", err)
	}
	if len(re.Args) != 2 {
		t.Errorf("错误文案要带得出允许范围，实际参数 %v", re.Args)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("越界判定不该发出写 SQL：%v", err)
	}
}

// 范围表的上下限必须与 core/auth 的钳制常量同值：不同源就意味着「能存进去但生效不了」。
func TestCaptchaLengthRangeMatchesAuthBounds(t *testing.T) {
	rg, ok := valueRanges["login.captcha_length"]
	if !ok {
		t.Fatal("login.captcha_length 应当登记在 valueRanges 里")
	}
	if rg[0] != auth.CaptchaLenMin || rg[1] != auth.CaptchaLenMax {
		t.Errorf("范围与 core/auth 不一致：这里 [%d,%d]，auth [%d,%d]", rg[0], rg[1], auth.CaptchaLenMin, auth.CaptchaLenMax)
	}
	def := parseCaptchaLength("5", auth.CaptchaLenDefault)
	if def != auth.CaptchaLenDefault || def < rg[0] || def > rg[1] {
		t.Errorf("出厂值必须自身落在范围内：default=%d range=[%d,%d]", def, rg[0], rg[1])
	}
}
