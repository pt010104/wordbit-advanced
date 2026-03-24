package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

type mode4TestClock struct {
	now time.Time
}

func (c mode4TestClock) Now() time.Time { return c.now }

type mode4TestWordRepo struct {
	words map[uuid.UUID]domain.Word
}

func (r *mode4TestWordRepo) UpsertWord(ctx context.Context, candidate domain.CandidateWord) (domain.Word, error) {
	return domain.Word{}, errors.New("not implemented")
}
func (r *mode4TestWordRepo) GetByID(ctx context.Context, wordID uuid.UUID) (domain.Word, error) {
	word, ok := r.words[wordID]
	if !ok {
		return domain.Word{}, domain.ErrNotFound
	}
	return word, nil
}
func (r *mode4TestWordRepo) UpdateWord(ctx context.Context, wordID uuid.UUID, candidate domain.CandidateWord) (domain.Word, error) {
	return domain.Word{}, errors.New("not implemented")
}
func (r *mode4TestWordRepo) ListWordIDsSeenAsNew(ctx context.Context, userID uuid.UUID, since time.Time) ([]uuid.UUID, error) {
	return nil, nil
}
func (r *mode4TestWordRepo) ListBankWords(ctx context.Context, userID uuid.UUID, level domain.CEFRLevel, topic string, excludeWordIDs []uuid.UUID, limit int) ([]domain.Word, error) {
	return nil, nil
}
func (r *mode4TestWordRepo) ListWordsByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.Word, error) {
	out := make([]domain.Word, 0, len(ids))
	for _, id := range ids {
		if word, ok := r.words[id]; ok {
			out = append(out, word)
		}
	}
	return out, nil
}

type mode4TestStateRepo struct {
	mode4Candidates []domain.UserWordState
	states          map[uuid.UUID]domain.UserWordState
	upserts         []domain.UserWordState
}

