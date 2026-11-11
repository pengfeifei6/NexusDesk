package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestCacheConfigurationBounds(t *testing.T) {
	for _, k := range []string{"CACHE_ENABLED", "CACHE_TTL_SECONDS", "REDIS_OPERATION_TIMEOUT_MS", "CACHE_MAX_BYTES"} {
		t.Setenv(k, "")
		if e := os.Unsetenv(k); e != nil {
			t.Fatal(e)
		}
	}
	c, e := loadCache()
	if e != nil || !c.Enabled || c.TTL != 30*time.Second || c.Timeout != 100*time.Millisecond || c.MaxBytes != 1<<20 {
		t.Fatalf("defaults: %+v %v", c, e)
	}
	for _, tc := range []struct{ k, v string }{{"CACHE_ENABLED", "1"}, {"CACHE_TTL_SECONDS", "0"}, {"CACHE_TTL_SECONDS", "301"}, {"REDIS_OPERATION_TIMEOUT_MS", "0"}, {"REDIS_OPERATION_TIMEOUT_MS", "1001"}, {"CACHE_MAX_BYTES", "1023"}, {"CACHE_MAX_BYTES", "4194305"}} {
		t.Run(tc.k+tc.v, func(t *testing.T) {
			t.Setenv(tc.k, tc.v)
			_, e := loadCache()
			if e == nil || !strings.Contains(e.Error(), tc.k) {
				t.Fatal(e)
			}
		})
	}
}
