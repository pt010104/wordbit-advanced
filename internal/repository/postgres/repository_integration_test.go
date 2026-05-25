//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"

	"wordbit-advanced-app/backend/internal/database"
	"wordbit-advanced-app/backend/internal/domain"
	postgresrepo "wordbit-advanced-app/backend/internal/repository/postgres"
)

func TestCreateAndReadDailyPoolAgainstPostgres(t *testing.T) {
	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Skipf("docker unavailable: %v", err)
	}

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "16-alpine",
		Env: []string{
			"POSTGRES_USER=postgres",
			"POSTGRES_PASSWORD=postgres",
			"POSTGRES_DB=wordbit_test",
		},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		_ = pool.Purge(resource)
	})

	databaseURL := fmt.Sprintf("postgres://postgres:postgres@localhost:%s/wordbit_test?sslmode=disable", resource.GetPort("5432/tcp"))
	pool.MaxWait = 90 * time.Second
	if err := pool.Retry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return database.RunMigrations(ctx, databaseURL, "up")
	}); err != nil {
		t.Fatalf("migrate postgres container: %v", err)
	}

	ctx := context.Background()
	db, err := database.OpenPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer db.Close()

	repos := postgresrepo.NewRepositories(db)
	user, err := repos.Users.GetOrCreateByExternalSubject(ctx, "integration-user", "integration@example.com")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	word, err := repos.Words.UpsertWord(ctx, domain.CandidateWord{
		Word:              "sustain",
		NormalizedForm:    "sustain",
		CanonicalForm:     "sustain",
		Lemma:             "sustain",
		Level:             domain.CEFRB2,
		Topic:             "Environment",
		EnglishMeaning:    "maintain",
		VietnameseMeaning: "duy trì",
		SourceProvider:    domain.DefaultLLMProvider,
	})
	if err != nil {
		t.Fatalf("upsert word: %v", err)
	}

	poolDate := time.Now().UTC().Format("2006-01-02")
	createdPool, items, err := repos.Pools.CreatePoolWithItems(ctx, domain.DailyLearningPool{
		UserID:         user.ID,
		LocalDate:      poolDate,
		Timezone:       domain.DefaultTimezone,
		Topic:          "Environment",
		DueReviewCount: 0,
		ShortTermCount: 0,
		WeakCount:      0,
		NewCount:       1,
		GeneratedAt:    time.Now().UTC(),
	}, []domain.DailyLearningPoolItem{{
		UserID:                user.ID,
		WordID:                word.ID,
		Ordinal:               1,
		ItemType:              domain.PoolItemTypeNew,
		ReviewMode:            domain.ReviewModeReveal,
		Status:                domain.PoolItemStatusPending,
		IsReview:              false,
		FirstExposureRequired: true,
	}})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	if createdPool.ID == uuid.Nil {
		t.Fatalf("expected pool id to be assigned")
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 pool item, got %d", len(items))
	}

	reloadedPool, reloadedItems, err := repos.Pools.GetByLocalDate(ctx, user.ID, poolDate)
	if err != nil {
		t.Fatalf("reload pool: %v", err)
	}
	if reloadedPool.ID != createdPool.ID {
		t.Fatalf("expected same pool id, got %s vs %s", reloadedPool.ID, createdPool.ID)
	}
	if len(reloadedItems) != 1 || reloadedItems[0].Word == nil || reloadedItems[0].Word.Word != "sustain" {
		t.Fatalf("expected reloaded item with joined word, got %+v", reloadedItems)
	}
}

func TestListDictionaryEntriesOrdersUpcomingReviewsFirst(t *testing.T) {
	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Skipf("docker unavailable: %v", err)
	}

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "16-alpine",
		Env: []string{
			"POSTGRES_USER=postgres",
			"POSTGRES_PASSWORD=postgres",
			"POSTGRES_DB=wordbit_test",
		},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		_ = pool.Purge(resource)
	})

	databaseURL := fmt.Sprintf("postgres://postgres:postgres@localhost:%s/wordbit_test?sslmode=disable", resource.GetPort("5432/tcp"))
	pool.MaxWait = 90 * time.Second
	if err := pool.Retry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return database.RunMigrations(ctx, databaseURL, "up")
	}); err != nil {
		t.Fatalf("migrate postgres container: %v", err)
	}

	ctx := context.Background()
	db, err := database.OpenPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer db.Close()

	repos := postgresrepo.NewRepositories(db)
	user, err := repos.Users.GetOrCreateByExternalSubject(ctx, "dictionary-order-user", "dictionary@example.com")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	earlierWord, err := repos.Words.UpsertWord(ctx, domain.CandidateWord{
		Word:              "arrive",
		NormalizedForm:    "arrive",
		CanonicalForm:     "arrive",
		Lemma:             "arrive",
		Level:             domain.CEFRB1,
		Topic:             "Travel",
		EnglishMeaning:    "reach a place",
		VietnameseMeaning: "den noi",
		SourceProvider:    domain.DefaultLLMProvider,
	})
	if err != nil {
		t.Fatalf("upsert earlier word: %v", err)
	}
	laterWord, err := repos.Words.UpsertWord(ctx, domain.CandidateWord{
		Word:              "wander",
		NormalizedForm:    "wander",
		CanonicalForm:     "wander",
		Lemma:             "wander",
		Level:             domain.CEFRB1,
		Topic:             "Travel",
		EnglishMeaning:    "walk around",
		VietnameseMeaning: "di lang thang",
		SourceProvider:    domain.DefaultLLMProvider,
	})
	if err != nil {
		t.Fatalf("upsert later word: %v", err)
	}

	earlierReview := time.Now().UTC().Add(15 * time.Minute)
	laterReview := time.Now().UTC().Add(45 * time.Minute)
	if _, err := repos.States.Upsert(ctx, domain.UserWordState{
		UserID:       user.ID,
		WordID:       earlierWord.ID,
		Status:       domain.WordStatusReview,
		NextReviewAt: &earlierReview,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert earlier state: %v", err)
	}
	if _, err := repos.States.Upsert(ctx, domain.UserWordState{
		UserID:       user.ID,
		WordID:       laterWord.ID,
		Status:       domain.WordStatusReview,
		NextReviewAt: &laterReview,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert later state: %v", err)
	}

	entries, err := repos.States.ListDictionaryEntries(ctx, user.ID, domain.DictionaryFilterUnknown, "", nil, 20, 0)
	if err != nil {
		t.Fatalf("ListDictionaryEntries: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(entries))
	}
	if entries[0].Word.Word != "arrive" || entries[1].Word.Word != "wander" {
		t.Fatalf("expected earliest next_review_at first, got %s then %s", entries[0].Word.Word, entries[1].Word.Word)
	}
}
