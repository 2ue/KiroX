package browsersignup

import "testing"

func TestParsePlaywrightProxy(t *testing.T) {
	p := parsePlaywrightProxy("http://user:pass@10.0.0.1:8080")
	if p == nil {
		t.Fatal("expected proxy")
	}
	if p.Server != "http://10.0.0.1:8080" {
		t.Fatalf("server=%s", p.Server)
	}
	if p.Username != "user" || p.Password != "pass" {
		t.Fatalf("auth=%s:%s", p.Username, p.Password)
	}
	if parsePlaywrightProxy("") != nil {
		t.Fatal("empty should be nil")
	}
	if parsePlaywrightProxy("not a url") != nil {
		t.Fatal("invalid should be nil")
	}
}

func TestNormalizeEngine(t *testing.T) {
	if normalizeEngine("Playwright") != "playwright" {
		t.Fatal("playwright")
	}
	if normalizeEngine("chromium") != "playwright" {
		t.Fatal("chromium alias")
	}
	if normalizeEngine("") != "camoufox" {
		t.Fatal("default camoufox")
	}
}

func TestWorkerEmbedded(t *testing.T) {
	if len(workerPy) < 100 {
		t.Fatal("worker.py not embedded")
	}
}
