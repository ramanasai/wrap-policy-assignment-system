// Package logging provides the project-wide zerolog setup.
//
// Conventions (AGENTS.md):
//   - Feature packages NEVER import zerolog directly; they receive a
//     *zerolog.Logger from here (constructor-injected).
//   - The resolver package stays pure: no logging at all (invariant #2).
//     Logging happens around the resolver, not inside it.
//   - Structured JSON by default; pretty console output for local dev via
//     LOG_FORMAT=console.
package logging

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Well-known component names so dashboards can filter consistently.
const (
	ComponentAPI        = "api"
	ComponentReconciler = "reconciler"
	ComponentScheduler  = "scheduler"
	ComponentSweeper    = "sweeper"
	ComponentMigrations = "migrations"
	ComponentSeed       = "seed"
)

// Env vars consulted by SetupFromEnv. Kept here so every entrypoint agrees.
const (
	EnvLogLevel   = "LOG_LEVEL"
	EnvLogFormat  = "LOG_FORMAT"
	FormatJSON    = "json"
	FormatConsole = "console"
)

// Options configures a logger. Level and Format are raw strings (as they
// arrive from config/env); validation is via ParseLevel/ValidFormat.
type Options struct {
	Level  string    // "debug".."disabled"; "" = info
	Format string    // "json" (default) | "console"
	Out    io.Writer // default os.Stderr
}

// Build constructs a logger from options WITHOUT touching global state —
// safe for tests and for running multiple logger variants in one process.
func Build(opts Options) zerolog.Logger {
	out := opts.Out
	if out == nil {
		out = os.Stderr
	}
	if opts.Format == FormatConsole {
		out = zerolog.ConsoleWriter{
			Out:        out,
			TimeFormat: time.RFC3339,
		}
	}
	return zerolog.New(out).
		Level(ParseLevel(opts.Level)).
		With().Timestamp().Logger()
}

func init() {
	// Process-wide field naming — set once here so every logger built by
	// this package (and its tests) emits the same schema.
	zerolog.TimestampFieldName = "ts"
	zerolog.TimeFieldFormat = time.RFC3339
}

// SetupFromEnv configures process-global zerolog defaults from LOG_LEVEL /
// LOG_FORMAT and returns the root logger. Call once from each entrypoint's
// main(); use Build directly in tests.
func SetupFromEnv() zerolog.Logger {
	zerolog.SetGlobalLevel(ParseLevel(os.Getenv(EnvLogLevel)))
	return Build(Options{Level: os.Getenv(EnvLogLevel), Format: os.Getenv(EnvLogFormat)})
}

// New returns a logger tagged with a component name. Every log line in the
// process is attributable to exactly one component.
func New(root zerolog.Logger, component string) zerolog.Logger {
	return root.With().Str("component", component).Logger()
}

// WithRequestID returns a request-scoped child logger. Request IDs survive
// across goroutine handoffs because the logger value is carried in context,
// not global state.
func WithRequestID(l zerolog.Logger, requestID string) zerolog.Logger {
	return l.With().Str("request_id", requestID).Logger()
}

// ParseLevel maps level strings to zerolog levels. Unknown or empty values
// default to info — a typo must never crash a service. Strict startup
// validation is available via IsValidLevel for config-time fail-fast.
func ParseLevel(s string) zerolog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return zerolog.DebugLevel
	case "info", "":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	case "panic":
		return zerolog.PanicLevel
	case "disabled":
		return zerolog.Disabled
	default:
		return zerolog.InfoLevel
	}
}

// IsValidLevel reports whether s is an accepted LOG_LEVEL value.
// "" counts as valid (means "default").
func IsValidLevel(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug", "info", "warn", "warning", "error", "fatal", "panic", "disabled", "":
		return true
	default:
		return false
	}
}

// IsValidFormat reports whether s is an accepted LOG_FORMAT value.
// "" counts as valid (means "json").
func IsValidFormat(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case FormatJSON, FormatConsole, "":
		return true
	default:
		return false
	}
}

// Nop returns a logger that discards everything — for code paths whose
// logging output is irrelevant to the test at hand.
func Nop() zerolog.Logger {
	return zerolog.Nop()
}

// NewTest returns a debug-level logger writing to w (typically a
// bytes.Buffer) so tests can assert on emitted structured fields.
func NewTest(w io.Writer) zerolog.Logger {
	return Build(Options{Level: "debug", Out: w})
}
