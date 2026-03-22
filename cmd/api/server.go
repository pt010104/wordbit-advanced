package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"wordbit-advanced-app/backend/internal/auth"
	"wordbit-advanced-app/backend/internal/config"
	"wordbit-advanced-app/backend/internal/database"
	apihttp "wordbit-advanced-app/backend/internal/http"
	"wordbit-advanced-app/backend/internal/integrations/gemini"
	"wordbit-advanced-app/backend/internal/repository/postgres"
	"wordbit-advanced-app/backend/internal/scheduler"
	"wordbit-advanced-app/backend/internal/service"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func runServer(ctx context.Context, cfg config.Config) error {
	logger := newLogger(cfg.LogLevel)
	if cfg.AutoMigrate {
		if err := database.RunMigrations(ctx, cfg.DatabaseURL, "up"); err != nil {
			return fmt.Errorf("auto migrate: %w", err)
		}
	}
	db, err := database.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	repos := postgres.NewRepositories(db)
	clock := service.RealClock{}
	identity := service.NewIdentityService(repos.Users, clock)
	settings := service.NewSettingsService(repos.Settings)
	dictionary := service.NewDictionaryService(repos.Settings, repos.Words, repos.States, repos.Pools, clock)
	geminiClient := gemini.NewClient(cfg.Gemini, logger)
	poolService := service.NewPoolService(repos.Settings, repos.Words, repos.States, repos.Pools, repos.Events, repos.LLMRuns, geminiClient, clock, logger, cfg.MemoryCauseInferenceEnabled)
	learningService := service.NewLearningService(repos.Settings, repos.States, repos.Pools, repos.Events, poolService, clock, logger, cfg.MemoryCauseInferenceEnabled)
	exerciseService := service.NewExerciseService(repos.Settings, repos.Words, repos.States, repos.ExercisePacks, repos.LLMRuns, geminiClient, clock, logger)
	verifier := auth.NewVerifier(cfg.Auth, logger)

	router := apihttp.NewRouter(cfg, logger, db, verifier, identity, settings, dictionary, poolService, learningService, exerciseService, repos.LLMRuns, apihttp.BuildInfo{
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
	})

	var appScheduler *scheduler.Scheduler
	if cfg.Scheduler.Enabled {
		appScheduler, err = scheduler.New(logger, scheduler.Dependencies{
			Users:           repos.Users,
			LearningService: learningService,
			Clock:           clock,
			Lookback:        cfg.Scheduler.ActiveUserLookback,
		}, cfg.Scheduler.PrewarmCron, cfg.Scheduler.WeaknessRefreshCron)
		if err != nil {
			return fmt.Errorf("create scheduler: %w", err)
		}
		appScheduler.Start()
		defer appScheduler.Stop()
	}

	server := &http.Server{
		Addr:         cfg.Address(),
		Handler:      router,
		ReadTimeout:  cfg.HTTPReadTimeout,
		WriteTimeout: cfg.HTTPWriteTimeout,
		IdleTimeout:  cfg.HTTPIdleTimeout,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server starting", "addr", cfg.Address())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger.Info("server shutting down")
	return server.Shutdown(shutdownCtx)
}

func newLogger(level string) *slog.Logger {
	slogLevel := slog.LevelInfo
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel}))
}
