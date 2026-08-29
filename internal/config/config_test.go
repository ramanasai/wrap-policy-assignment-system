package config

import (
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() with clean env = %v", err)
	}
	if cfg.AppEnv != EnvDev {
		t.Errorf("AppEnv = %q, want dev", cfg.AppEnv)
	}
	if cfg.HTTPPort != DefaultHTTPPort {
		t.Errorf("HTTPPort = %d, want %d", cfg.HTTPPort, DefaultHTTPPort)
	}
	if cfg.Log.Level != DefaultLogLevel || cfg.Log.Format != DefaultLogFormat {
		t.Errorf("Log = %+v, want level %q format %q", cfg.Log, DefaultLogLevel, DefaultLogFormat)
	}
	if cfg.ReconcilerBatchSize != DefaultReconcilerBatch {
		t.Errorf("ReconcilerBatchSize = %d, want %d", cfg.ReconcilerBatchSize, DefaultReconcilerBatch)
	}
	if cfg.SweeperInterval != DefaultSweeperInterval {
		t.Errorf("SweeperInterval = %v, want %v", cfg.SweeperInterval, DefaultSweeperInterval)
	}
	if cfg.ResolverCacheSize != DefaultResolverCache {
		t.Errorf("ResolverCacheSize = %d, want %d", cfg.ResolverCacheSize, DefaultResolverCache)
	}
}

func TestLoad_ValidOverrides(t *testing.T) {
	t.Setenv(EnvAppEnv, EnvStaging)
	t.Setenv(EnvHTTPPort, "9090")
	t.Setenv(EnvDatabaseURL, "postgres://prod-host/db")
	t.Setenv("LOG_LEVEL", "warn")
	t.Setenv("LOG_FORMAT", "console")
	t.Setenv(EnvReconcilerBatch, "1000")
	t.Setenv(EnvSweeperInterval, "1h")
	t.Setenv(EnvResolverCache, "50000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.AppEnv != EnvStaging || cfg.HTTPPort != 9090 {
		t.Errorf("AppEnv/Port = %q/%d", cfg.AppEnv, cfg.HTTPPort)
	}
	if cfg.DatabaseURL != "postgres://prod-host/db" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.Log.Level != "warn" || cfg.Log.Format != "console" {
		t.Errorf("Log = %+v", cfg.Log)
	}
	if cfg.ReconcilerBatchSize != 1000 || cfg.SweeperInterval != time.Hour || cfg.ResolverCacheSize != 50000 {
		t.Errorf("worker config wrong: %+v", cfg)
	}
}

func TestLoad_InvalidValuesFailFast(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantSub string
	}{
		{
			name:    "bad APP_ENV",
			env:     map[string]string{EnvAppEnv: "chaos"},
			wantSub: "APP_ENV",
		},
		{
			name:    "port too low",
			env:     map[string]string{EnvHTTPPort: "0"},
			wantSub: "HTTP_PORT",
		},
		{
			name:    "port too high",
			env:     map[string]string{EnvHTTPPort: "70000"},
			wantSub: "HTTP_PORT",
		},
		{
			name:    "port not a number",
			env:     map[string]string{EnvHTTPPort: "eighty"},
			wantSub: "HTTP_PORT",
		},
		{
			name:    "bad log level fails fast at startup",
			env:     map[string]string{"LOG_LEVEL": "loud"},
			wantSub: "LOG_LEVEL",
		},
		{
			name:    "bad log format",
			env:     map[string]string{"LOG_FORMAT": "xml"},
			wantSub: "LOG_FORMAT",
		},
		{
			name:    "zero batch size",
			env:     map[string]string{EnvReconcilerBatch: "0"},
			wantSub: "RECONCILER_BATCH_SIZE",
		},
		{
			name:    "negative sweeper interval",
			env:     map[string]string{EnvSweeperInterval: "-5m"},
			wantSub: "SWEEPER_INTERVAL",
		},
		{
			name:    "bad duration",
			env:     map[string]string{EnvSweeperInterval: "soon"},
			wantSub: "SWEEPER_INTERVAL",
		},
		{
			name:    "negative cache size",
			env:     map[string]string{EnvResolverCache: "-1"},
			wantSub: "RESOLVER_CACHE_SIZE",
		},
		{
			name:    "prod without DATABASE_URL",
			env:     map[string]string{EnvAppEnv: EnvProd, EnvDatabaseURL: ""},
			wantSub: "DATABASE_URL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			if err == nil {
				t.Fatalf("Load() error = nil, want containing %q", tt.wantSub)
			}
			if !contains(err.Error(), tt.wantSub) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantSub)
			}
		})
	}
}

func TestLoad_ProdRequiresRealDatabaseURL(t *testing.T) {
	t.Setenv(EnvAppEnv, EnvProd)
	t.Setenv(EnvDatabaseURL, "postgres://real-host/db?sslmode=require")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("prod with real DATABASE_URL must load, got %v", err)
	}
	if cfg.AppEnv != EnvProd {
		t.Errorf("AppEnv = %q", cfg.AppEnv)
	}
}

func TestLoadOrDefault_NeverErrors(t *testing.T) {
	// Invalid config → silent defaults (test convenience only).
	t.Setenv(EnvHTTPPort, "not-a-port")
	cfg := LoadOrDefault()
	if cfg.HTTPPort != DefaultHTTPPort {
		t.Errorf("LoadOrDefault HTTPPort = %d, want default %d", cfg.HTTPPort, DefaultHTTPPort)
	}
}

func TestMustLoad_PanicsOnInvalid(t *testing.T) {
	t.Setenv(EnvHTTPPort, "not-a-port")
	defer func() {
		if recover() == nil {
			t.Fatal("MustLoad should panic on invalid config")
		}
	}()
	MustLoad()
}

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
