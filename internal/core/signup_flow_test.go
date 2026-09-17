package core

import (
	"testing"
	"time"
)

func TestProfileOTPDwellWaitsForClaimedTimeOnPage(t *testing.T) {
	if got := profileOTPDwell(0, 5000); got != 5*time.Second {
		t.Fatalf("profileOTPDwell(0, 5000) = %s", got)
	}
	if got := profileOTPDwell(2000, 5000); got != 3*time.Second {
		t.Fatalf("profileOTPDwell(2000, 5000) = %s", got)
	}
	if got := profileOTPDwell(8000, 5000); got != 0 {
		t.Fatalf("profileOTPDwell(8000, 5000) = %s, want 0", got)
	}
}

func TestProfileOTPTimeOnPageUsesElapsed(t *testing.T) {
	if got := profileOTPTimeOnPage(6123, 5000); got != 6123 {
		t.Fatalf("profileOTPTimeOnPage() = %d, want elapsed", got)
	}
	if got := profileOTPTimeOnPage(0, 5000); got != 5000 {
		t.Fatalf("profileOTPTimeOnPage() = %d, want target", got)
	}
}
