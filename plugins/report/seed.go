package report

// 出厂内置报表：插件首次为某个租户工作时种入一套开箱内容（数据集 → 图表 → 报表页 → 数据大屏），
// 避免新装客户打开报表中心只看到空壳。
//
// 约束：
//   - 只引用 sources.go 白名单里的表与列（seed_test.go 逐列校验，白名单收紧会立刻打红）；
//   - 幂等：按 Remark 上的内置标记 + 名称判断，种过就跳过；用户改名/删除的内容不会被覆盖或重复补种；
//   - 失败只记日志，不阻断启动（内置内容是锦上添花，不是启动前置条件）。

import (
	"context"
	"errors"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// builtinMark 标记内置行；改内容时升版本号，让老库保留旧行、新租户拿到新行。
const builtinMark = "内置模板 v2"

// prevMark 是上一版内置标记，只用于就地升级（见 upgradeBuiltins）。
const prevMark = "内置模板 v1"

// defaultTenantID 是平台默认租户：单租户部署与内置管理员都挂在它下面，它不在 sys_tenant 里。
const defaultTenantID = idgen.ID(1)

// 内置内容存 i18n 键而不是中文文案（与菜单名同一套机制）：报表前端只对
// plugin_report.builtin. 前缀做翻译，用户自建内容存原文，所以不会误伤。
const keyPrefix = "plugin_report.builtin."

const (
	dsLogin = keyPrefix + "ds.login"
	dsRun   = keyPrefix + "ds.cronRun"
	dsJob   = keyPrefix + "ds.job"
)

const (
	cLoginTrend  = keyPrefix + "chart.loginTrend"
	cLoginResult = keyPrefix + "chart.loginResult"
	cLoginFail   = keyPrefix + "chart.loginFail"
	cBrowser     = keyPrefix + "chart.browser"
	cLoginType   = keyPrefix + "chart.loginType"
	cRunTrend    = keyPrefix + "chart.runTrend"
	cJobCost     = keyPrefix + "chart.jobCost"
	cLoginTotal  = keyPrefix + "chart.loginTotal"
	cJobEnabled  = keyPrefix + "chart.jobEnabled"
	cJobStatus   = keyPrefix + "chart.jobStatus"
)

const (
	rOps   = keyPrefix + "report.ops"
	rGroup = keyPrefix + "group.overview"
	sDuty  = keyPrefix + "screen.duty"
)

const (
	mLoginCount = keyPrefix + "measure.loginCount"
	mCount      = keyPrefix + "measure.count"
	mFailCount  = keyPrefix + "measure.failCount"
	mRunCount   = keyPrefix + "measure.runCount"
	mAvgCost    = keyPrefix + "measure.avgCost"
	mLoginSum   = keyPrefix + "measure.loginSum"
	mJobCount   = keyPrefix + "measure.jobCount"
	mLastRunAt  = keyPrefix + "measure.lastRunAt"
	fRange      = keyPrefix + "filter.range"
	uTimes      = keyPrefix + "unit.times"
	uJobs       = keyPrefix + "unit.jobs"
)

type seedDataset struct {
	name    string
	source  string
	columns []string
}

type seedChart struct {
	name    string
	dataset string
	typ     string
	spec    Spec
	colors  []string
	stacked bool
	unit    string
}

type seedReportItem struct {
	chart string
	span  int
}

func seedDatasets() []seedDataset {
	return []seedDataset{
		{dsLogin, "sys_login_log", []string{"created_at", "login_type", "status", "fail_reason", "browser", "os"}},
		{dsRun, "plg_cron_log", []string{"job_name", "handler", "trigger_type", "status", "attempts", "cost_ms", "started_at", "finished_at"}},
		{dsJob, "plg_cron_job", []string{"name", "handler", "enabled", "retry_count", "last_status", "last_run_at", "created_at"}},
	}
}

func countMeasure(label string) Measure { return Measure{Agg: "count", Label: label} }

func seedCharts() []seedChart {
	return []seedChart{
		{cLoginTrend, dsLogin, ChartLine, Spec{
			Dimensions: []Dim{{Column: "created_at", Bucket: "day"}},
			Measures:   []Measure{countMeasure(mLoginCount)},
			OrderBy:    []Order{{Ref: "d0"}},
			Limit:      30,
		}, nil, false, ""},
		{cLoginResult, dsLogin, ChartPie, Spec{
			Dimensions: []Dim{{Column: "status"}},
			Measures:   []Measure{countMeasure(mCount)},
			OrderBy:    []Order{{Ref: "m0", Desc: true}},
			Limit:      10,
		}, nil, false, ""},
		{cLoginFail, dsLogin, ChartBar, Spec{
			Dimensions: []Dim{{Column: "fail_reason"}},
			Measures:   []Measure{countMeasure(mFailCount)},
			Filters:    []Filter{{Column: "fail_reason", Op: "!=", Value: ""}},
			OrderBy:    []Order{{Ref: "m0", Desc: true}},
			Limit:      10,
		}, nil, false, ""},
		{cBrowser, dsLogin, ChartPie, Spec{
			Dimensions: []Dim{{Column: "browser"}},
			Measures:   []Measure{countMeasure(mCount)},
			OrderBy:    []Order{{Ref: "m0", Desc: true}},
			Limit:      8,
		}, nil, false, ""},
		{cLoginType, dsLogin, ChartBar, Spec{
			Dimensions: []Dim{{Column: "login_type"}},
			Measures:   []Measure{countMeasure(mCount)},
			OrderBy:    []Order{{Ref: "m0", Desc: true}},
			Limit:      10,
		}, nil, false, ""},
		{cRunTrend, dsRun, ChartLine, Spec{
			Dimensions: []Dim{{Column: "started_at", Bucket: "day"}, {Column: "status"}},
			Measures:   []Measure{countMeasure(mRunCount)},
			OrderBy:    []Order{{Ref: "d0"}},
			Limit:      120,
		}, nil, true, ""},
		{cJobCost, dsRun, ChartBar, Spec{
			Dimensions: []Dim{{Column: "handler"}},
			Measures:   []Measure{{Agg: "avg", Column: "cost_ms", Label: mAvgCost}},
			OrderBy:    []Order{{Ref: "m0", Desc: true}},
			Limit:      10,
		}, nil, false, ""},
		{cLoginTotal, dsLogin, ChartMetric, Spec{
			Measures: []Measure{countMeasure(mLoginSum)},
			Limit:    1,
		}, nil, false, uTimes},
		{cJobEnabled, dsJob, ChartMetric, Spec{
			Measures: []Measure{countMeasure(mJobCount)},
			Filters:  []Filter{{Column: "enabled", Op: "=", Value: true}},
			Limit:    1,
		}, nil, false, uJobs},
		{cJobStatus, dsJob, ChartTable, Spec{
			Dimensions: []Dim{{Column: "name"}, {Column: "handler"}, {Column: "last_status"}},
			Measures:   []Measure{{Agg: "max", Column: "last_run_at", Label: mLastRunAt}},
			OrderBy:    []Order{{Ref: "m0", Desc: true}},
			Limit:      20,
		}, nil, false, ""},
	}
}

// builtInReport 是内置报表页「运营与运维总览」：12 列栅格流式排布。
func builtInReport() (name string, group string, items []seedReportItem, filterDataset, filterLabel string) {
	return rOps, rGroup, []seedReportItem{
		{cLoginTotal, 3}, {cJobEnabled, 3}, {cLoginTrend, 6},
		{cRunTrend, 12},
		{cLoginResult, 4}, {cBrowser, 4}, {cLoginType, 4},
		{cLoginFail, 6}, {cJobCost, 6},
		{cJobStatus, 12},
	}, dsLogin, fRange
}

// builtInScreen 是内置大屏「值班大屏」：24 列栅格，30 秒自动刷新。
func builtInScreen() (name string, refreshSec int, items []struct {
	chart      string
	x, y, w, h int
}) {
	return sDuty, 30, []struct {
		chart      string
		x, y, w, h int
	}{
		{cLoginTotal, 0, 0, 6, 4},
		{cJobEnabled, 6, 0, 6, 4},
		{cLoginTrend, 12, 0, 12, 8},
		{cRunTrend, 0, 4, 12, 8},
		{cLoginResult, 0, 12, 6, 7},
		{cBrowser, 6, 12, 6, 7},
		{cLoginFail, 12, 12, 6, 7},
		{cJobCost, 18, 12, 6, 7},
		{cLoginType, 0, 19, 12, 7},
		{cJobStatus, 12, 19, 12, 7},
	}
}

// seedBuiltins 为每个租户补种一次内置内容。
func seedBuiltins(db *gorm.DB, log *zap.Logger) {
	if db == nil {
		return
	}
	if log == nil {
		log = zap.NewNop()
	}
	ids := seedTenantIDs(db)
	for _, tid := range ids {
		ctx := tenant.With(context.Background(), tid)
		created, err := seedTenant(db.WithContext(ctx), tid, log)
		if err != nil {
			log.Warn("报表内置内容种入失败（不影响启动）", zap.String("tenant", tid.String()), zap.Error(err))
			continue
		}
		log.Info("报表内置内容检查完成", zap.String("tenant", tid.String()), zap.Int("created", created))
	}
}

// seedTenantIDs 取需要种内容的租户：平台默认租户 + 有账号的租户 + 已开通的租户。
// 只查 sys_tenant 会漏掉默认租户，那样管理员打开报表中心仍然是空壳。
func seedTenantIDs(db *gorm.DB) []idgen.ID {
	ctx := tenant.Skip(context.Background())
	out := []idgen.ID{defaultTenantID}
	var ids []idgen.ID
	if err := db.WithContext(ctx).Table("sys_user").Distinct("tenant_id").Pluck("tenant_id", &ids).Error; err == nil {
		out = append(out, ids...)
	}
	ids = nil
	if err := db.WithContext(ctx).Table("sys_tenant").Pluck("id", &ids).Error; err == nil {
		out = append(out, ids...)
	}
	seen := map[idgen.ID]bool{}
	kept := out[:0]
	for _, id := range out {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		kept = append(kept, id)
	}
	return kept
}

// seedTenant 补种一个租户的内置内容；返回本次新建的行数（0 表示早已种过或只做校验）。
func seedTenant(db *gorm.DB, tid idgen.ID, log *zap.Logger) (int, error) {
	created := 0
	// 先把 v1 的中文内置内容就地升级成 v2 的 i18n 键，再判断要不要补种；
	// 顺序反了会给老租户多插一套重复内容。
	renamed, err := upgradeBuiltins(db, tid)
	if err != nil {
		return created, err
	}
	if renamed > 0 {
		log.Info("报表内置内容已升级为多语言键", zap.String("tenant", tid.String()), zap.Int("renamed", renamed))
	}
	if ok, err := alreadySeeded(db, tid); err != nil || ok {
		return created, err
	}
	tx := db.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}
	datasets := map[string]idgen.ID{}
	for _, d := range seedDatasets() {
		id, isNew, err := ensureDataset(tx, tid, d)
		if err != nil {
			tx.Rollback()
			return created, err
		}
		if isNew {
			created++
		}
		datasets[d.name] = id
	}
	charts := map[string]idgen.ID{}
	for _, c := range seedCharts() {
		id, isNew, err := ensureChart(tx, tid, c, datasets[c.dataset])
		if err != nil {
			tx.Rollback()
			return created, err
		}
		if isNew {
			created++
		}
		charts[c.name] = id
	}
	n, err := ensureReport(tx, tid, charts, datasets)
	if err != nil {
		tx.Rollback()
		return created, err
	}
	created += n
	m, err := ensureScreen(tx, tid, charts)
	if err != nil {
		tx.Rollback()
		return created, err
	}
	created += m
	if err := tx.Commit().Error; err != nil {
		return created, err
	}
	return created, nil
}

