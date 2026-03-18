package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"wordbit-advanced-app/backend/internal/service"
)

type Scheduler struct {
	cron     *cron.Cron
	logger   *slog.Logger
	stopOnce sync.Once
}

type Dependencies struct {
	Users           service.UserRepository
	LearningService *service.LearningService
	Clock           service.Clock
	Lookback        time.Duration
}

func New(logger *slog.Logger, deps Dependencies, prewarmSpec string, weaknessSpec string) (*Scheduler, error) {
	c := cron.New(cron.WithLocation(time.UTC))
	s := &Scheduler{
		cron:   c,
		logger: logger,
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