func (r *mode4TestStateRepo) Get(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) (domain.UserWordState, error) {
	state, ok := r.states[wordID]
	if !ok {
		return domain.UserWordState{}, domain.ErrNotFound
	}
	return state, nil
}
func (r *mode4TestStateRepo) ListDueWithinWindow(ctx context.Context, userID uuid.UUID, start time.Time, end time.Time, learningOnly bool) ([]domain.UserWordState, error) {
	return nil, nil
}
func (r *mode4TestStateRepo) ListWeakCandidates(ctx context.Context, userID uuid.UUID, excludeWordIDs []uuid.UUID, limit int) ([]domain.UserWordState, error) {
	return nil, nil
}
func (r *mode4TestStateRepo) ListMode4Candidates(ctx context.Context, userID uuid.UUID, limit int) ([]domain.UserWordState, error) {
	if limit <= 0 || limit >= len(r.mode4Candidates) {
		return append([]domain.UserWordState(nil), r.mode4Candidates...), nil
	}
	return append([]domain.UserWordState(nil), r.mode4Candidates[:limit]...), nil
}
func (r *mode4TestStateRepo) ListExistingWords(ctx context.Context, userID uuid.UUID) ([]domain.UserWordState, error) {
	return nil, nil
}
func (r *mode4TestStateRepo) ListDictionaryEntries(ctx context.Context, userID uuid.UUID, filter domain.DictionaryFilter, query string, limit int, offset int) ([]domain.DictionaryEntry, error) {
	return nil, nil
}
func (r *mode4TestStateRepo) Upsert(ctx context.Context, state domain.UserWordState) (domain.UserWordState, error) {
	if r.states == nil {
		r.states = map[uuid.UUID]domain.UserWordState{}
	}
	r.states[state.WordID] = state
	r.upserts = append(r.upserts, state)
	return state, nil
}
func (r *mode4TestStateRepo) Delete(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error {
	return nil
}
func (r *mode4TestStateRepo) RefreshWeaknessScores(ctx context.Context, userID uuid.UUID) error {
	return nil
}

type mode4TestReviewRepo struct {
	state        domain.Mode4ReviewState
	passages     map[uuid.UUID]domain.Mode4ReviewPassage
	generationID map[int]uuid.UUID
}

func (r *mode4TestReviewRepo) GetOrCreateState(ctx context.Context, userID uuid.UUID) (domain.Mode4ReviewState, error) {
	if r.state.UserID == uuid.Nil {
		r.state.UserID = userID
	}
	return r.state, nil
}
func (r *mode4TestReviewRepo) UpsertState(ctx context.Context, state domain.Mode4ReviewState) (domain.Mode4ReviewState, error) {
	r.state = state
	return r.state, nil
}
func (r *mode4TestReviewRepo) GetActivePassage(ctx context.Context, userID uuid.UUID) (domain.Mode4ReviewPassage, error) {
	for _, passage := range r.passages {
		if passage.UserID == userID && passage.Status == domain.Mode4ReviewPassageStatusActive {
			return passage, nil
		}
	}
	return domain.Mode4ReviewPassage{}, domain.ErrNotFound
}
func (r *mode4TestReviewRepo) GetPassage(ctx context.Context, userID uuid.UUID, passageID uuid.UUID) (domain.Mode4ReviewPassage, error) {
	passage, ok := r.passages[passageID]
	if !ok || passage.UserID != userID {
		return domain.Mode4ReviewPassage{}, domain.ErrNotFound
	}
	return passage, nil
}
func (r *mode4TestReviewRepo) GetPassageByGeneration(ctx context.Context, userID uuid.UUID, generationNumber int) (domain.Mode4ReviewPassage, error) {
	passageID, ok := r.generationID[generationNumber]
	if !ok {
		return domain.Mode4ReviewPassage{}, domain.ErrNotFound
	}
	return r.GetPassage(ctx, userID, passageID)
}
func (r *mode4TestReviewRepo) GetLatestPassage(ctx context.Context, userID uuid.UUID) (domain.Mode4ReviewPassage, error) {
	var (
		best     domain.Mode4ReviewPassage
		bestSeen bool
	)
	for _, passage := range r.passages {
		if passage.UserID != userID {
			continue
		}
		if !bestSeen || passage.GenerationNumber > best.GenerationNumber {
			best = passage
			bestSeen = true
		}
	}
	if !bestSeen {
		return domain.Mode4ReviewPassage{}, domain.ErrNotFound
	}
	return best, nil
}
func (r *mode4TestReviewRepo) CreatePassage(ctx context.Context, passage domain.Mode4ReviewPassage) (domain.Mode4ReviewPassage, error) {
	if r.passages == nil {
		r.passages = map[uuid.UUID]domain.Mode4ReviewPassage{}
	}
	if r.generationID == nil {
		r.generationID = map[int]uuid.UUID{}
	}
	r.passages[passage.ID] = passage
	r.generationID[passage.GenerationNumber] = passage.ID
	return passage, nil
}
func (r *mode4TestReviewRepo) UpdatePassageSkip(ctx context.Context, userID uuid.UUID, passageID uuid.UUID, skipCount int, skippedAt *time.Time) (domain.Mode4ReviewPassage, error) {
	passage, err := r.GetPassage(ctx, userID, passageID)
	if err != nil {
		return domain.Mode4ReviewPassage{}, err
	}
	passage.SkipCount = skipCount
	passage.LastSkippedAt = skippedAt
	passage.Status = domain.Mode4ReviewPassageStatusActive
	r.passages[passageID] = passage
	return passage, nil
}
func (r *mode4TestReviewRepo) UpdatePassageStatus(ctx context.Context, userID uuid.UUID, passageID uuid.UUID, status domain.Mode4ReviewPassageStatus, completedAt *time.Time) (domain.Mode4ReviewPassage, error) {
	passage, err := r.GetPassage(ctx, userID, passageID)
	if err != nil {
		return domain.Mode4ReviewPassage{}, err
	}
	passage.Status = status
	passage.CompletedAt = completedAt
	r.passages[passageID] = passage
	return passage, nil
}

type mode4TestEventRepo struct {
	events []domain.LearningEvent
}

func (r *mode4TestEventRepo) Insert(ctx context.Context, event domain.LearningEvent) error {
	r.events = append(r.events, event)
	return nil
}
func (r *mode4TestEventRepo) ListRecentByPoolItem(ctx context.Context, itemID uuid.UUID) ([]domain.LearningEvent, error) {
	return nil, nil
}

type mode4TestLLMRepo struct {
	runs []domain.LLMGenerationRun
}

func (r *mode4TestLLMRepo) Insert(ctx context.Context, run domain.LLMGenerationRun) error {
	_, err := r.InsertReturning(ctx, run)
	return err
}
func (r *mode4TestLLMRepo) InsertReturning(ctx context.Context, run domain.LLMGenerationRun) (domain.LLMGenerationRun, error) {
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	r.runs = append(r.runs, run)
	return run, nil
}
func (r *mode4TestLLMRepo) CountByUserLocalDateAndPrompt(ctx context.Context, userID uuid.UUID, localDate string, prompt string) (int, error) {
	return 0, nil
}
func (r *mode4TestLLMRepo) ListRecentByUser(ctx context.Context, userID uuid.UUID, limit int) ([]domain.LLMGenerationRun, error) {
	return append([]domain.LLMGenerationRun(nil), r.runs...), nil
}

type mode4TestGenerator struct {
	calls   int
	inputs  []Mode4PassageGenerationInput
	payload domain.Mode4WeakPassagePayload
	raw     string
	err     error
}

func (g *mode4TestGenerator) GenerateMode4WeakPassage(ctx context.Context, input Mode4PassageGenerationInput) (domain.Mode4WeakPassagePayload, string, error) {
	g.calls++
	g.inputs = append(g.inputs, input)
	return g.payload, g.raw, g.err
}

func TestDetermineMode4WordCountHeuristics(t *testing.T) {
	t.Parallel()

	highRisk := []mode4Candidate{
		{state: domain.UserWordState{Difficulty: 0.90}},
		{state: domain.UserWordState{WeaknessScore: 2.10}},
		{state: domain.UserWordState{LastRating: domain.RatingHard}},
		{state: domain.UserWordState{Difficulty: 0.40}},
	}
	if got := determineMode4WordCount(highRisk); got != 4 {
		t.Fatalf("expected 4 words for high-risk group, got %d", got)
	}

	mediumRisk := []mode4Candidate{
		{state: domain.UserWordState{Difficulty: 0.70, LearningStage: 4}},
		{state: domain.UserWordState{WeaknessScore: 1.40, LearningStage: 4}},
		{state: domain.UserWordState{Status: domain.WordStatusLearning, LearningStage: 4}},
		{state: domain.UserWordState{Difficulty: 0.40, LearningStage: 4}},
		{state: domain.UserWordState{Difficulty: 0.30, LearningStage: 4}},
	}
	if got := determineMode4WordCount(mediumRisk); got != 5 {
		t.Fatalf("expected 5 words for medium-risk group, got %d", got)
	}

	easier := []mode4Candidate{
		{state: domain.UserWordState{Difficulty: 0.20, LearningStage: 4}},
		{state: domain.UserWordState{Difficulty: 0.25, LearningStage: 4}},
		{state: domain.UserWordState{Difficulty: 0.30, LearningStage: 4}},
		{state: domain.UserWordState{Difficulty: 0.35, LearningStage: 4}},
		{state: domain.UserWordState{Difficulty: 0.40, LearningStage: 4}},
		{state: domain.UserWordState{Difficulty: 0.45, LearningStage: 4}},
	}
	if got := determineMode4WordCount(easier); got != 6 {
		t.Fatalf("expected 6 words for easier group, got %d", got)
	}
}

func TestMode4SelectGenerationWordsOddUsesTopWeakest(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	service := newMode4ServiceForTest(
		time.Date(2026, 3, 24, 8, 0, 0, 0, time.UTC),
		&mode4TestWordRepo{},
		&mode4TestStateRepo{},
		&mode4TestReviewRepo{state: domain.Mode4ReviewState{UserID: userID}},
		&mode4TestEventRepo{},
		&mode4TestLLMRepo{},
		&mode4TestGenerator{},
	)
	candidates := buildMode4Candidates(userID, 6)

	selected, err := service.selectGenerationWords(context.Background(), userID, 1, candidates)
	if err != nil {
		t.Fatalf("selectGenerationWords() error = %v", err)
	}
	if len(selected) != 6 {
		t.Fatalf("expected 6 selected words, got %d", len(selected))
	}
	for index, candidate := range selected {
		if candidate.word.ID != candidates[index].word.ID {
			t.Fatalf("expected candidate %d to preserve rank order", index)
		}
	}
}

func TestMode4SelectGenerationWordsEvenDedupesOnlyAgainstImmediatePreviousGeneration(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	candidates := buildMode4Candidates(userID, 6)
	reviewRepo := &mode4TestReviewRepo{
		state:        domain.Mode4ReviewState{UserID: userID, GenerationCount: 3},
		passages:     map[uuid.UUID]domain.Mode4ReviewPassage{},
		generationID: map[int]uuid.UUID{},
	}

	generationTwo := domain.Mode4ReviewPassage{
		ID:               uuid.New(),
		UserID:           userID,
		GenerationNumber: 2,
		WordIDs:          []uuid.UUID{candidates[0].word.ID, candidates[1].word.ID},
		Status:           domain.Mode4ReviewPassageStatusCompleted,
	}
	generationThree := domain.Mode4ReviewPassage{
		ID:               uuid.New(),
		UserID:           userID,
		GenerationNumber: 3,
		WordIDs:          []uuid.UUID{candidates[2].word.ID, candidates[3].word.ID, candidates[4].word.ID, candidates[5].word.ID},
		Status:           domain.Mode4ReviewPassageStatusCompleted,
	}
	reviewRepo.passages[generationTwo.ID] = generationTwo
	reviewRepo.passages[generationThree.ID] = generationThree
	reviewRepo.generationID[2] = generationTwo.ID
	reviewRepo.generationID[3] = generationThree.ID

	service := newMode4ServiceForTest(
		time.Date(2026, 3, 24, 8, 0, 0, 0, time.UTC),
		&mode4TestWordRepo{},
		&mode4TestStateRepo{},
		reviewRepo,
		&mode4TestEventRepo{},
		&mode4TestLLMRepo{},
		&mode4TestGenerator{},
	)

	selected, err := service.selectGenerationWords(context.Background(), userID, 4, candidates)
	if err != nil {
		t.Fatalf("selectGenerationWords() error = %v", err)
	}
	if len(selected) != 6 {
		t.Fatalf("expected 6 selected words, got %d", len(selected))
	}
	if selected[0].word.ID != candidates[0].word.ID || selected[1].word.ID != candidates[1].word.ID {
		t.Fatalf("expected words reused from generation 2 to stay eligible; got first IDs %s %s", selected[0].word.ID, selected[1].word.ID)
	}
}

func TestMode4SelectGenerationWordsEvenFallsBackToOverlapWhenNeeded(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	candidates := buildMode4Candidates(userID, 6)
	reviewRepo := &mode4TestReviewRepo{
		state:        domain.Mode4ReviewState{UserID: userID, GenerationCount: 1},
		passages:     map[uuid.UUID]domain.Mode4ReviewPassage{},
		generationID: map[int]uuid.UUID{},
	}

	previous := domain.Mode4ReviewPassage{
		ID:               uuid.New(),
		UserID:           userID,
		GenerationNumber: 1,
		WordIDs:          []uuid.UUID{candidates[0].word.ID, candidates[1].word.ID, candidates[2].word.ID, candidates[3].word.ID, candidates[4].word.ID},
		Status:           domain.Mode4ReviewPassageStatusCompleted,
	}
	reviewRepo.passages[previous.ID] = previous
	reviewRepo.generationID[1] = previous.ID

	service := newMode4ServiceForTest(
		time.Date(2026, 3, 24, 8, 0, 0, 0, time.UTC),
		&mode4TestWordRepo{},
		&mode4TestStateRepo{},
		reviewRepo,
		&mode4TestEventRepo{},
		&mode4TestLLMRepo{},
		&mode4TestGenerator{},
	)

	selected, err := service.selectGenerationWords(context.Background(), userID, 2, candidates)
	if err != nil {
		t.Fatalf("selectGenerationWords() error = %v", err)
	}
	if len(selected) != 6 {
		t.Fatalf("expected fallback to reuse overlapping words, got %d", len(selected))
	}
	if selected[0].word.ID != candidates[5].word.ID {
		t.Fatalf("expected first non-overlapping word to be selected first, got %s", selected[0].word.ID)
	}
}

func TestMode4CompleteDoneUsesLatestRatingsAndLeavesUnratedAsNoRating(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 24, 9, 0, 0, 0, time.UTC)
	userID := uuid.New()
	wordRepo, stateRepo, reviewRepo, eventRepo, llmRepo, generator, passage := buildMode4CompletionFixture(userID, now)
	service := newMode4ServiceForTest(now, wordRepo, stateRepo, reviewRepo, eventRepo, llmRepo, generator)

	err := service.Complete(context.Background(), domain.User{ID: userID}, Mode4CompletionRequest{
		PassageID:      passage.ID,
		Action:         domain.Mode4ReviewActionDone,
		ResponseTimeMs: 2300,
		ClientEventID:  "client-1",
		Ratings: []Mode4WordRatingInput{
			{WordID: passage.WordIDs[0], Rating: domain.RatingHard},
			{WordID: passage.WordIDs[1], Rating: domain.RatingMedium},
			{WordID: passage.WordIDs[0], Rating: domain.RatingEasy},
		},
	})
	if err != nil {
		t.Fatalf("Complete(done) error = %v", err)
	}

	if len(stateRepo.upserts) != 2 {
		t.Fatalf("expected 2 rated word state upserts, got %d", len(stateRepo.upserts))
	}
	if got := stateRepo.states[passage.WordIDs[0]].LastRating; got != domain.RatingEasy {
		t.Fatalf("expected latest rating easy for first word, got %s", got)
	}
	if got := stateRepo.states[passage.WordIDs[1]].LastRating; got != domain.RatingMedium {
		t.Fatalf("expected medium rating for second word, got %s", got)
	}
	if got := stateRepo.states[passage.WordIDs[2]].LastRating; got != "" {
		t.Fatalf("expected unrated word state to stay unchanged, got %s", got)
	}

	updatedPassage, err := reviewRepo.GetPassage(context.Background(), userID, passage.ID)
	if err != nil {
		t.Fatalf("GetPassage() error = %v", err)
	}
	if updatedPassage.Status != domain.Mode4ReviewPassageStatusCompleted {
		t.Fatalf("expected completed passage, got %s", updatedPassage.Status)
	}
	if reviewRepo.state.ActivePassageID != nil {
		t.Fatalf("expected active passage to clear after done")
	}
	if reviewRepo.state.NextEligibleAt == nil || reviewRepo.state.NextEligibleAt.Sub(now) != 3*time.Hour {
		t.Fatalf("expected next eligible at +3h, got %#v", reviewRepo.state.NextEligibleAt)
	}
	if len(eventRepo.events) != 3 {
		t.Fatalf("expected 1 passage event and 2 word-rating events, got %d", len(eventRepo.events))
	}
	finalRatings, _ := eventRepo.events[0].Payload["final_ratings"].(map[string]string)
	if finalRatings[passage.WordIDs[0].String()] != string(domain.RatingEasy) {
		t.Fatalf("expected final rating easy for first word, got %+v", finalRatings)
	}
	if finalRatings[passage.WordIDs[2].String()] != string(domain.RatingNone) {
		t.Fatalf("expected no_rating for unrated word, got %+v", finalRatings)
	}
}

