package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
)

// ValidateObservabilityAddress accepts only literal loopback addresses. Empty disables diagnostics.
func ValidateObservabilityAddress(addr string) error {
	if addr == "" {
		return nil
	}
	host, port, err := net.SplitHostPort(addr)
	n, e := strconv.Atoi(port)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() || e != nil || n < 0 || n > 65535 || port == "" {
		return fmt.Errorf("diagnostics address must use a literal loopback IP and numeric port 0..65535")
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return fmt.Errorf("diagnostics address must use a numeric port 0..65535")
		}
	}
	return nil
}
func loadObservability(key string) (string, error) {
	addr := os.Getenv(key)
	if err := ValidateObservabilityAddress(addr); err != nil {
		return "", fmt.Errorf("%s: %w", key, err)
	}
	return addr, nil
}
