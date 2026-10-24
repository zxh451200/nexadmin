package smscode

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"nexadmin/core/notify"
)

type recSink struct {
	mu   sync.Mutex
	recs []notify.Record
}

func (r *recSink) Record(_ context.Context, rec notify.Record) {
	r.mu.Lock()
	r.recs = append(r.recs, rec)
	r.mu.Unlock()
}

func TestSendRecordsResultWithoutCode(t *testing.T) {
	s, prov, _ := newSvc()
	sink := &recSink{}
	s.SetRecorder(sink)

	if err := s.Send(context.Background(), phone, "1.1.1.1", true); err != nil {
		t.Fatal(err)
	}
	if len(sink.recs) != 1 {
		t.Fatalf("records = %+v", sink.recs)
	}
	r := sink.recs[0]
	if r.Channel != notify.RecordSMS || r.Scene != notify.SceneVerifyCode || r.Status != notify.StatusSuccess ||
		r.Recipient != phone || r.Subject != "login" {
		t.Fatalf("record = %+v", r)
	}
	if strings.Contains(r.Recipient+r.Subject+r.Err+r.Scene, prov.code) {
		t.Fatal("verification code leaked into the record")
	}
}

func TestSendRecordsSceneAndFailureWithoutCodeOrFullPhone(t *testing.T) {
	s, prov, _ := newSvc()
	sink := &recSink{}
	s.SetRecorder(sink)
	// 服务商把请求内容回显进了错误：验证码和完整手机号都不能原样进入记录。
	prov.err = errors.New("provider echo: phone=" + phone + " rejected")
	ctx := WithScene(context.Background(), notify.SceneBindPhone)
	if err := s.Send(ctx, phone, "1.1.1.1", true); err == nil {
		t.Fatal("expected failure")
	}
	r := sink.recs[0]
	if r.Scene != notify.SceneBindPhone || r.Status != notify.StatusFail {
		t.Fatalf("record = %+v", r)
	}
	if strings.Contains(r.Err, phone) || strings.Contains(r.Err, prov.code) {
		t.Fatalf("phone or code leaked: %q", r.Err)
	}
}

func TestSendWithoutRecorderAndFakeDeliveryAreNotRecorded(t *testing.T) {
	s, prov, _ := newSvc() // 没有注入 Recorder：空操作
	if err := s.Send(context.Background(), phone, "1.1.1.1", true); err != nil || prov.n != 1 {
		t.Fatalf("send without recorder: err=%v n=%d", err, prov.n)
	}
	s2, prov2, _ := newSvc()
	sink := &recSink{}
	s2.SetRecorder(sink)
	if err := s2.Send(context.Background(), phone, "1.1.1.1", false); err != nil || prov2.n != 0 || len(sink.recs) != 0 {
		t.Fatal("a fake (deliver=false) send is not a delivery and must not be recorded")
	}
	var nilSvc *Service
	nilSvc.SetRecorder(sink) // nil 安全
}
