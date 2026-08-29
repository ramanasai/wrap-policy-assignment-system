package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// Build — output shape and formats
// ---------------------------------------------------------------------------

func TestBuild_JSONOutputHasExpectedFields(t *testing.T) {
	var buf bytes.Buffer
	log := Build(Options{Level: "debug", Out: &buf})
	log.Info().Str("key", "value").Msg("hello")

	line := buf.String()
	if strings.HasPrefix(line, "{") == false {
		t.Fatalf("output is not JSON: %q", line)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v — %q", err, line)
	}
	if parsed["level"] != "info" {
		t.Errorf("level = %v, want info", parsed["level"])
	}
	if parsed["message"] != "hello" {
		t.Errorf("message = %v, want hello", parsed["message"])
	}
	if parsed["key"] != "value" {
		t.Errorf("custom field key = %v, want value", parsed["key"])
	}
	if _, ok := parsed["ts"]; !ok {
		t.Error("timestamp field 'ts' missing")
	}
}

func TestBuild_ConsoleFormatIsNotJSON(t *testing.T) {
	var buf bytes.Buffer
	log := Build(Options{Level: "info", Format: FormatConsole, Out: &buf})
	log.Info().Msg("pretty")

	out := buf.String()
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("console format should be human-readable, got JSON: %q", out)
	}
	if !strings.Contains(out, "pretty") {
		t.Errorf("console output missing message: %q", out)
	}
}

func TestBuild_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	log := Build(Options{Level: "warn", Out: &buf})

	log.Debug().Msg("debug") // suppressed
	log.Info().Msg("info")   // suppressed
	log.Warn().Msg("warn")   // emitted
	log.Error().Msg("error") // emitted

	out := buf.String()
	if strings.Contains(out, "debug") || strings.Contains(out, "info") {
		t.Errorf("below-threshold lines leaked: %q", out)
	}
	if !strings.Contains(out, "warn") || !strings.Contains(out, "error") {
		t.Errorf("threshold lines missing: %q", out)
	}
}

// ---------------------------------------------------------------------------
// New / WithRequestID — field injection
// ---------------------------------------------------------------------------

func TestNew_TagsComponent(t *testing.T) {
	var buf bytes.Buffer
	root := NewTest(&buf)
	api := New(root, ComponentAPI)
	api.Info().Msg("serving")

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["component"] != ComponentAPI {
		t.Errorf("component = %v, want %q", parsed["component"], ComponentAPI)
	}
}

func TestWithRequestID_SurvivesChaining(t *testing.T) {
	var buf bytes.Buffer
	root := NewTest(&buf)
	req := WithRequestID(New(root, ComponentAPI), "req-123")
	req.Info().Msg("handling")

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["request_id"] != "req-123" {
		t.Errorf("request_id = %v, want req-123", parsed["request_id"])
	}
	if parsed["component"] != ComponentAPI {
		t.Errorf("component lost after request scoping: %v", parsed["component"])
	}
}

// ---------------------------------------------------------------------------
// ParseLevel / IsValidLevel / IsValidFormat
// ---------------------------------------------------------------------------

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in   string
		want zerolog.Level
	}{
		{in: "debug", want: zerolog.DebugLevel},
		{in: "info", want: zerolog.InfoLevel},
		{in: "", want: zerolog.InfoLevel},
		{in: "DEBUG", want: zerolog.DebugLevel},
		{in: "warn", want: zerolog.WarnLevel},
		{in: "warning", want: zerolog.WarnLevel},
		{in: "error", want: zerolog.ErrorLevel},
		{in: "fatal", want: zerolog.FatalLevel},
		{in: "panic", want: zerolog.PanicLevel},
		{in: "disabled", want: zerolog.Disabled},
		{in: "garbage", want: zerolog.InfoLevel}, // unknown falls back to info
	}
	for _, tt := range tests {
		if got := ParseLevel(tt.in); got != tt.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseLevel_UnknownFallsBackToInfoNotPanic(t *testing.T) {
	// The critical property: a typo must never take down a service.
	if ParseLevel("not-a-level") == 0 {
		t.Fatal("unknown level resolved to zero (panic level?)")
	}
}

func TestIsValidLevel(t *testing.T) {
	valid := []string{"", "debug", "info", "warn", "warning", "error", "fatal", "panic", "disabled", " DEBUG "}
	for _, s := range valid {
		if !IsValidLevel(s) {
			t.Errorf("IsValidLevel(%q) = false, want true", s)
		}
	}
	invalid := []string{"loud", "verbose", "informational"}
	for _, s := range invalid {
		if IsValidLevel(s) {
			t.Errorf("IsValidLevel(%q) = true, want false", s)
		}
	}
}

func TestIsValidFormat(t *testing.T) {
	valid := []string{"", "json", "console", "CONSOLE"}
	for _, s := range valid {
		if !IsValidFormat(s) {
			t.Errorf("IsValidFormat(%q) = false, want true", s)
		}
	}
	if IsValidFormat("xml") {
		t.Error("IsValidFormat(xml) = true, want false")
	}
}

// ---------------------------------------------------------------------------
// Concurrency — zerolog must stay JSON-valid under parallel writers
// ---------------------------------------------------------------------------

func TestBuild_ConcurrentWritesProduceValidJSON(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	log := NewTest(&safeWriter{mu: &mu, w: &buf})

	var wg sync.WaitGroup
	const goroutines = 16
	const lines = 25
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < lines; i++ {
				log.Info().Int("g", id).Msg("concurrent")
			}
		}(g)
	}
	wg.Wait()

	linesOut := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(linesOut) != goroutines*lines {
		t.Fatalf("lost lines under concurrency: got %d, want %d", len(linesOut), goroutines*lines)
	}
	for i, line := range linesOut {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Fatalf("line %d is not valid JSON (interleaved writes?): %v — %q", i, err, line)
		}
	}
}

type safeWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (s *safeWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