func TestMode4CompleteSkipDiscardsRatingsAndKeepsPassageActive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 24, 9, 0, 0, 0, time.UTC)
	userID := uuid.New()
	wordRepo, stateRepo, reviewRepo, eventRepo, llmRepo, generator, passage := buildMode4CompletionFixture(userID, now)
	service := newMode4ServiceForTest(now, wordRepo, stateRepo, reviewRepo, eventRepo, llmRepo, generator)

	err := service.Complete(context.Background(), domain.User{ID: userID}, Mode4CompletionRequest{
		PassageID:      passage.ID,
		Action:         domain.Mode4ReviewActionSkip,
		ResponseTimeMs: 1400,
		ClientEventID:  "client-skip",
		Ratings: []Mode4WordRatingInput{
			{WordID: passage.WordIDs[0], Rating: domain.RatingHard},
		},
	})
	if err != nil {
		t.Fatalf("Complete(skip) error = %v", err)
	}

	if len(stateRepo.upserts) != 0 {
		t.Fatalf("expected skip to avoid per-word state upserts, got %d", len(stateRepo.upserts))
	}
	updatedPassage, err := reviewRepo.GetPassage(context.Background(), userID, passage.ID)
	if err != nil {
		t.Fatalf("GetPassage() error = %v", err)
	}
	if updatedPassage.Status != domain.Mode4ReviewPassageStatusActive {
		t.Fatalf("expected skipped passage to remain active, got %s", updatedPassage.Status)
	}
	if updatedPassage.SkipCount != 1 {
		t.Fatalf("expected skip_count=1, got %d", updatedPassage.SkipCount)
	}
	if reviewRepo.state.ActivePassageID == nil || *reviewRepo.state.ActivePassageID != passage.ID {
		t.Fatalf("expected active passage to stay set after skip")
	}
	if reviewRepo.state.NextEligibleAt == nil || reviewRepo.state.NextEligibleAt.Sub(now) != 3*time.Hour {
		t.Fatalf("expected skip to delay by 3h, got %#v", reviewRepo.state.NextEligibleAt)
	}
	if len(eventRepo.events) != 1 {
		t.Fatalf("expected only one passage event for skip, got %d", len(eventRepo.events))
	}
	if _, ok := eventRepo.events[0].Payload["final_ratings"]; ok {
		t.Fatalf("expected skip payload to omit final_ratings")
	}
}

