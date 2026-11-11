package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type CacheConfig struct {
	Enabled      bool
	TTL, Timeout time.Duration
	MaxBytes     int
}

func loadCache() (CacheConfig, error) {
	c := CacheConfig{Enabled: true, TTL: 30 * time.Second, Timeout: 100 * time.Millisecond, MaxBytes: 1 << 20}
	if v, ok := os.LookupEnv("CACHE_ENABLED"); ok {
		if v != "true" && v != "false" {
			return CacheConfig{}, fmt.Errorf("CACHE_ENABLED must be true or false")
		}
		c.Enabled = v == "true"
	}
	for _, f := range []struct {
		name     string
		min, max int
		apply    func(int)
	}{
		{"CACHE_TTL_SECONDS", 1, 300, func(v int) { c.TTL = time.Duration(v) * time.Second }},
		{"REDIS_OPERATION_TIMEOUT_MS", 1, 1000, func(v int) { c.Timeout = time.Duration(v) * time.Millisecond }},
		{"CACHE_MAX_BYTES", 1024, 4 << 20, func(v int) { c.MaxBytes = v }},
	} {
		if v, ok := os.LookupEnv(f.name); ok {
			n, e := strconv.Atoi(v)
			if e != nil || n < f.min || n > f.max {
				return CacheConfig{}, fmt.Errorf("%s is outside its supported range", f.name)
			}
			f.apply(n)
		}
	}
	return c, nil
}
