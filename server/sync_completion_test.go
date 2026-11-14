package main

import (
	"errors"
	"testing"
)

func TestSyncCompletionReplacesStaleErrorAndKeepsLastSuccessOnFailure(t *testing.T) {
	a, _ := movieBotTestApp(t)
	a.updateSettings(map[string]string{"last_sync_error": "legacy HTTP 503", "last_sync_success_at": "previous"})
	a.setSync(func(s *syncState) { s.Running = true; s.Error = "legacy HTTP 503"; s.Total = 414; s.Done = 414 })
	a.finishSync(nil)
	state := a.syncSnapshot()
	saved := a.settings()
	if state.Running || state.Error != "" || state.Stage != "同步完成" || state.FinishedAt == "" || saved["last_sync_error"] != "" || saved["last_sync_success_at"] == "previous" {
		t.Fatal("success retained stale failure")
	}
	success := saved["last_sync_success_at"]
	a.finishSync(errors.New("current connection failure"))
	state = a.syncSnapshot()
	saved = a.settings()
	if state.Running || state.Stage != "同步失败" || state.Error != "current connection failure" || saved["last_sync_error"] != state.Error || saved["last_sync_success_at"] != success {
		t.Fatal("failure hidden or last success overwritten")
	}
}
