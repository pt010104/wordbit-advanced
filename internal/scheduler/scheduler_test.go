package scheduler

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
	"wordbit-advanced-app/backend/internal/service"
)

func TestPrewarmDailyPoolsLoadsPoolForEveryActiveUserWithoutDynamicReview(t *testing.T) {
	t.Parallel()

	users := []domain.User{
		{ID: uuid.New()},
		{ID: uuid.New()},
	}
	userRepo := &stubUserRepo{activeUsers: users}
	poolPrewarmer := &stubPoolPrewarmer{
		view: service.DailyPoolView{
			Pool: domain.DailyLearningPool{LocalDate: "2026-03-28"},
		},
	}
	dynamicReview := &stubDynamicReviewPrewarmer{}
	deps := Dependencies{
		Users:                userRepo,
		LearningService:      &stubWeaknessRefresher{},
		PoolService:          poolPrewarmer,
		DynamicReviewService: dynamicReview,
		Clock:                fixedClock{now: time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC)},
		Lookback:             24 * time.Hour,
	}

	if err := prewarmDailyPools(context.Background(), deps, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("prewarmDailyPools() error = %v", err)
	}

	if poolPrewarmer.calls != len(users) {
		t.Fatalf("expected %d pool prewarm calls, got %d", len(users), poolPrewarmer.calls)
	}
	if dynamicReview.calls != 0 {
		t.Fatalf("expected no dynamic review prewarm calls, got %d", dynamicReview.calls)
	}
}

type stubUserRepo struct {
	activeUsers []domain.User
}

func (r *stubUserRepo) GetOrCreateByExternalSubject(ctx context.Context, subject string, email string) (domain.User, error) {
	return domain.User{}, nil
}

func (r *stubUserRepo) TouchLastActive(ctx context.Context, userID uuid.UUID, at time.Time) error {
	return nil
}

func (r *stubUserRepo) ListActiveUsers(ctx context.Context, since time.Time) ([]domain.User, error) {
	return append([]domain.User(nil), r.activeUsers...), nil
}

type stubWeaknessRefresher struct{}

func (r *stubWeaknessRefresher) RefreshWeaknessForActiveUser(ctx context.Context, userID uuid.UUID) error {
	return nil
}

type stubPoolPrewarmer struct {
	calls int
	view  service.DailyPoolView
}

func (p *stubPoolPrewarmer) GetOrCreateDailyPool(ctx context.Context, user domain.User) (service.DailyPoolView, error) {
	p.calls++
	return p.view, nil
}

type stubDynamicReviewPrewarmer struct {
	calls int
}

func (p *stubDynamicReviewPrewarmer) Prewarm(ctx context.Context, userID uuid.UUID, localDate string, items []domain.DailyLearningPoolItem) (service.DynamicReviewGenerationResult, error) {
	p.calls++
	return service.DynamicReviewGenerationResult{}, nil
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time { return c.now }
