package service

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

func TestSelectDynamicReviewCandidatesUsesDueCoreOnly(t *testing.T) {
	t.Parallel()

	reviewWord := testDynamicReviewWord("forecast")
	shortTermWord := testDynamicReviewWord("allocate")
	weakWord := testDynamicReviewWord("revenue")

	candidates := selectDynamicReviewCandidates([]domain.DailyLearningPoolItem{
		testDynamicPoolItem(reviewWord, domain.PoolItemTypeReview, domain.ReviewModeMultipleChoice, 1),
		testDynamicPoolItem(shortTermWord, domain.PoolItemTypeShortTerm, domain.ReviewModeFillBlank, 2),
		testDynamicPoolItem(weakWord, domain.PoolItemTypeWeak, domain.ReviewModeMultipleChoice, 3),
		testDynamicPoolItem(testDynamicReviewWord("define"), domain.PoolItemTypeReview, domain.ReviewModeReveal, 4),
	})

	if len(candidates) != 2 {
		t.Fatalf("expected 2 due-core candidates, got %d", len(candidates))
	}
	if candidates[0].WordID != reviewWord.ID || candidates[1].WordID != shortTermWord.ID {
		t.Fatalf("unexpected candidate order: %+v", candidates)
	}
}

func TestDynamicReviewOverlayReusesPromptForWeakPractice(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	word := testDynamicReviewWord("forecast")
	repo := &dynamicReviewPromptRepoStub{
		prompts: []domain.DailyDynamicReviewPrompt{{
			ID:         uuid.New(),
			UserID:     userID,
			LocalDate:  "2026-03-23",
			WordID:     word.ID,
			ReviewMode: domain.ReviewModeMultipleChoice,
			Payload: domain.DynamicReviewPromptPayload{
				PromptText:  "Which word best matches a prediction based on current sales data?",
				Source:      dynamicReviewPromptSource,
				GeneratedAt: "2026-03-23T00:10:05Z",
			},
		}},
	}
	service := NewDynamicReviewService(repo, &dynamicReviewLLMRepoStub{}, &dynamicReviewGeneratorStub{}, dynamicReviewClock{now: time.Now().UTC()}, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)))

	items, err := service.OverlayPoolItems(context.Background(), userID, "2026-03-23", []domain.DailyLearningPoolItem{
		testDynamicPoolItem(word, domain.PoolItemTypeWeak, domain.ReviewModeMultipleChoice, 1),
	})
	if err != nil {
		t.Fatalf("OverlayPoolItems() error = %v", err)
	}
	if items[0].Metadata == nil || items[0].Metadata[dynamicReviewMetadataKey] == nil {
		t.Fatalf("expected weak item to reuse cached dynamic prompt metadata")
	}
}

func TestDynamicReviewPrewarmChunksAndRecordsLLMRuns(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	items := make([]domain.DailyLearningPoolItem, 0, dynamicReviewPromptChunkSize+1)
	for i := 0; i < dynamicReviewPromptChunkSize+1; i++ {
		word := testDynamicReviewWord(fmt.Sprintf("word-%02d", i))
		items = append(items, testDynamicPoolItem(word, domain.PoolItemTypeReview, domain.ReviewModeMultipleChoice, i+1))
	}

	repo := &dynamicReviewPromptRepoStub{}
	llmRepo := &dynamicReviewLLMRepoStub{}
	generator := &dynamicReviewGeneratorStub{}
	service := NewDynamicReviewService(repo, llmRepo, generator, dynamicReviewClock{now: time.Date(2026, 3, 23, 0, 10, 5, 0, time.UTC)}, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)))

	result, err := service.Prewarm(context.Background(), userID, "2026-03-23", items)
	if err != nil {
		t.Fatalf("Prewarm() error = %v", err)
	}
	if result.EligibleCount != dynamicReviewPromptChunkSize+1 {
		t.Fatalf("expected eligible_count=%d, got %d", dynamicReviewPromptChunkSize+1, result.EligibleCount)
	}
	if result.GeneratedCount != dynamicReviewPromptChunkSize+1 {
		t.Fatalf("expected generated_count=%d, got %d", dynamicReviewPromptChunkSize+1, result.GeneratedCount)
	}
	if len(generator.calls) != 2 {
		t.Fatalf("expected 2 generator calls, got %d", len(generator.calls))
	}
	if len(llmRepo.runs) != 2 {
		t.Fatalf("expected 2 llm runs, got %d", len(llmRepo.runs))
	}
	if len(repo.prompts) != dynamicReviewPromptChunkSize+1 {
		t.Fatalf("expected %d cached prompts, got %d", dynamicReviewPromptChunkSize+1, len(repo.prompts))
	}
}

