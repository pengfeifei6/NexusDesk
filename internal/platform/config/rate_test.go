package config

import (
	"os"
	"testing"
	"time"
)

func TestAIRateConfiguration(t *testing.T) {
	for _, k := range []string{"AI_RATE_LIMIT_ENABLED", "AI_RATE_LIMIT_REQUESTS", "AI_RATE_LIMIT_WINDOW_SECONDS"} {
		t.Setenv(k, "")
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := loadAIRate()
	if err != nil || !cfg.Enabled || cfg.Requests != 10 || cfg.Window != time.Minute {
		t.Fatalf("default rate configuration: %+v %v", cfg, err)
	}
	t.Setenv("AI_RATE_LIMIT_ENABLED", "false")
	t.Setenv("AI_RATE_LIMIT_REQUESTS", "1")
	t.Setenv("AI_RATE_LIMIT_WINDOW_SECONDS", "2")
	cfg, err = loadAIRate()
	if err != nil || cfg.Enabled || cfg.Requests != 1 || cfg.Window != 2*time.Second {
		t.Fatalf("configured rate: %+v %v", cfg, err)
	}
	for _, tc := range []struct{ k, v string }{{"AI_RATE_LIMIT_ENABLED", "1"}, {"AI_RATE_LIMIT_REQUESTS", "0"}, {"AI_RATE_LIMIT_REQUESTS", "10001"}, {"AI_RATE_LIMIT_WINDOW_SECONDS", "0"}, {"AI_RATE_LIMIT_WINDOW_SECONDS", "3601"}} {
		t.Run(tc.k+tc.v, func(t *testing.T) {
			t.Setenv(tc.k, tc.v)
			if _, err := loadAIRate(); err == nil {
				t.Fatal("invalid rate accepted")
			}
		})
	}
}
