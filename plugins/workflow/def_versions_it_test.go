package workflow

import (
	"strings"
	"testing"
)

// 定义版本留档与回滚的真实库用例（MySQL IT；跳过条件与 handler_integration_test 相同）。
// 钉住三条：覆盖保存留旧版、历史 XML 可回看、回滚后版本继续递增且可再回滚。
// 版本历史表由本用例用 AutoMigrate 从模型自建（主 harness 只建 001/002 的表）。

func TestDefVersionArchiveAndRollbackIT(t *testing.T) {
	db := setupWorkflowDB(t)
	if err := db.AutoMigrate(&DefVersion{}); err != nil {
		t.Fatalf("建版本历史表失败: %v", err)
	}

	s := NewService(db, nopLogger{})
	// savePublished 走 tctx(42)（无租户），内存 store 的单测够用；真库带租户过滤器，
	// 必须用本包既有的 tctx 约定，否则 TenantModel 直接拒绝写入。
	def, err := s.DefSave(tctx(42), 0, DefForm{Key: "leave", Name: "请假审批", Status: DefPublished, XML: sampleXML})
	if err != nil {
		t.Fatalf("保存定义失败: %v", err)
	}

	xmlV1, err := s.DefXML(tctx(42), def.ID)
	if err != nil {
		t.Fatal(err)
	}

	// 修改内容再保存：当前版本递增为 2，旧版本 1 进历史表
	changed := strings.Replace(sampleXML, `name="主管审批"`, `name="主管审批（改）"`, 1)
	if _, err := s.DefSave(tctx(42), def.ID, DefForm{Key: def.Key, Name: def.Name, Status: DefPublished, XML: changed}); err != nil {
		t.Fatal(err)
	}

	versions, err := s.DefVersions(tctx(42), def.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].Version != 1 {
		t.Fatalf("历史版本 = %+v, want [v1]", versions)
	}
	gotV1, err := s.DefVersionXML(tctx(42), def.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if gotV1 != xmlV1 {
		t.Fatal("历史版本的 XML 必须是修改前的原文")
	}

	// 回滚到 v1：当前版本递增为 3（内容回到 v1），历史表变成两份
	out, err := s.DefRollback(tctx(42), def.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != 3 {
		t.Fatalf("回滚后 version = %d, want 3", out.Version)
	}
	xmlNow, _ := s.DefXML(tctx(42), def.ID)
	if xmlNow != xmlV1 {
		t.Fatal("回滚后当前 XML 应与 v1 一致")
	}
	if versions, _ := s.DefVersions(tctx(42), def.ID); len(versions) != 2 {
		t.Fatalf("历史版本数 = %d, want 2", len(versions))
	}
}