// alreadySeeded 判断该租户是否已种过。这里用 Table() 裸查，租户插件不会自动加条件，
// 所以必须显式带 tenant_id——否则会拿别的租户的行误判成「已种过」，后面的租户全都种不进去。
func alreadySeeded(db *gorm.DB, tid idgen.ID) (bool, error) {
	for _, tbl := range []string{"plg_report_dataset", "plg_report_report", "plg_report_screen"} {
		var n int64
		// v1 也算「种过」：老租户由 upgradeBuiltins 就地改名，不能再补一套重复内容。
		if err := db.Table(tbl).Where("remark IN ? AND tenant_id = ?", []string{builtinMark, prevMark}, tid).Count(&n).Error; err != nil {
			return false, err // 迁移未跑：表不存在，跳过本租户
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

func ensureDataset(db *gorm.DB, tid idgen.ID, d seedDataset) (idgen.ID, bool, error) {
	var got Dataset
	err := db.Where("remark = ? and name = ?", builtinMark, d.name).First(&got).Error
	if errors.Is(err, nil) {
		return got.ID, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, err
	}
	cfg, err := encodeJSON(DatasetConfig{Columns: d.columns})
	if err != nil {
		return 0, false, err
	}
	row := Dataset{
		TenantModel: database.TenantModel{PlatformModel: database.PlatformModel{ID: nextID()}, TenantID: tid},
		Name:        d.name, Source: d.source, Config: cfg, Remark: builtinMark,
	}
	if err := db.Create(&row).Error; err != nil {
		return 0, false, err
	}
	return row.ID, true, nil
}

func ensureChart(db *gorm.DB, tid idgen.ID, c seedChart, datasetID idgen.ID) (idgen.ID, bool, error) {
	var got Chart
	err := db.Where("remark = ? and name = ?", builtinMark, c.name).First(&got).Error
	if errors.Is(err, nil) {
		return got.ID, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, err
	}
	cfg, err := encodeJSON(ChartConfig{Spec: c.spec, Colors: c.colors, Stacked: c.stacked, Unit: c.unit})
	if err != nil {
		return 0, false, err
	}
	row := Chart{
		TenantModel: database.TenantModel{PlatformModel: database.PlatformModel{ID: nextID()}, TenantID: tid},
		Name:        c.name, DatasetID: datasetID, Type: c.typ, Config: cfg, Remark: builtinMark,
	}
	if err := db.Create(&row).Error; err != nil {
		return 0, false, err
	}
	return row.ID, true, nil
}

func ensureReport(db *gorm.DB, tid idgen.ID, charts, datasets map[string]idgen.ID) (int, error) {
	name, group, items, filterDS, filterLabel := builtInReport()
	var got ReportDef
	err := db.Where("remark = ? and name = ?", builtinMark, name).First(&got).Error
	if err == nil {
		return 0, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	rc := ReportConfig{Items: make([]ReportItem, 0, len(items))}
	for _, it := range items {
		rc.Items = append(rc.Items, ReportItem{ChartID: charts[it.chart].String(), Span: it.span})
	}
	rc.Filters = []ReportFilter{{
		Key: "range", Label: filterLabel, Type: "dateRange",
		DatasetID: datasets[filterDS].String(), Column: "created_at",
	}}
	cfg, err := encodeJSON(rc)
	if err != nil {
		return 0, err
	}
	row := ReportDef{
		TenantModel: database.TenantModel{PlatformModel: database.PlatformModel{ID: nextID()}, TenantID: tid},
		Name:        name, GroupName: group, Config: cfg, Remark: builtinMark,
	}
	if err := db.Create(&row).Error; err != nil {
		return 0, err
	}
	return 1, nil
}

func ensureScreen(db *gorm.DB, tid idgen.ID, charts map[string]idgen.ID) (int, error) {
	name, refresh, items := builtInScreen()
	var got Screen
	err := db.Where("remark = ? and name = ?", builtinMark, name).First(&got).Error
	if err == nil {
		return 0, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	sc := ScreenConfig{RefreshSec: refresh, Title: name, Items: make([]ScreenItem, 0, len(items))}
	for _, it := range items {
		sc.Items = append(sc.Items, ScreenItem{ChartID: charts[it.chart].String(), X: it.x, Y: it.y, W: it.w, H: it.h, Title: it.chart})
	}
	cfg, err := encodeJSON(sc)
	if err != nil {
		return 0, err
	}
	row := Screen{
		TenantModel: database.TenantModel{PlatformModel: database.PlatformModel{ID: nextID()}, TenantID: tid},
		Name:        name, Config: cfg, Remark: builtinMark,
	}
	if err := db.Create(&row).Error; err != nil {
		return 0, err
	}
	return 1, nil
}

func nextID() idgen.ID {
	id, err := idgen.Next()
	if err != nil {
		return 0 // 交给 BeforeCreate 兜底生成
	}
	return id
}

// ---- v1 → v2 就地升级：内置内容从「写死中文」改成「i18n 键」----

// builtinNames 是 v1 的内置行名 → v2 的键。
var builtinNames = map[string]string{
	"登录明细": dsLogin, "定时任务执行明细": dsRun, "定时任务清单": dsJob,
	"登录量趋势": cLoginTrend, "登录结果分布": cLoginResult, "登录失败原因 Top10": cLoginFail,
	"浏览器占比": cBrowser, "登录方式分布": cLoginType, "任务执行趋势（按状态）": cRunTrend,
	"任务平均耗时 Top10": cJobCost, "累计登录次数": cLoginTotal, "启用中的定时任务": cJobEnabled,
	"任务最近状态一览": cJobStatus, "运营与运维总览": rOps, "总览": rGroup, "值班大屏": sDuty,
}

// builtinTexts 是配置 JSON 里的度量名 / 过滤器名 / 单位 → 键。整字段精确匹配，不做子串替换。
var builtinTexts = map[string]string{
	"登录次数": mLoginCount, "次数": mCount, "失败次数": mFailCount, "执行次数": mRunCount,
	"平均耗时(ms)": mAvgCost, "累计登录": mLoginSum, "任务数": mJobCount, "最近执行时间": mLastRunAt,
	"时间范围": fRange, "次": uTimes, "个": uJobs,
}

// upgradeBuiltins 把老租户的 v1 内置行就地改成 v2（名字与配置里的文案都换成 i18n 键）。
// 只动「值仍是 v1 内置文案」的行/字段：用户改过名的保持原样，也不会因此被补种第二套。
// 返回被改行的数量。
func upgradeBuiltins(db *gorm.DB, tid idgen.ID) (int, error) {
	changed := 0
	var datasets []Dataset
	if err := db.Where("remark = ?", prevMark).Find(&datasets).Error; err != nil {
		return changed, err
	}
	for _, d := range datasets {
		next, ok := builtinNames[d.Name]
		if !ok {
			continue
		}
		if err := db.Model(&Dataset{}).Where("id = ?", d.ID).
			Updates(map[string]any{"name": next, "remark": builtinMark}).Error; err != nil {
			return changed, err
		}
		changed++
	}

	var charts []Chart
	if err := db.Where("remark = ?", prevMark).Find(&charts).Error; err != nil {
		return changed, err
	}
	for _, c := range charts {
		var cfg ChartConfig
		if err := decodeStrict(c.Config, &cfg); err != nil {
			return changed, err
		}
		touched := upgradeChartConfig(&cfg)
		name, renamed := builtinNames[c.Name]
		if !touched && !renamed {
			continue
		}
		up := map[string]any{"remark": builtinMark}
		if renamed {
			up["name"] = name
		}
		if touched {
			raw, err := encodeJSON(cfg)
			if err != nil {
				return changed, err
			}
			up["config"] = raw
		}
		if err := db.Model(&Chart{}).Where("id = ?", c.ID).Updates(up).Error; err != nil {
			return changed, err
		}
		changed++
	}

	var reports []ReportDef
	if err := db.Where("remark = ?", prevMark).Find(&reports).Error; err != nil {
		return changed, err
	}
	for _, r := range reports {
		var cfg ReportConfig
		if err := decodeStrict(r.Config, &cfg); err != nil {
			return changed, err
		}
		touched := upgradeReportConfig(&cfg)
		up := map[string]any{"remark": builtinMark}
		if next, ok := builtinNames[r.Name]; ok {
			up["name"] = next
			touched = true
		}
		if next, ok := builtinNames[r.GroupName]; ok {
			up["group_name"] = next
			touched = true
		}
		if !touched {
			continue
		}
		raw, err := encodeJSON(cfg)
		if err != nil {
			return changed, err
		}
		up["config"] = raw
		if err := db.Model(&ReportDef{}).Where("id = ?", r.ID).Updates(up).Error; err != nil {
			return changed, err
		}
		changed++
	}

	var screens []Screen
	if err := db.Where("remark = ?", prevMark).Find(&screens).Error; err != nil {
		return changed, err
	}
	for _, sc := range screens {
		var cfg ScreenConfig
		if err := decodeStrict(sc.Config, &cfg); err != nil {
			return changed, err
		}
		touched := upgradeScreenConfig(&cfg)
		up := map[string]any{"remark": builtinMark}
		if next, ok := builtinNames[sc.Name]; ok {
			up["name"] = next
			touched = true
		}
		if !touched {
			continue
		}
		raw, err := encodeJSON(cfg)
		if err != nil {
			return changed, err
		}
		up["config"] = raw
		if err := db.Model(&Screen{}).Where("id = ?", sc.ID).Updates(up).Error; err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// upgradeChartConfig / upgradeReportConfig / upgradeScreenConfig 把配置里的 v1 中文文案换成键，
// 返回是否有改动。单独抽出来是为了能不带库跑测试。
func upgradeChartConfig(cfg *ChartConfig) bool {
	touched := false
	for i := range cfg.Spec.Measures {
		if next, ok := builtinTexts[cfg.Spec.Measures[i].Label]; ok {
			cfg.Spec.Measures[i].Label = next
			touched = true
		}
	}
	if next, ok := builtinTexts[cfg.Unit]; ok {
		cfg.Unit = next
		touched = true
	}
	return touched
}

func upgradeReportConfig(cfg *ReportConfig) bool {
	touched := false
	for i := range cfg.Filters {
		if next, ok := builtinTexts[cfg.Filters[i].Label]; ok {
			cfg.Filters[i].Label = next
			touched = true
		}
	}
	return touched
}

func upgradeScreenConfig(cfg *ScreenConfig) bool {
	touched := false
	if next, ok := builtinNames[cfg.Title]; ok {
		cfg.Title = next
		touched = true
	}
	for i := range cfg.Items {
		if next, ok := builtinNames[cfg.Items[i].Title]; ok {
			cfg.Items[i].Title = next
			touched = true
		}
	}
	return touched
}
