package browsersignup

import "testing"

func TestIsRunningDefault(t *testing.T) {
	if IsRunning() {
		t.Fatal("manager should start idle")
	}
}

func TestStartRejectsSecondBatch(t *testing.T) {
	mgr.mu.Lock()
	mgr.running = true
	mgr.mu.Unlock()
	defer func() {
		mgr.mu.Lock()
		mgr.running = false
		mgr.mu.Unlock()
	}()
	got := Start(StartRequest{Count: 1, Engine: "camoufox"})
	if got["error"] == nil {
		t.Fatal("expected error while running")
	}
}
