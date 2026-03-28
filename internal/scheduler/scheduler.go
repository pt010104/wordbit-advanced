package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"wordbit-advanced-app/backend/internal/domain"
	"wordbit-advanced-app/backend/internal/service"
)

type Scheduler struct {
	cron     *cron.Cron
	logger   *slog.Logger
	stopOnce sync.Once
}

type WeaknessRefresher interface {
	RefreshWeaknessForActiveUser(ctx context.Context, userID uuid.UUID) error
}

type DailyPoolPrewarmer interface {
	GetOrCreateDailyPool(ctx context.Context, user domain.User) (service.DailyPoolView, error)
}

type DynamicReviewPrewarmer interface {
	Prewarm(ctx context.Context, userID uuid.UUID, localDate string, items []domain.DailyLearningPoolItem) (service.DynamicReviewGenerationResult, error)
}

type Dependencies struct {
	Users                service.UserRepository
	LearningService      WeaknessRefresher
	PoolService          DailyPoolPrewarmer
	DynamicReviewService DynamicReviewPrewarmer
	Clock                service.Clock
	Lookback             time.Duration
}

func New(logger *slog.Logger, deps Dependencies, dailyPoolPrewarmSpec string, dynamicReviewPrewarmSpec string, weaknessSpec string) (*Scheduler, error) {
	location, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		return nil, err
	}
	c := cron.New(cron.WithLocation(location))
	s := &Scheduler{
		cron:   c,
		logger: logger,
	}

	if _, err := c.AddFunc(dailyPoolPrewarmSpec, func() {
		s.run("prewarm_daily_pool", func(ctx context.Context) error {
			return prewarmDailyPools(ctx, deps, s.logger)
		})
	}); err != nil {
		return nil, err
	}
	if _, err := c.AddFunc(dynamicReviewPrewarmSpec, func() {
		s.run("prewarm_dynamic_review", func(ctx context.Context) error {
			return prewarmDynamicReviewPrompts(ctx, deps, s.logger)
		})
	}); err != nil {
		return nil, err
	}
	if _, err := c.AddFunc(weaknessSpec, func() {
		s.run("refresh_weakness", func(ctx context.Context) error {
			return refreshWeakness(ctx, deps)
		})
	}); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Scheduler) Start() {
	s.cron.Start()
}

func (s *Scheduler) Stop() context.Context {
	var stopCtx context.Context
	s.stopOnce.Do(func() {
		stopCtx = s.cron.Stop()
	})
	if stopCtx == nil {
		return context.Background()
	}
	return stopCtx
}

func (s *Scheduler) run(name string, fn func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	start := time.Now()
	if err := fn(ctx); err != nil {
		s.logger.Error("scheduler job failed", "job", name, "error", err)
		return
	}
	s.logger.Info("scheduler job completed", "job", name, "duration_ms", time.Since(start).Milliseconds())
}

func refreshWeakness(ctx context.Context, deps Dependencies) error {
	users, err := deps.Users.ListActiveUsers(ctx, deps.Clock.Now().Add(-deps.Lookback))
	if err != nil {
		return err
	}
	for _, user := range users {
		if err := deps.LearningService.RefreshWeaknessForActiveUser(ctx, user.ID); err != nil {
			return err
		}
	}
	return nil
}

func prewarmDailyPools(ctx context.Context, deps Dependencies, logger *slog.Logger) error {
	users, err := deps.Users.ListActiveUsers(ctx, deps.Clock.Now().Add(-deps.Lookback))
	if err != nil {
		return err
	}
	for _, user := range users {
		if _, err := deps.PoolService.GetOrCreateDailyPool(ctx, user); err != nil {
			logger.Warn("prewarm daily pool skipped", "user_id", user.ID, "error", err)
		}
	}
	return nil
}

func prewarmDynamicReviewPrompts(ctx context.Context, deps Dependencies, logger *slog.Logger) error {
	users, err := deps.Users.ListActiveUsers(ctx, deps.Clock.Now().Add(-deps.Lookback))
	if err != nil {
		return err
	}
	for _, user := range users {
		view, viewErr := deps.PoolService.GetOrCreateDailyPool(ctx, user)
		if viewErr != nil {
			logger.Warn("prewarm dynamic review skipped: load pool", "user_id", user.ID, "error", viewErr)
			continue
		}
		if _, err := deps.DynamicReviewService.Prewarm(ctx, user.ID, view.Pool.LocalDate, view.Items); err != nil {
			logger.Warn("prewarm dynamic review skipped: prepare prompts", "user_id", user.ID, "local_date", view.Pool.LocalDate, "error", err)
		}
	}
	return nil
}