func TestDynamicReviewBackfillGeneratesOnlyOneChunkForCurrentCard(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	items := make([]domain.DailyLearningPoolItem, 0, dynamicReviewPromptChunkSize+5)
	var current domain.DailyLearningPoolItem
	for i := 0; i < dynamicReviewPromptChunkSize+5; i++ {
		word := testDynamicReviewWord(fmt.Sprintf("term-%02d", i))
		item := testDynamicPoolItem(word, domain.PoolItemTypeReview, domain.ReviewModeFillBlank, i+1)
		items = append(items, item)
		if i == dynamicReviewPromptChunkSize+2 {
			current = item
		}
	}

	repo := &dynamicReviewPromptRepoStub{}
	llmRepo := &dynamicReviewLLMRepoStub{}
	generator := &dynamicReviewGeneratorStub{}
	service := NewDynamicReviewService(repo, llmRepo, generator, dynamicReviewClock{now: time.Date(2026, 3, 23, 8, 0, 0, 0, time.UTC)}, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)))

	if err := service.BackfillForCurrentCard(context.Background(), userID, "2026-03-23", items, current); err != nil {
		t.Fatalf("BackfillForCurrentCard() error = %v", err)
	}
	if len(generator.calls) != 1 {
		t.Fatalf("expected 1 generator call for backfill, got %d", len(generator.calls))
	}
	if len(repo.prompts) != 5 {
		t.Fatalf("expected one trailing chunk of 5 prompts, got %d", len(repo.prompts))
	}
}

func TestDynamicReviewBackfillSkipsHiddenMeaningCard(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	word := testDynamicReviewWord("clarify")
	current := testDynamicPoolItem(word, domain.PoolItemTypeReview, domain.ReviewModeReveal, 1)
	generator := &dynamicReviewGeneratorStub{}
	service := NewDynamicReviewService(&dynamicReviewPromptRepoStub{}, &dynamicReviewLLMRepoStub{}, generator, dynamicReviewClock{now: time.Now().UTC()}, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)))

	if err := service.BackfillForCurrentCard(context.Background(), userID, "2026-03-23", []domain.DailyLearningPoolItem{current}, current); err != nil {
		t.Fatalf("BackfillForCurrentCard() error = %v", err)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("expected no generator call for hidden_meaning card")
	}
}

func TestValidateDynamicReviewPayloadRejectsLeakingPrompt(t *testing.T) {
	t.Parallel()

	word := testDynamicReviewWord("forecast")
	issues := validateDynamicReviewPayload(domain.DynamicReviewPromptBatchPayload{
		Items: []domain.DynamicReviewPromptBatchItem{{
			WordID:     word.ID,
			ReviewMode: domain.ReviewModeFillBlank,
			PromptText: "The team used forecast to predict next quarter.",
		}},
	}, []dynamicReviewCandidate{{
		WordID:     word.ID,
		ReviewMode: domain.ReviewModeFillBlank,
		Word:       word,
	}})

	if len(issues) == 0 {
		t.Fatalf("expected validation issues for leaked target spelling")
	}
}

type dynamicReviewPromptRepoStub struct {
	prompts []domain.DailyDynamicReviewPrompt
}

func (r *dynamicReviewPromptRepoStub) ListByLocalDate(ctx context.Context, userID uuid.UUID, localDate string) ([]domain.DailyDynamicReviewPrompt, error) {
	out := make([]domain.DailyDynamicReviewPrompt, 0, len(r.prompts))
	for _, prompt := range r.prompts {
		if prompt.UserID == userID && prompt.LocalDate == localDate {
			out = append(out, prompt)
		}
	}
	return out, nil
}

