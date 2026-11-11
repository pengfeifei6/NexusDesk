package main

import (
	"bytes"
	"context"
	"errors"
	"github.com/pengfeifei6/NexusDesk/internal/jobs"
	"github.com/pengfeifei6/NexusDesk/internal/platform/observe"
	"log/slog"
	"strings"
	"testing"
)

func TestObservedDeliveryPreservesReturnsAndUsesIndependentIdentity(t *testing.T) {
	for _, wantErr := range []error{nil, errors.New("private-error")} {
		var logs, metrics bytes.Buffer
		r := observe.NewRegistry()
		logger := slog.New(slog.NewJSONHandler(&logs, nil))
		parent := observe.WithRequestID(context.Background())
		var deliveryID string
		execute := observedDelivery(r, logger, func(ctx context.Context, p jobs.Payload) error {
			deliveryID = observe.RequestID(ctx)
			if p.JobID != 42 || p.OrgID != 99 || p.RunVersion != 3 {
				t.Fatal(p)
			}
			return wantErr
		})
		err := execute(parent, jobs.Payload{JobID: 42, OrgID: 99, RunVersion: 3})
		if err != wantErr {
			t.Fatal("return changed")
		}
		r.WritePrometheus(&metrics)
		if len(deliveryID) != 32 || deliveryID == observe.RequestID(parent) || strings.Contains(logs.String(), "private-error") || strings.Contains(logs.String(), "org_id") || strings.Contains(logs.String(), "SUCCEEDED") || !strings.Contains(logs.String(), deliveryID) || !strings.Contains(logs.String(), `"job_id":42`) || !strings.Contains(logs.String(), `"run_version":3`) {
			t.Fatal(logs.String())
		}
		result := "nil"
		if wantErr != nil {
			result = "error"
		}
		if !strings.Contains(metrics.String(), `nexusdesk_worker_returns_total{phase="delivery_return",result="`+result+`"} 1`) {
			t.Fatal(metrics.String())
		}
	}
}

func TestPanickedDeliveryNeverCountsANilReturn(t *testing.T) {
	var logs, metrics bytes.Buffer
	r := observe.NewRegistry()
	execute := observedDelivery(r, slog.New(slog.NewJSONHandler(&logs, nil)), func(context.Context, jobs.Payload) error { panic("private-panic") })
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("delivery panic swallowed")
			}
		}()
		execute(context.Background(), jobs.Payload{JobID: 1, OrgID: 2, RunVersion: 1})
	}()
	r.WritePrometheus(&metrics)
	if strings.Contains(metrics.String(), `result="nil"}`) || logs.Len() != 0 {
		t.Fatal("panic counted as a returned nil", metrics.String(), logs.String())
	}
}
