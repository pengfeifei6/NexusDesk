package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type AIRateConfig struct {
	Enabled  bool
	Requests int
	Window   time.Duration
}

func loadAIRate() (AIRateConfig, error) {
	c := AIRateConfig{Enabled: true, Requests: 10, Window: 60 * time.Second}
	if value, ok := os.LookupEnv("AI_RATE_LIMIT_ENABLED"); ok {
		if value != "true" && value != "false" {
			return AIRateConfig{}, fmt.Errorf("AI_RATE_LIMIT_ENABLED must be true or false")
		}
		c.Enabled = value == "true"
	}
	for _, field := range []struct {
		name  string
		max   int
		apply func(int)
	}{{"AI_RATE_LIMIT_REQUESTS", 10000, func(n int) { c.Requests = n }}, {"AI_RATE_LIMIT_WINDOW_SECONDS", 3600, func(n int) { c.Window = time.Duration(n) * time.Second }}} {
		if raw, ok := os.LookupEnv(field.name); ok {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > field.max {
				return AIRateConfig{}, fmt.Errorf("%s is outside its supported range", field.name)
			}
			field.apply(n)
		}
	}
	return c, nil
}
