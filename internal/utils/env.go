// Package utils holds small, dependency-light shared helpers.
//
// Scope discipline (AGENTS.md): utilities here must be generic enough to be
// useful across internal packages (config, logging, api, workers) but never
// contain domain logic — that belongs to the resolver or the services.
package utils

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// ---------------------------------------------------------------------------
// .env loading
// ---------------------------------------------------------------------------

// LoadDotEnv loads key=value pairs from .env into the process environment
// without overwriting variables that are already set (host env wins — that is
// what makes it safe in containers). A missing .env file is NOT an error:
// in production, configuration comes from the real environment. Any other
// read/parse failure is surfaced.
func LoadDotEnv(paths ...string) error {
	err := godotenv.Load(paths...)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}

// ---------------------------------------------------------------------------
// Typed env getters
//
// Contract:
//   - unset  → default, no error
//   - set and valid → parsed value, no error
//   - set and INVALID → error naming the variable (fail fast at startup —
//     a typo'd value must never silently become the default)
// ---------------------------------------------------------------------------

func GetString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// GetInt parses an integer env var. Valid range is enforced by the caller
// via Validate-style checks when domain bounds exist (e.g. port 1–65535).
func GetInt(key string, def int) (int, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q", key, raw)
	}
	return n, nil
}

// GetDuration parses a Go duration string ("30s", "5m", "1h").
func GetDuration(key string, def time.Duration) (time.Duration, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q", key, raw)
	}
	return d, nil
}

// GetBool accepts "true"/"false"/"1"/"0"/"t"/"f"/"yes"/"no" (case-insensitive).
func GetBool(key string, def bool) (bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	switch strings.ToLower(raw) {
	case "true", "1", "t", "yes":
		return true, nil
	case "false", "0", "f", "no":
		return false, nil
	default:
		return false, fmt.Errorf("%s: invalid boolean %q", key, raw)
	}
}

// Require returns the env value, erroring when absent — for settings that
// have no sensible default (e.g. DATABASE_URL outside dev).
func Require(key string) (string, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return "", fmt.Errorf("%s: required but not set", key)
	}
	return v, nil
}