func TestMode4MaybeOverlayCardReusesActivePassageWithoutGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 24, 9, 0, 0, 0, time.UTC)
	userID := uuid.New()
	wordRepo, stateRepo, reviewRepo, eventRepo, llmRepo, generator, passage := buildMode4CompletionFixture(userID, now)
	service := newMode4ServiceForTest(now, wordRepo, stateRepo, reviewRepo, eventRepo, llmRepo, generator)

	card, err := service.MaybeOverlayCard(context.Background(), domain.User{ID: userID}, CardResponse{
		CardType:  domain.LearnCardTypePoolItem,
		LocalDate: "2026-03-24",
		PoolItem:  &domain.DailyLearningPoolItem{ID: uuid.New(), UserID: userID},
	})
	if err != nil {
		t.Fatalf("MaybeOverlayCard() error = %v", err)
	}
	if card.CardType != domain.LearnCardTypeMode4WeakPassageReview || card.Mode4 == nil {
		t.Fatalf("expected mode4 overlay response, got %+v", card)
	}
	if card.Mode4.PassageID != passage.ID {
		t.Fatalf("expected active passage %s, got %s", passage.ID, card.Mode4.PassageID)
	}
	if generator.calls != 0 {
		t.Fatalf("expected active passage reuse without generator call, got %d calls", generator.calls)
	}
}

