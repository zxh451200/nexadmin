package report

import (
	"strings"
	"testing"
)

// 内置内容不连库也要自证：列必须在白名单里、图表配置必须过插件自己的校验、
// 引用必须闭合。白名单收紧或字段改名时，这里会先红，而不是等客户在页面上看到空图。

func datasetByName(t *testing.T, name string) seedDataset {
	t.Helper()
	for _, d := range seedDatasets() {
		if d.name == name {
			return d
		}
	}
	t.Fatalf("图表引用了不存在的数据集 %q", name)
	return seedDataset{}
}

func TestSeedDatasetsUseWhitelistedColumns(t *testing.T) {
	reg := newRegistry(defaultRaw(), nil)
	for _, d := range seedDatasets() {
		src, ok := reg.Get(d.source)
		if !ok {
			t.Errorf("数据集 %q 的表 %q 不在白名单", d.name, d.source)
			continue
		}
		if len(d.columns) == 0 {
			t.Errorf("数据集 %q 没有选列", d.name)
		}
		for _, col := range d.columns {
			if _, ok := src.Col(col); !ok {
				t.Errorf("数据集 %q 选了白名单外的列 %q（表 %q）", d.name, col, d.source)
			}
		}
	}
}

func TestSeedChartsPassOwnValidators(t *testing.T) {
	names := map[string]bool{}
	for _, c := range seedCharts() {
		if names[c.name] {
			t.Errorf("图表名称重复：%q", c.name)
		}
		names[c.name] = true

		ds := datasetByName(t, c.dataset)
		if err := validateChartShape(c.typ, c.spec); err != nil {
			t.Errorf("图表 %q 形状不合法：%v", c.name, err)
		}
		if err := validateColors(c.colors); err != nil {
			t.Errorf("图表 %q 颜色不合法：%v", c.name, err)
		}
		if c.spec.Limit < 1 || c.spec.Limit > MaxLimit {
			t.Errorf("图表 %q limit=%d，应在 1..%d", c.name, c.spec.Limit, MaxLimit)
		}
		allowed := map[string]bool{}
		for _, col := range ds.columns {
			allowed[col] = true
		}
		cols := []string{}
		for _, d := range c.spec.Dimensions {
			cols = append(cols, d.Column)
		}
		for _, m := range c.spec.Measures {
			cols = append(cols, m.Column)
		}
		for _, f := range c.spec.Filters {
			cols = append(cols, f.Column)
		}
		for _, col := range cols {
			if col == "" {
				continue // count(*) 允许空列
			}
			if !allowed[col] {
				t.Errorf("图表 %q 用了数据集 %q 未选的列 %q", c.name, c.dataset, col)
			}
		}
		for _, d := range c.spec.Dimensions {
			if d.Bucket != "" && d.Bucket != "day" && d.Bucket != "month" && d.Bucket != "year" {
				t.Errorf("图表 %q 的时间粒度 %q 无效", c.name, d.Bucket)
			}
		}
	}
}

func TestSeedReportItemsResolve(t *testing.T) {
	charts := map[string]bool{}
	for _, c := range seedCharts() {
		charts[c.name] = true
	}
	name, group, items, filterDS, _ := builtInReport()
	if name == "" || group == "" {
		t.Fatal("内置报表缺少名称或分组")
	}
	if len(items) == 0 || len(items) > maxReportItems {
		t.Fatalf("内置报表组件数 %d 超出 1..%d", len(items), maxReportItems)
	}
	seen := map[string]bool{}
	for _, it := range items {
		if !charts[it.chart] {
			t.Errorf("报表引用了不存在的图表 %q", it.chart)
		}
		if it.span < 1 || it.span > 12 {
			t.Errorf("图表 %q 的 span=%d 不在 1..12", it.chart, it.span)
		}
		if seen[it.chart] {
			t.Errorf("报表重复引用图表 %q", it.chart)
		}
		seen[it.chart] = true
	}
	_ = datasetByName(t, filterDS)
}

func TestSeedScreenLayoutValid(t *testing.T) {
	charts := map[string]bool{}
	for _, c := range seedCharts() {
		charts[c.name] = true
	}
	name, refresh, items := builtInScreen()
	if name == "" {
		t.Fatal("内置大屏缺少名称")
	}
	if refresh < 5 || refresh > 3600 {
		t.Errorf("大屏刷新 %d 秒不在 5..3600", refresh)
	}
	sc := make([]ScreenItem, 0, len(items))
	for _, it := range items {
		if !charts[it.chart] {
			t.Errorf("大屏引用了不存在的图表 %q", it.chart)
		}
		sc = append(sc, ScreenItem{ChartID: it.chart, X: it.x, Y: it.y, W: it.w, H: it.h, Title: it.chart})
	}
	if err := validateScreenItems(sc); err != nil {
		t.Errorf("内置大屏布局不合法：%v", err)
	}
}

