package region

import (
	"strings"
	"unicode/utf8"

	"github.com/mozillazg/go-pinyin"
)

// 行政区划通名：生成拼音前先去掉（"长安区" → "长安"）。
var pinyinSuffixes = []string{
	"维吾尔自治区", "壮族自治区", "回族自治区", "特别行政区", "自治区",
	"自治州", "自治县", "自治旗", "地区", "林区", "矿区", "新区",
	"省", "市", "区", "县", "旗", "盟",
}

// pinyinOverrides 是多音字地名的读音表：通用字典按常用读音切分地名会读错
// （长治、六安、厦门、番禺、藏族自治州……）。表内是逐条人工核对过的结果，
// go-pinyin 的默认读音以外的地名仍走默认逻辑。
var pinyinOverrides = map[string]string{
	"宝坻区":         "baodi",
	"长安区":         "changan",
	"泊头市":         "botou",
	"大城县":         "dacheng",
	"矿区":          "kuangqu",
	"长治市":         "changzhi",
	"霍林郭勒市":       "huolinguole",
	"锡林郭勒盟":       "xilinguole",
	"长海县":         "changhai",
	"弓长岭区":        "gongchangling",
	"调兵山市":        "tiaobingshan",
	"长春市":         "changchun",
	"长白朝鲜族自治县":    "changbaichaoxianzu",
	"长岭县":         "changling",
	"大箐山县":        "daqingshan",
	"茄子河区":        "qiezihe",
	"长宁区":         "changning",
	"长兴县":         "changxing",
	"长丰县":         "changfeng",
	"蚌埠市":         "bengbu",
	"枞阳县":         "zongyang",
	"歙县":          "she",
	"天长市":         "tianchang",
	"六安市":         "luan",
	"长乐区":         "changle",
	"厦门市":         "xiamen",
	"长泰区":         "changtai",
	"长汀县":         "changting",
	"铅山县":         "yanshan",
	"长清区":         "changqing",
	"牟平区":         "muping",
	"东阿县":         "donge",
	"中牟县":         "zhongmu",
	"长垣市":         "changyuan",
	"长葛市":         "changge",
	"长阳土家族自治县":    "changyangtujiazu",
	"长沙市":         "changsha",
	"长沙县":         "changsha",
	"番禺区":         "panyu",
	"佛山市":         "foshan",
	"佛冈县":         "fogang",
	"长洲区":         "changzhou",
	"龙圩区":         "longxu",
	"覃塘区":         "qintang",
	"重庆市":         "chongqing",
	"长寿区":         "changshou",
	"成都市":         "chengdu",
	"都江堰市":        "dujiangyan",
	"犍为县":         "qianwei",
	"长宁县":         "changning",
	"荥经县":         "yingjing",
	"阿坝藏族羌族自治州":   "abazangzuqiangzu",
	"甘孜藏族自治州":     "ganzizangzu",
	"木里藏族自治县":     "mulizangzu",
	"长顺县":         "changshun",
	"牟定县":         "mouding",
	"弥勒市":         "mile",
	"迪庆藏族自治州":     "diqingzangzu",
	"西藏自治区":       "xizang",
	"长武县":         "changwu",
	"延长县":         "yanchang",
	"子长市":         "zichang",
	"佛坪县":         "foping",
	"天祝藏族自治县":     "tianzhuzangzu",
	"民乐县":         "minle",
	"崆峒区":         "kongtong",
	"甘南藏族自治州":     "gannanzangzu",
	"海北藏族自治州":     "haibeizangzu",
	"黄南藏族自治州":     "huangnanzangzu",
	"海南藏族自治州":     "hainanzangzu",
	"果洛藏族自治州":     "guoluozangzu",
	"玉树藏族自治州":     "yushuzangzu",
	"海西蒙古族藏族自治州":  "haiximengguzuzangzu",
	"库尔勒市":        "kuerle",
	"克孜勒苏柯尔克孜自治州": "kezilesukeerkezi",
	"疏勒县":         "shule",
	"伽师县":         "jiashi",
	"策勒县":         "cele",
	"尼勒克县":        "nileke",
	"阿勒泰地区":       "aletai",
	"阿勒泰市":        "aletai",
	"蔚县":          "yu",
	"乐亭县":         "laoting",
	"洪洞县":         "hongtong",
	"涡阳县":         "guoyang",
	"泌阳县":         "biyang",
	"乐清市":         "yueqing",
	"单县":          "shan",
	"黄陂区":         "huangpi",
}

var pinyinArgs = pinyin.NewArgs()

// Pinyin 返回地名的小写无声调拼音，ü 统一写作 v（与输入法一致），用于前端按拼音搜索。
func Pinyin(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if v, ok := pinyinOverrides[name]; ok {
		return v
	}
	base := name
	for _, s := range pinyinSuffixes {
		if strings.HasSuffix(base, s) && utf8.RuneCountInString(base) > utf8.RuneCountInString(s) {
			base = strings.TrimSuffix(base, s)
			break
		}
	}
	var b strings.Builder
	for _, p := range pinyin.Pinyin(base, pinyinArgs) {
		if len(p) == 0 {
			continue
		}
		b.WriteString(p[0])
	}
	return strings.ReplaceAll(strings.ToLower(b.String()), "ü", "v")
}
