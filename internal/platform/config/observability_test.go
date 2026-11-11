package config

import "testing"

func TestObservabilityAddressValidation(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{{"", true}, {"127.0.0.1:0", true}, {"[::1]:6061", true}, {"localhost:6060", false}, {":6060", false}, {"0.0.0.0:6060", false}, {"127.0.0.1:65536", false}, {"127.0.0.1:+1", false}, {" 127.0.0.1:6060", false}} {
		t.Setenv("OBSERVABILITY_ADDR", tc.value)
		_, err := loadObservability("OBSERVABILITY_ADDR")
		if (err == nil) != tc.valid {
			t.Errorf("%q: %v", tc.value, err)
		}
	}
}