func TestMode4MaybeOverlayCardGeneratesWhenEligibleAndNoActivePassage(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 24, 9, 0, 0, 0, time.UTC)
	userID := uuid.New()
	words, states := buildMode4WordAndStates(userID, 4)
	wordRepo := &mode4TestWordRepo{words: words}
	stateRepo := &mode4TestStateRepo{states: states, mode4Candidates: orderedStates(states)}
	reviewRepo := &mode4TestReviewRepo{
		state:        domain.Mode4ReviewState{UserID: userID, GenerationCount: 1},
		passages:     map[uuid.UUID]domain.Mode4ReviewPassage{},
		generationID: map[int]uuid.UUID{},
	}
	eventRepo := &mode4TestEventRepo{}
	llmRepo := &mode4TestLLMRepo{}
	generator := &mode4TestGenerator{
		payload: domain.Mode4WeakPassagePayload{
			PlainPassageText:      "Alpha met beta before gamma helped delta.",
			MarkedPassageMarkdown: "**alpha** met **beta** before **gamma** helped **delta**.",
		},
		raw: `{"plain_passage_text":"Alpha met beta before gamma helped delta.","marked_passage_markdown":"**alpha** met **beta** before **gamma** helped **delta**."}`,
	}
	service := newMode4ServiceForTest(now, wordRepo, stateRepo, reviewRepo, eventRepo, llmRepo, generator)

	card, err := service.MaybeOverlayCard(context.Background(), domain.User{ID: userID}, CardResponse{
		CardType:  domain.LearnCardTypePoolItem,
		LocalDate: "2026-03-24",
		PoolItem:  &domain.DailyLearningPoolItem{ID: uuid.New(), UserID: userID},
	})
	if err != nil {
		t.Fatalf("MaybeOverlayCard() error = %v", err)
	}
	if generator.calls != 1 {
		t.Fatalf("expected generator call, got %d", generator.calls)
	}
	if card.Mode4 == nil || card.CardType != domain.LearnCardTypeMode4WeakPassageReview {
		t.Fatalf("expected generated mode4 card, got %+v", card)
	}
	if reviewRepo.state.GenerationCount != 2 {
		t.Fatalf("expected generation count to increment to 2, got %d", reviewRepo.state.GenerationCount)
	}
	if reviewRepo.state.ActivePassageID == nil {
		t.Fatalf("expected newly generated active passage")
	}
	if len(llmRepo.runs) != 1 || llmRepo.runs[0].AcceptedCount != 4 {
		t.Fatalf("expected one successful llm run with accepted_count=4, got %+v", llmRepo.runs)
	}
}

