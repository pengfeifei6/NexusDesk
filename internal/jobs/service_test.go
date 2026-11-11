package jobs

import (
	"context"
	"testing"
	"time"
)

func TestMalformedPayloadNeverExecutesDatabaseOrModel(t *testing.T) {
	s := NewService(nil, nil, nil, Limits{Enabled: true})
	for _, p := range []Payload{{}, {JobID: 1, OrgID: 1}, {JobID: -1, OrgID: 1, RunVersion: 1}} {
		if e := s.Execute(context.Background(), p); Code(e) != "INVALID_REQUEST" {
			t.Fatalf("malformed payload reached execution: %v", e)
		}
	}
}
func TestLimitsKeepBoundedDefaultsAndAllowShortTestLeases(t *testing.T) {
	l := (Limits{Enabled: true}).normalized()
	if l.LeaseDuration != 30*time.Second || l.HeartbeatInterval != 5*time.Second || l.TaskTimeout != 180*time.Second || l.DispatchInterval != 2*time.Second || !l.Enabled {
		t.Fatal(l)
	}
	tiny := Limits{LeaseDuration: time.Second, HeartbeatInterval: time.Millisecond, TaskTimeout: 2 * time.Second, DispatchInterval: time.Millisecond}
	if tiny.normalized() != tiny {
		t.Fatal("injected timing changed")
	}
}

func TestCanceledDeliveryNeverStartsDatabaseOrModel(t *testing.T) {
	s := NewService(nil, nil, nil, Limits{Enabled: true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := s.Execute(ctx, Payload{JobID: 1, OrgID: 1, RunVersion: 1}); Code(e) != "DELIVERY_RETRY" {
		t.Fatalf("canceled delivery: %v", e)
	}
}
