package idcard

// 证件类型。号码首位/前缀就能区分，不需要额外输入。
const (
	KindResidentID   = "resident_id"   // 大陆居民身份证（含 15 位旧证）
	KindHMTPermit    = "hmt_permit"    // 港澳台居民居住证：810000 港、820000 澳、830000 台
	KindForeignerPRP = "foreigner_prp" // 外国人永久居留身份证（"五星卡"）
)

// hmtRegionNames 是证件口径的归属地名称。行政区划表里有 810000/820000，
// 但"台湾"只有区划口径的 710000 台湾省，没有证件口径的 830000，
// 所以这里补一张三行的映射，避免把台湾居民居住证显示成"归属地未识别"。
var hmtRegionNames = map[string]string{
	"810000": "香港特别行政区",
	"820000": "澳门特别行政区",
	"830000": "台湾地区",
}

// detectKind 按 GB 11643 与新版永居证编号规则识别证件类型。输入必须是已补齐的 18 位号码。
//
// 五星卡编号（国家移民管理局《新版外国人永久居留身份证适配性改造要点》）：第 1 位
// 身份标识码 9、第 2-3 位申领地省级代码、第 4-6 位国籍的 ISO 3166-1 数字代码，
// 第 7-14 位出生日期、第 15-17 位顺序码、第 18 位校验码；同一份文件明确校验码
// "采用 MOD11-2 算法……与公民身份号码校验方式相同"，所以这里复用同一套校验位算法。
func detectKind(number string) string {
	switch {
	case len(number) == 18 && number[0] == '9':
		return KindForeignerPRP
	case len(number) == 18:
		if _, ok := hmtRegionNames[number[:6]]; ok {
			return KindHMTPermit
		}
	}
	return KindResidentID
}

// prpRegionCode 取五星卡的申领地省级代码并补成 6 位区划码。
func prpRegionCode(number string) string { return number[1:3] + "0000" }

// prpCountryNumeric 取五星卡号码里的 ISO 3166-1 数字国籍码。
func prpCountryNumeric(number string) string { return number[3:6] }