// 内置内容必须能被配置解码器原样读回：字段名写错会在种入后变成「配置损坏」。
func TestSeedConfigsDecodeStrict(t *testing.T) {
	for _, c := range seedCharts() {
		cfg, err := encodeJSON(ChartConfig{Spec: c.spec, Colors: c.colors, Stacked: c.stacked, Unit: c.unit})
		if err != nil {
			t.Fatalf("图表 %q 编码失败：%v", c.name, err)
		}
		var back ChartConfig
		if err := decodeStrict(cfg, &back); err != nil {
			t.Errorf("图表 %q 配置回读失败：%v", c.name, err)
		}
	}
	for _, d := range seedDatasets() {
		cfg, err := encodeJSON(DatasetConfig{Columns: d.columns})
		if err != nil {
			t.Fatalf("数据集 %q 编码失败：%v", d.name, err)
		}
		var back DatasetConfig
		if err := decodeStrict(cfg, &back); err != nil {
			t.Errorf("数据集 %q 配置回读失败：%v", d.name, err)
		}
		if len(back.Columns) != len(d.columns) {
			t.Errorf("数据集 %q 列数回读不一致", d.name)
		}
	}
}

func TestSeedIdempotencyMarkIsStable(t *testing.T) {
	if builtinMark == "" {
		t.Fatal("内置标记不能为空：种入幂等靠它判断")
	}
	for _, d := range seedDatasets() {
		if d.name == builtinMark {
			t.Fatal("数据集名称与内置标记撞车")
		}
	}
}

// 内置内容必须存 i18n 键：存中文就等于把英文界面写死成中文。
func TestSeedContentUsesI18nKeys(t *testing.T) {
	var names, texts []string
	for _, d := range seedDatasets() {
		names = append(names, d.name)
	}
	for _, c := range seedCharts() {
		names = append(names, c.name, c.dataset)
		for _, m := range c.spec.Measures {
			texts = append(texts, m.Label)
		}
		texts = append(texts, c.unit)
	}
	rn, rg, _, _, _ := builtInReport()
	names = append(names, rn, rg)
	sn, _, sitems := builtInScreen()
	names = append(names, sn)
	for _, it := range sitems {
		names = append(names, it.chart)
	}
	check := func(kind string, vals []string) {
		for _, v := range vals {
			if v == "" {
				continue
			}
			if !strings.HasPrefix(v, keyPrefix) {
				t.Errorf("%s 未走 i18n 键：%q", kind, v)
			}
		}
	}
	check("名称", names)
	check("文案", texts)
}

// 升级字典的目标键必须真的是种进去的那批键：写错一个字符，老租户就会得到一套中文 + 一套键。
func TestUpgradeMapsCoverSeededKeys(t *testing.T) {
	seeded := map[string]bool{}
	for _, d := range seedDatasets() {
		seeded[d.name] = true
	}
	for _, c := range seedCharts() {
		seeded[c.name] = true
		for _, m := range c.spec.Measures {
			seeded[m.Label] = true
		}
		if c.unit != "" {
			seeded[c.unit] = true
		}
	}
	rn, rg, _, _, rFilter := builtInReport()
	seeded[rn] = true
	seeded[rg] = true
	seeded[rFilter] = true
	sn, _, sitems := builtInScreen()
	seeded[sn] = true
	for _, it := range sitems {
		seeded[it.chart] = true
	}
	for old, next := range builtinNames {
		if !seeded[next] {
			t.Errorf("builtinNames[%q] 指向 %q，但种入内容里没有这个键", old, next)
		}
		if strings.HasPrefix(old, keyPrefix) {
			t.Errorf("升级字典的键应是 v1 中文，不该是键：%q", old)
		}
	}
	for old, next := range builtinTexts {
		if !seeded[next] {
			t.Errorf("builtinTexts[%q] 指向 %q，但种入内容里没有这个键", old, next)
		}
	}
}

// 配置里的度量名/单位/过滤器名/大屏标题都要换掉；用户自己写的文案保持原样。
func TestUpgradeConfigHelpers(t *testing.T) {
	cfg := &ChartConfig{Spec: Spec{
		Measures: []Measure{{Agg: "count", Label: "登录次数"}, {Agg: "sum", Label: "我的自定义度量"}},
	}, Unit: "次"}
	if !upgradeChartConfig(cfg) {
		t.Fatal("应判定为有改动")
	}
	if cfg.Spec.Measures[0].Label != mLoginCount || cfg.Unit != uTimes {
		t.Fatalf("内置文案未替换：%+v", cfg)
	}
	if cfg.Spec.Measures[1].Label != "我的自定义度量" {
		t.Fatalf("用户文案被误改：%q", cfg.Spec.Measures[1].Label)
	}

	rc := &ReportConfig{Filters: []ReportFilter{{Key: "range", Label: "时间范围"}, {Key: "x", Label: "我的筛选"}}}
	if !upgradeReportConfig(rc) || rc.Filters[0].Label != fRange || rc.Filters[1].Label != "我的筛选" {
		t.Fatalf("报表过滤器升级结果不对：%+v", rc.Filters)
	}

	sc := &ScreenConfig{Title: "值班大屏", Items: []ScreenItem{{Title: "登录量趋势"}, {Title: "我自己加的图"}}}
	if !upgradeScreenConfig(sc) || sc.Title != sDuty || sc.Items[0].Title != cLoginTrend || sc.Items[1].Title != "我自己加的图" {
		t.Fatalf("大屏升级结果不对：%+v", sc)
	}

	// 已经升级过的配置再跑一遍必须是无操作
	before := *sc
	if upgradeScreenConfig(sc) || before.Title != sc.Title {
		t.Fatal("重复升级不应产生改动")
	}
}
