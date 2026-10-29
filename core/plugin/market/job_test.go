package market

import "testing"

func TestRestartJobFailureIsTerminal(t *testing.T) {
	r := NewRunner()
	r.jobs["restart"] = &Job{ID: "restart", State: "running"}
	r.MarkRestarting("restart")
	r.finish("restart", "failed")
	job, ok := r.Get("restart")
	if !ok || job.State != "failed" {
		t.Fatalf("restart failure must not remain in waiting state: %+v", job)
	}
}

func TestRestartJobDoesNotCompleteBeforeNewProcess(t *testing.T) {
	r := NewRunner()
	r.jobs["restart"] = &Job{ID: "restart", State: "running"}
	r.MarkRestarting("restart")
	r.finish("restart", "done")
	job, _ := r.Get("restart")
	if job.State != "restarting" {
		t.Fatalf("scheduling restart does not prove the new process is ready: %+v", job)
	}
}