func newMode4ServiceForTest(
	now time.Time,
	wordRepo WordRepository,
	stateRepo WordStateRepository,
	reviewRepo Mode4ReviewRepository,
	eventRepo LearningEventRepository,
	llmRepo LLMRunRepository,
	generator Mode4PassageGenerator,
) *WeakPassageReviewService {
	return NewWeakPassageReviewService(
		wordRepo,
		stateRepo,
		reviewRepo,
		eventRepo,
		llmRepo,
		generator,
		mode4TestClock{now: now},
		slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	)
}

func buildMode4Candidates(userID uuid.UUID, count int) []mode4Candidate {
	candidates := make([]mode4Candidate, 0, count)
	for index := 0; index < count; index++ {
		wordID := uuid.New()
		label := mode4WordLabel(index)
		candidates = append(candidates, mode4Candidate{
			state: domain.UserWordState{
				UserID:        userID,
				WordID:        wordID,
				Status:        domain.WordStatusReview,
				Difficulty:    0.20 + float64(index)*0.02,
				WeaknessScore: 0.30 + float64(index)*0.01,
				LearningStage: 4,
			},
			word: buildMode4Word(wordID, label),
		})
	}
	return candidates
}

func buildMode4CompletionFixture(userID uuid.UUID, now time.Time) (*mode4TestWordRepo, *mode4TestStateRepo, *mode4TestReviewRepo, *mode4TestEventRepo, *mode4TestLLMRepo, *mode4TestGenerator, domain.Mode4ReviewPassage) {
	words, states := buildMode4WordAndStates(userID, 3)
	wordIDs := make([]uuid.UUID, 0, len(words))
	for id := range words {
		wordIDs = append(wordIDs, id)
	}
	ordered := orderedWordIDs(words)
	sourceWords := make([]domain.Mode4ReviewSourceWord, 0, len(ordered))
	for _, wordID := range ordered {
		word := words[wordID]
		state := states[wordID]
		sourceWords = append(sourceWords, domain.Mode4ReviewSourceWord{
			WordID:         word.ID,
			Word:           word.Word,
			NormalizedForm: word.NormalizedForm,
			Topic:          word.Topic,
			Level:          word.Level,
			WeaknessScore:  state.WeaknessScore,
			NextReviewAt:   state.NextReviewAt,
			Status:         state.Status,
			LastRating:     state.LastRating,
			Difficulty:     state.Difficulty,
			LearningStage:  state.LearningStage,
		})
	}
	passage := domain.Mode4ReviewPassage{
		ID:                    uuid.New(),
		UserID:                userID,
		GenerationNumber:      1,
		WordIDs:               ordered,
		SourceWords:           sourceWords,
		PlainPassageText:      "Alpha met beta before gamma.",
		MarkedPassageMarkdown: "**alpha** met **beta** before **gamma**.",
		PassageSpans: []domain.Mode4PassageSpan{
			{Text: "alpha", WordID: &ordered[0], TargetWord: words[ordered[0]].Word},
			{Text: " met "},
			{Text: "beta", WordID: &ordered[1], TargetWord: words[ordered[1]].Word},
			{Text: " before "},
			{Text: "gamma", WordID: &ordered[2], TargetWord: words[ordered[2]].Word},
			{Text: "."},
		},
		Status: domain.Mode4ReviewPassageStatusActive,
	}
	reviewRepo := &mode4TestReviewRepo{
		state: domain.Mode4ReviewState{
			UserID:          userID,
			GenerationCount: 1,
			ActivePassageID: &passage.ID,
		},
		passages: map[uuid.UUID]domain.Mode4ReviewPassage{
			passage.ID: passage,
		},
		generationID: map[int]uuid.UUID{
			1: passage.ID,
		},
	}
	return &mode4TestWordRepo{words: words}, &mode4TestStateRepo{states: states}, reviewRepo, &mode4TestEventRepo{}, &mode4TestLLMRepo{}, &mode4TestGenerator{}, passage
}

