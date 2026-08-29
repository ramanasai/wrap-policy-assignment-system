// Package config loads typed runtime configuration from the process
// environment (optionally seeded from a .env file via utils.LoadDotEnv).
//
// Contract:
//   - unset vars → documented defaults
//   - set but invalid → Load returns an error naming the variable
//     (fail fast at startup; never silently default a typo'd value)
//   - prod requires DATABASE_URL; dev falls back to the local default
package config

import (
	"fmt"
	"time"

	"github.com/ramanasai/wrap-policy-assignment-system/internal/logging"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/utils"
)

// AppEnv values.
const (
	EnvDev     = "dev"
	EnvStaging = "staging"
	EnvProd    = "prod"
)

// Env var names.
const (
	EnvAppEnv          = "APP_ENV"
	EnvHTTPPort        = "HTTP_PORT"
	EnvDatabaseURL     = "DATABASE_URL"
	EnvReconcilerBatch = "RECONCILER_BATCH_SIZE"
	EnvSweeperInterval = "SWEEPER_INTERVAL"
	EnvResolverCache   = "RESOLVER_CACHE_SIZE"
	EnvSeedEmployees   = "SEED_EMPLOYEES"
)

// Config is the fully-typed runtime configuration.
type Config struct {
	AppEnv string

	HTTPPort    int
	DatabaseURL string

	Log LogConfig

	ReconcilerBatchSize int
	SweeperInterval     time.Duration
	ResolverCacheSize   int
	SeedEmployees       int
}

// LogConfig carries logging setup (validated at Load time).
type LogConfig struct {
	Level  string
	Format string
}

// Defaults — also documented in .env.example.
const (
	DefaultAppEnv          = EnvDev
	DefaultHTTPPort        = 8080
	DefaultDatabaseURL     = "postgres://postgres:postgres@localhost:5432/pas?sslmode=disable"
	DefaultLogLevel        = "info"
	DefaultLogFormat       = logging.FormatJSON
	DefaultReconcilerBatch = 500
	DefaultSweeperInterval = 15 * time.Minute
	DefaultResolverCache   = 10_000
	DefaultSeedEmployees   = 1000
)

// Load reads and validates configuration from the environment.
func Load() (Config, error) {
	cfg := Config{
		AppEnv:              utils.GetString(EnvAppEnv, DefaultAppEnv),
		HTTPPort:            DefaultHTTPPort,
		DatabaseURL:         utils.GetString(EnvDatabaseURL, DefaultDatabaseURL),
		Log:                 LogConfig{Level: utils.GetString(logging.EnvLogLevel, DefaultLogLevel), Format: utils.GetString(logging.EnvLogFormat, DefaultLogFormat)},
		ReconcilerBatchSize: DefaultReconcilerBatch,
		SweeperInterval:     DefaultSweeperInterval,
		ResolverCacheSize:   DefaultResolverCache,
	}

	// Integers & durations: parse errors surface with the env var name.
	var err error
	if cfg.HTTPPort, err = utils.GetInt(EnvHTTPPort, DefaultHTTPPort); err != nil {
		return Config{}, err
	}
	if cfg.ReconcilerBatchSize, err = utils.GetInt(EnvReconcilerBatch, DefaultReconcilerBatch); err != nil {
		return Config{}, err
	}
	if cfg.ResolverCacheSize, err = utils.GetInt(EnvResolverCache, DefaultResolverCache); err != nil {
		return Config{}, err
	}
	if cfg.SeedEmployees, err = utils.GetInt(EnvSeedEmployees, DefaultSeedEmployees); err != nil {
		return Config{}, err
	}
	if cfg.SweeperInterval, err = utils.GetDuration(EnvSweeperInterval, DefaultSweeperInterval); err != nil {
		return Config{}, err
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// MustLoad returns config or panics — for entrypoints where startup without
// valid configuration is meaningless.
func MustLoad() Config {
	cfg, err := Load()
	if err != nil {
		panic(fmt.Sprintf("config: %v", err))
	}
	return cfg
}

func (c Config) validate() error {
	switch c.AppEnv {
	case EnvDev, EnvStaging, EnvProd:
	default:
		return fmt.Errorf("%s: invalid APP_ENV %q (want dev|staging|prod)", EnvAppEnv, c.AppEnv)
	}

	if c.HTTPPort < 1 || c.HTTPPort > 65535 {
		return fmt.Errorf("%s: port %d out of range 1-65535", EnvHTTPPort, c.HTTPPort)
	}

	if c.AppEnv == EnvProd && c.DatabaseURL == DefaultDatabaseURL {
		return fmt.Errorf("%s: required in prod (no database configured)", EnvDatabaseURL)
	}

	if !logging.IsValidLevel(c.Log.Level) {
		return fmt.Errorf("%s: invalid level %q", logging.EnvLogLevel, c.Log.Level)
	}
	if !logging.IsValidFormat(c.Log.Format) {
		return fmt.Errorf("%s: invalid format %q (want json|console)", logging.EnvLogFormat, c.Log.Format)
	}

	if c.ReconcilerBatchSize < 1 {
		return fmt.Errorf("%s: must be >= 1, got %d", EnvReconcilerBatch, c.ReconcilerBatchSize)
	}
	if c.SweeperInterval <= 0 {
		return fmt.Errorf("%s: must be positive, got %s", EnvSweeperInterval, c.SweeperInterval)
	}
	if c.ResolverCacheSize < 0 {
		return fmt.Errorf("%s: must be >= 0, got %d", EnvResolverCache, c.ResolverCacheSize)
	}
	if c.SeedEmployees < 1 {
		return fmt.Errorf("%s: must be >= 1, got %d", EnvSeedEmployees, c.SeedEmployees)
	}
	return nil
}

// LoadOrDefault is the test/dev convenience: never errors, falls back to
// defaults for any invalid value. Use in tests; entrypoints use Load/MustLoad.
func LoadOrDefault() Config {
	cfg, err := Load()
	if err != nil {
		return defaultsOnly()
	}
	return cfg
}

func defaultsOnly() Config {
	return Config{
		AppEnv:              DefaultAppEnv,
		HTTPPort:            DefaultHTTPPort,
		DatabaseURL:         DefaultDatabaseURL,
		Log:                 LogConfig{Level: DefaultLogLevel, Format: DefaultLogFormat},
		ReconcilerBatchSize: DefaultReconcilerBatch,
		SweeperInterval:     DefaultSweeperInterval,
		ResolverCacheSize:   DefaultResolverCache,
		SeedEmployees:       DefaultSeedEmployees,
	}
}
