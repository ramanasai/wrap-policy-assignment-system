package utils

import (
	"os"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Env getters
//
// Contract under test:
//   unset → default, nil error
//   set + valid → parsed value, nil error
//   set + invalid → named error (never a silent default)
//   set but empty → treated as unset (documented convenience)
// ---------------------------------------------------------------------------

func TestGetString(t *testing.T) {
	t.Setenv("TEST_STR", "hello")
	if got := GetString("TEST_STR", "def"); got != "hello" {
		t.Errorf("GetString set = %q, want hello", got)
	}
	if got := GetString("TEST_STR_UNSET", "def"); got != "def" {
		t.Errorf("GetString unset = %q, want def", got)
	}
	t.Setenv("TEST_STR_EMPTY", "")
	if got := GetString("TEST_STR_EMPTY", "def"); got != "def" {
		t.Errorf("GetString empty counts as unset, got %q", got)
	}
}

func TestGetInt(t *testing.T) {
	tests := []struct {
		name    string
		value   string // "" = unset/empty
		def     int
		want    int
		wantErr bool
	}{
		{name: "unset returns default", value: "", def: 7, want: 7},
		{name: "valid positive", value: "42", def: 7, want: 42},
		{name: "valid negative", value: "-3", def: 7, want: -3},
		{name: "valid zero", value: "0", def: 7, want: 0},
		{name: "garbage errors", value: "many", def: 7, wantErr: true},
		{name: "float errors", value: "3.5", def: 7, wantErr: true},
		{name: "whitespace is invalid (strict)", value: " 5", def: 7, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TEST_INT", tt.value)
			got, err := GetInt("TEST_INT", tt.def)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetInt() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("GetInt() = %d, want %d", got, tt.want)
			}
			if err != nil && !contains(err.Error(), "TEST_INT") {
				t.Errorf("error %q must name the env var", err.Error())
			}
		})
	}
}

func TestGetDuration(t *testing.T) {
	t.Setenv("TEST_DUR", "90s")
	got, err := GetDuration("TEST_DUR", time.Minute)
	if err != nil || got != 90*time.Second {
		t.Fatalf("GetDuration() = (%v, %v), want (90s, nil)", got, err)
	}

	t.Setenv("TEST_DUR", "soon")
	_, err = GetDuration("TEST_DUR", time.Minute)
	if err == nil || !contains(err.Error(), "TEST_DUR") {
		t.Fatalf("GetDuration(invalid) error = %v, want named error", err)
	}

	t.Setenv("TEST_DUR", "")
	got, err = GetDuration("TEST_DUR", time.Minute)
	if err != nil || got != time.Minute {
		t.Fatalf("GetDuration(empty) = (%v, %v), want (1m, nil)", got, err)
	}
}

func TestGetBool(t *testing.T) {
	truthy := []string{"true", "TRUE", "True", "1", "t", "yes", "YES"}
	for _, v := range truthy {
		t.Setenv("TEST_BOOL", v)
		got, err := GetBool("TEST_BOOL", false)
		if err != nil || got != true {
			t.Errorf("GetBool(%q) = (%v, %v), want (true, nil)", v, got, err)
		}
	}
	falsy := []string{"false", "0", "f", "no", "NO"}
	for _, v := range falsy {
		t.Setenv("TEST_BOOL", v)
		got, err := GetBool("TEST_BOOL", true)
		if err != nil || got != false {
			t.Errorf("GetBool(%q) = (%v, %v), want (false, nil)", v, got, err)
		}
	}
	t.Setenv("TEST_BOOL", "maybe")
	if _, err := GetBool("TEST_BOOL", true); err == nil {
		t.Error("GetBool(maybe) should error")
	}
	t.Setenv("TEST_BOOL", "")
	got, err := GetBool("TEST_BOOL", true)
	if err != nil || got != true {
		t.Errorf("GetBool(empty) = (%v, %v), want (true, nil)", got, err)
	}
}

func TestRequire(t *testing.T) {
	t.Setenv("TEST_REQ", "present")
	v, err := Require("TEST_REQ")
	if err != nil || v != "present" {
		t.Fatalf("Require() = (%q, %v), want (present, nil)", v, err)
	}
	t.Setenv("TEST_REQ", "")
	_, err = Require("TEST_REQ")
	if err == nil || !contains(err.Error(), "required") {
		t.Fatalf("Require(missing) error = %v, want 'required' error", err)
	}
}

// ---------------------------------------------------------------------------
// .env loading
// ---------------------------------------------------------------------------

func TestLoadDotEnv(t *testing.T) {
	t.Run("loads key=value pairs", func(t *testing.T) {
		path := writeTemp(t, "TEST_DOTENV_A=hello\nTEST_DOTENV_B=42\n")
		if err := LoadDotEnv(path); err != nil {
			t.Fatalf("LoadDotEnv() = %v", err)
		}
		if got := GetString("TEST_DOTENV_A", ""); got != "hello" {
			t.Errorf("A = %q, want hello", got)
		}
	})

	t.Run("host env wins over .env file", func(t *testing.T) {
		t.Setenv("TEST_DOTENV_C", "from-host")
		path := writeTemp(t, "TEST_DOTENV_C=from-file\n")
		if err := LoadDotEnv(path); err != nil {
			t.Fatalf("LoadDotEnv() = %v", err)
		}
		if got := GetString("TEST_DOTENV_C", ""); got != "from-host" {
			t.Errorf("host env must win, got %q", got)
		}
	})

	t.Run("missing file is not an error", func(t *testing.T) {
		if err := LoadDotEnv("/nonexistent/.env"); err != nil {
			t.Fatalf("missing .env should not error, got %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// Dates
// ---------------------------------------------------------------------------

func TestParseDate(t *testing.T) {
	tm, err := ParseDate("2026-03-03")
	if err != nil {
		t.Fatalf("ParseDate(valid) = %v", err)
	}
	if tm.Year() != 2026 || tm.Month() != time.March || tm.Day() != 3 {
		t.Fatalf("ParseDate gave %v", tm)
	}

	bad := []string{"", "2026-3-3", "03/03/2026", "2026-03-03T00:00:00Z", "not a date"}
	for _, s := range bad {
		if _, err := ParseDate(s); err == nil {
			t.Errorf("ParseDate(%q) should error", s)
		}
	}
}

func TestTodayUTC_Format(t *testing.T) {
	today := TodayUTC()
	if _, err := ParseDate(today); err != nil {
		t.Fatalf("TodayUTC() = %q is not a valid business date: %v", today, err)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

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

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/.env"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp .env: %v", err)
	}
	return path
}