func buildMode4WordAndStates(userID uuid.UUID, count int) (map[uuid.UUID]domain.Word, map[uuid.UUID]domain.UserWordState) {
	words := map[uuid.UUID]domain.Word{}
	states := map[uuid.UUID]domain.UserWordState{}
	now := time.Date(2026, 3, 24, 9, 0, 0, 0, time.UTC)
	for index := 0; index < count; index++ {
		wordID := uuid.New()
		label := mode4WordLabel(index)
		word := buildMode4Word(wordID, label)
		nextReview := now.Add(time.Duration(index+1) * time.Hour)
		words[wordID] = word
		states[wordID] = domain.UserWordState{
			UserID:        userID,
			WordID:        wordID,
			Status:        domain.WordStatusReview,
			NextReviewAt:  &nextReview,
			WeaknessScore: 0.5 + float64(index)*0.1,
			Difficulty:    0.35 + float64(index)*0.05,
			LearningStage: 4,
		}
	}
	return words, states
}

func orderedStates(states map[uuid.UUID]domain.UserWordState) []domain.UserWordState {
	ids := orderedWordIDsFromStates(states)
	out := make([]domain.UserWordState, 0, len(ids))
	for _, id := range ids {
		out = append(out, states[id])
	}
	return out
}

func orderedWordIDs(words map[uuid.UUID]domain.Word) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(words))
	for id := range words {
		ids = append(ids, id)
	}
	for left := 0; left < len(ids); left++ {
		for right := left + 1; right < len(ids); right++ {
			if words[ids[left]].Word > words[ids[right]].Word {
				ids[left], ids[right] = ids[right], ids[left]
			}
		}
	}
	return ids
}

