package apihttp

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"wordbit-advanced-app/backend/internal/auth"
	"wordbit-advanced-app/backend/internal/domain"
	"wordbit-advanced-app/backend/internal/service"
)

type Middleware struct {
	logger     *slog.Logger
	verifier   *auth.Verifier
	identity   *service.IdentityService
	adminToken string
}

func NewMiddleware(logger *slog.Logger, verifier *auth.Verifier, identity *service.IdentityService, adminToken string) *Middleware {
	return &Middleware{
		logger:     logger,
		verifier:   verifier,
		identity:   identity,
		adminToken: adminToken,
	}
}

func (m *Middleware) RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		m.logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}

func (m *Middleware) Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				m.logger.Error("panic recovered", "error", recovered)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject, err := m.verifier.Verify(r.Context(), auth.ParseBearerToken(r.Header.Get("Authorization")))
		if err != nil {
			writeError(w, domain.ErrUnauthorized)
			return
		}
		user, err := m.identity.ResolveUser(r.Context(), service.AuthSubject{
			Subject: subject.Subject,
			Email:   subject.Email,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), user)))
	})
}

func (m *Middleware) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.adminToken == "" || r.Header.Get("X-Admin-Token") != m.adminToken {
			writeError(w, domain.ErrForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey("admin"), true)))
	})
}