func (r *dynamicReviewPromptRepoStub) UpsertBatch(ctx context.Context, prompts []domain.DailyDynamicReviewPrompt) ([]domain.DailyDynamicReviewPrompt, error) {
	for _, prompt := range prompts {
		replaced := false
		for idx := range r.prompts {
			if r.prompts[idx].UserID == prompt.UserID && r.prompts[idx].LocalDate == prompt.LocalDate && r.prompts[idx].WordID == prompt.WordID && r.prompts[idx].ReviewMode == prompt.ReviewMode {
				r.prompts[idx] = prompt
				replaced = true
				break
			}
		}
		if !replaced {
			r.prompts = append(r.prompts, prompt)
		}
	}
	return prompts, nil
}

type dynamicReviewLLMRepoStub struct {
	runs []domain.LLMGenerationRun
}

func (r *dynamicReviewLLMRepoStub) Insert(ctx context.Context, run domain.LLMGenerationRun) error {
	_, err := r.InsertReturning(ctx, run)
	return err
}

func (r *dynamicReviewLLMRepoStub) InsertReturning(ctx context.Context, run domain.LLMGenerationRun) (domain.LLMGenerationRun, error) {
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	r.runs = append(r.runs, run)
	return run, nil
}

func (r *dynamicReviewLLMRepoStub) CountByUserLocalDateAndPrompt(ctx context.Context, userID uuid.UUID, localDate string, prompt string) (int, error) {
	count := 0
	for _, run := range r.runs {
		if run.UserID == userID && run.LocalDate == localDate && run.Prompt == prompt {
			count++
		}
	}
	return count, nil
}

func (r *dynamicReviewLLMRepoStub) ListRecentByUser(ctx context.Context, userID uuid.UUID, limit int) ([]domain.LLMGenerationRun, error) {
	return nil, nil
}

type dynamicReviewGeneratorStub struct {
	calls []DynamicReviewPromptGenerationInput
}

func (g *dynamicReviewGeneratorStub) GenerateDynamicReviewPrompts(ctx context.Context, input DynamicReviewPromptGenerationInput) (domain.DynamicReviewPromptBatchPayload, string, error) {
	g.calls = append(g.calls, input)
	items := make([]domain.DynamicReviewPromptBatchItem, 0, len(input.Items))
	for _, item := range input.Items {
		promptText := "Choose the best word for a business prediction."
		if item.ReviewMode == domain.ReviewModeFillBlank {
			promptText = "The company used _____ to guide a risky expansion."
		}
		items = append(items, domain.DynamicReviewPromptBatchItem{
			WordID:     item.WordID,
			ReviewMode: item.ReviewMode,
			PromptText: promptText,
		})
	}
	return domain.DynamicReviewPromptBatchPayload{Items: items}, "{}", nil
}

type dynamicReviewClock struct {
	now time.Time
}

func (c dynamicReviewClock) Now() time.Time { return c.now }

func testDynamicReviewWord(label string) domain.Word {
	id := uuid.New()
	return domain.Word{
		ID:                id,
		Word:              label,
		NormalizedForm:    NormalizeWord(label),
		CanonicalForm:     label,
		Lemma:             label,
		PartOfSpeech:      "noun",
		Level:             domain.CEFRB2,
		Topic:             "Business",
		EnglishMeaning:    "business meaning for " + label,
		VietnameseMeaning: "nghia cho " + label,
		ExampleSentence1:  "The team discussed " + label + " during the meeting.",
		ExampleSentence2:  "A second example about " + label + ".",
	}
}

func testDynamicPoolItem(word domain.Word, itemType domain.PoolItemType, reviewMode domain.ReviewMode, ordinal int) domain.DailyLearningPoolItem {
	wordCopy := word
	dueAt := time.Date(2026, 3, 23, 8, 0, 0, 0, time.UTC).Add(time.Duration(ordinal) * time.Minute)
	return domain.DailyLearningPoolItem{
		ID:         uuid.New(),
		PoolID:     uuid.New(),
		UserID:     uuid.New(),
		WordID:     word.ID,
		Ordinal:    ordinal,
		ItemType:   itemType,
		ReviewMode: reviewMode,
		DueAt:      &dueAt,
		Status:     domain.PoolItemStatusPending,
		IsReview:   true,
		Word:       &wordCopy,
	}
}