func orderedWordIDsFromStates(states map[uuid.UUID]domain.UserWordState) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(states))
	for id := range states {
		ids = append(ids, id)
	}
	for left := 0; left < len(ids); left++ {
		for right := left + 1; right < len(ids); right++ {
			leftTime := states[ids[left]].NextReviewAt
			rightTime := states[ids[right]].NextReviewAt
			if leftTime != nil && rightTime != nil && leftTime.After(*rightTime) {
				ids[left], ids[right] = ids[right], ids[left]
			}
		}
	}
	return ids
}

func buildMode4Word(wordID uuid.UUID, label string) domain.Word {
	return domain.Word{
		ID:                wordID,
		Word:              label,
		NormalizedForm:    NormalizeWord(label),
		CanonicalForm:     label,
		Lemma:             label,
		PartOfSpeech:      "noun",
		Level:             domain.CEFRB2,
		Topic:             "Business",
		IPA:               "/" + label + "/",
		EnglishMeaning:    label + " meaning",
		VietnameseMeaning: label + " nghia",
		ExampleSentence1:  "Example " + label,
		ExampleSentence2:  "Second " + label,
	}
}

func mode4WordLabel(index int) string {
	labels := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
	if index < len(labels) {
		return labels[index]
	}
	return "word" + uuid.NewString()
}
