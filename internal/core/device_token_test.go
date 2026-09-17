package core

import (
	"context"
	"testing"
	"time"
)

func TestPollDeviceTokenRespectsCancel(t *testing.T) {
	cfg := NewConfig()
	reg := NewRegistrar(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	reg.Ctx = ctx
	cancel()
	_, err := reg.PollDeviceToken(2 * time.Second)
	if err == nil {
		t.Fatal("expected cancel error")
	}
}
