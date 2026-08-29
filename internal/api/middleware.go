package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/ramanasai/wrap-policy-assignment-system/internal/logging"
)

// requestLogger emits one structured access-log line per request, carrying
// the chi request ID so frontend/trace correlation works end to end.
func requestLogger(root zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			next.ServeHTTP(ww, r)

			log := logging.WithRequestID(root, middleware.GetReqID(r.Context()))
			log.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", ww.Status()).
				Dur("elapsed", time.Since(start)).
				Msg("request")
		})
	}
}
