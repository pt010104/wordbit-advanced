package apihttp

import (
	"context"
	"log/slog"
	nethttp "net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"wordbit-advanced-app/backend/internal/auth"
	"wordbit-advanced-app/backend/internal/config"
	"wordbit-advanced-app/backend/internal/domain"
	"wordbit-advanced-app/backend/internal/service"
)

type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
}

type Handler struct {
	logger        *slog.Logger
	db            *pgxpool.Pool
	build         BuildInfo
	settings      *service.SettingsService
	dictionary    *service.DictionaryService
	pools         *service.PoolService
	learning      *service.LearningService
	dynamicReview *service.DynamicReviewService
	wordSets      *service.WordSetService
	importBuffer  *service.WordImportBufferService
	statistics    *service.StatisticsService
	recordings    *service.RecordingService
	llmRuns       service.LLMRunRepository
	promptTester  service.PromptTester
}

func NewRouter(cfg config.Config, logger *slog.Logger, db *pgxpool.Pool, verifier *auth.Verifier, identity *service.IdentityService, settings *service.SettingsService, dictionary *service.DictionaryService, pools *service.PoolService, learning *service.LearningService, dynamicReview *service.DynamicReviewService, wordSets *service.WordSetService, importBuffer *service.WordImportBufferService, statistics *service.StatisticsService, recordings *service.RecordingService, llmRuns service.LLMRunRepository, promptTester service.PromptTester, build BuildInfo) nethttp.Handler {
	mw := NewMiddleware(logger, verifier, identity, cfg.AdminToken)
	h := &Handler{
		logger:        logger,
		db:            db,
		build:         build,
		settings:      settings,
		dictionary:    dictionary,
		pools:         pools,
		learning:      learning,
		dynamicReview: dynamicReview,
		wordSets:      wordSets,
		importBuffer:  importBuffer,
		statistics:    statistics,
		recordings:    recordings,
		llmRuns:       llmRuns,
		promptTester:  promptTester,
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	// Compress responses (e.g. the ~121KB daily-pool payload) so large bodies
	// don't dominate request latency on slow/mobile networks. gzip typically
	// shrinks this JSON ~5-10x; clients negotiate via Accept-Encoding.
	r.Use(middleware.Compress(5))
	r.Use(mw.RequestLogger)
	r.Use(mw.Recoverer)
	r.Use(middleware.Timeout(3 * time.Minute))

	r.Get("/healthz", h.Health)

	r.Route("/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(mw.RequireUser)
			r.Get("/me/settings", h.GetSettings)
			r.Put("/me/settings", h.UpdateSettings)
			r.Post("/me/llm/test", h.TestLLM)
			r.Get("/me/word-sets", h.ListWordSets)
			r.Post("/me/word-sets", h.CreateWordSet)
			r.Put("/me/word-sets/{setID}", h.UpdateWordSet)
			r.Put("/me/word-sets/{setID}/preferences", h.UpdateWordSetPreferences)
			r.Delete("/me/word-sets/{setID}", h.DeleteWordSet)
			r.Post("/me/word-sets/{setID}/activate", h.ActivateWordSet)
			r.Get("/me/statistics", h.GetStatistics)
			r.Get("/me/dictionary/words", h.ListDictionaryWords)
			r.Post("/me/dictionary/words", h.CreateDictionaryWord)
			r.Put("/me/dictionary/words/{wordID}", h.UpdateDictionaryWord)
			r.Delete("/me/dictionary/words/{wordID}", h.DeleteDictionaryWord)
			r.Get("/me/import-buffer", h.ListImportBufferItems)
			r.Post("/me/import-buffer", h.CreateImportBufferItem)
			r.Post("/me/import-buffer/{itemID}/generate", h.GenerateImportBufferItem)
			r.Put("/me/import-buffer/{itemID}", h.UpdateImportBufferItemCandidate)
			r.Post("/me/import-buffer/{itemID}/confirm", h.ConfirmImportBufferItem)
			r.Delete("/me/import-buffer/{itemID}", h.DeleteImportBufferItem)
			r.Get("/me/daily-pool", h.GetDailyPool)
			r.Post("/me/daily-pool/more-words", h.AppendMoreWords)
			r.Post("/me/daily-pool/dynamic-review/generate", h.GenerateDynamicReviewPrompts)
			r.Get("/me/cards/next", h.GetNextCard)
			r.Post("/me/cards/{poolItemID}/first-exposure", h.SubmitFirstExposure)
			r.Post("/me/cards/{poolItemID}/review", h.SubmitReview)
			r.Post("/me/cards/{poolItemID}/undo-last-answer", h.UndoLastAnswer)
			r.Post("/me/cards/{poolItemID}/events/reveal", h.SubmitReveal)
			r.Post("/me/cards/{poolItemID}/events/pronunciation", h.SubmitPronunciation)
			r.Post("/me/recordings", h.UploadRecording)
			r.Get("/me/recordings/{wordID}", h.GetRecording)
		})

		r.Group(func(r chi.Router) {
			r.Use(mw.RequireAdmin)
			r.Post("/admin/users/{userID}/daily-pool/rebuild", h.AdminRebuildPool)
			r.Get("/admin/users/{userID}/llm-runs", h.AdminListLLMRuns)
		})
	})
	return r
}

func (h *Handler) Health(w nethttp.ResponseWriter, r *nethttp.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	status := "ok"
	dbStatus := "ok"
	if err := h.db.Ping(ctx); err != nil {
		status = "degraded"
		dbStatus = err.Error()
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{
		"status": status,
		"db":     dbStatus,
		"build":  h.build,
	})
}

func currentUser(r *nethttp.Request) (domain.User, error) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		return domain.User{}, domain.ErrUnauthorized
	}
	return user, nil
}
