package region

import (
	"context"
	"errors"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/config"
	"nexadmin/core/i18n"
	"nexadmin/core/response"
)

// 版本号没有唯一索引，跨实例的重复号只能靠「留档前先在事务里锁住快照行」防住：
// syncMu 只在单个进程内串行，两个实例的首轮自动同步会各读到同一个 MAX(version)，
// 写出两条相同版本号——按版本号取档与回滚从此有歧义。
// 这条用例把 FOR UPDATE 钉住：它看起来像一处可有可无的防御，删掉不报错、不失败，只在多实例下坏。
func TestSaveArchivesUnderRowLock(t *testing.T) {
	mock, db := newMockDB(t)
	svc := NewService(db)

	mock.ExpectBegin()
	// 期望语句必须以 FOR UPDATE 结尾；gorm 没生成锁子句时这条会直接失败。
	mock.ExpectQuery("SELECT \\* FROM `sys_region_snapshot` WHERE id .* FOR UPDATE").
		WillReturnRows(sqlmock.NewRows(snapshotColumns).AddRow(snapshotRow("旧树", "sha-old", "alice", 34, 333, 2874)...))
	expectMaxVersion(mock, 7)
	expectVersionInsert(mock)
	mock.ExpectExec("UPDATE `sys_region_snapshot`").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := svc.save(context.Background(), snapshotModel("新树", "sha-new", "bob")); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("留档读取没有走行锁（多实例会写出重复版本号）：%v", err)
	}
}

// 后台同步的失败原因必须按查看者语言出整句：RegionSection 直接 toast(status.error)，
// 当场 Store(err.Error()) 会把 region.sync_running 这类 key 原文甩到界面上。
func TestSyncStatusViewRendersLocalizedFailure(t *testing.T) {
	useLocaleBundle(t)

	cases := []struct {
		name     string
		actor    string
		wantZH   []string
		wantEN   []string
		notWant  []string
		storeErr error
	}{
		{
			name:   "自动同步失败带来源，且是整句",
			actor:  autoSyncActor,
			storeErr: response.Conflict("region.sync_running"),
			wantZH: []string{"定时自动同步失败", "地区数据同步在跑"},
			wantEN: []string{"Scheduled automatic region sync failed"},
			// key 原文一个字都不许出现在界面上
			notWant: []string{"region.sync_running"},
		},
		{
			name:     "人工同步失败不加自动前缀",
			actor:    "alice",
			storeErr: response.BadRequest("region.source_missing"),
			wantZH:   []string{"还没有配置数据源"},
			wantEN:   []string{"No data source"},
			notWant:  []string{"定时自动同步失败", "Scheduled automatic"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			syncJob.Store(failedSyncState(tc.actor, tc.storeErr))
			t.Cleanup(func() { syncJob.Store(&syncState{}) })

			zh := (&Service{}).SyncStatusView(i18n.WithLang(context.Background(), "zh-CN")).Error
			for _, want := range tc.wantZH {
				if !strings.Contains(zh, want) {
					t.Errorf("中文状态缺少 %q，实际：%q", want, zh)
				}
			}
			en := (&Service{}).SyncStatusView(i18n.WithLang(context.Background(), "en-US")).Error
			for _, want := range tc.wantEN {
				if !strings.Contains(en, want) {
					t.Errorf("英文状态缺少 %q，实际：%q", want, en)
				}
			}
			for _, bad := range tc.notWant {
				if strings.Contains(zh, bad) || strings.Contains(en, bad) {
					t.Errorf("界面出现了不该有的原文 %q（zh=%q en=%q）", bad, zh, en)
				}
			}
		})
	}
}

// 不是业务错误的失败（拉取超时、解析坏掉）没有 key 可翻，原文留着——
// 这类原因里的地址/耗时正是排查要的，不能为了本地化把它抹成「未知错误」。
func TestFailedSyncStateKeepsPlainError(t *testing.T) {
	st := failedSyncState("alice", errors.New("dial tcp 1.2.3.4:443: i/o timeout"))
	if st.errKey != "" {
		t.Errorf("普通错误不该被当成业务 key：%q", st.errKey)
	}
	if !strings.Contains(st.Error, "i/o timeout") {
		t.Errorf("原始错误信息丢了：%q", st.Error)
	}
	got := (&Service{}).SyncStatusView(context.Background())
	if !strings.Contains(got.Error, "i/o timeout") {
		t.Errorf("读状态时原文应原样给出，实际 %q", got.Error)
	}
}

// useLocaleBundle 装载随包词条：生产里由 core 启动时装，测试自己装一份，
// 否则 i18n.T 直接回 key，断言到的就不是用户看到的文案。
func useLocaleBundle(t *testing.T) {
	t.Helper()
	prev := i18n.Default()
	b, err := i18n.New([]config.Language{{Code: "zh-CN", Default: true}, {Code: "en-US"}}, "zh-CN", "none")
	if err != nil {
		t.Skipf("随包词条装载失败：%v", err)
	}
	i18n.SetDefault(b)
	t.Cleanup(func() { i18n.SetDefault(prev) })
}
