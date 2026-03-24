package service

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"log/slog"

	"wordbit-advanced-app/backend/internal/domain"
)

type exerciseTestClock struct {
	now time.Time
}

func (c exerciseTestClock) Now() time.Time { return c.now }

type exerciseSettingsRepo struct {
	settings domain.UserSettings
}

func (r *exerciseSettingsRepo) Get(ctx context.Context, userID uuid.UUID) (domain.UserSettings, error) {
	return r.settings, nil
}

func (r *exerciseSettingsRepo) Upsert(ctx context.Context, settings domain.UserSettings) (domain.UserSettings, error) {
	r.settings = settings
	return settings, nil
}

type exerciseWordRepo struct {
	words map[uuid.UUID]domain.Word
}

func (r *exerciseWordRepo) UpsertWord(ctx context.Context, candidate domain.CandidateWord) (domain.Word, error) {
	return domain.Word{}, errors.New("not implemented")
}
func (r *exerciseWordRepo) GetByID(ctx context.Context, wordID uuid.UUID) (domain.Word, error) {
	word, ok := r.words[wordID]
	if !ok {
		return domain.Word{}, domain.ErrNotFound
	}
	return word, nil
}
func (r *exerciseWordRepo) UpdateWord(ctx context.Context, wordID uuid.UUID, candidate domain.CandidateWord) (domain.Word, error) {
	return domain.Word{}, errors.New("not implemented")
}
func (r *exerciseWordRepo) ListWordIDsSeenAsNew(ctx context.Context, userID uuid.UUID, since time.Time) ([]uuid.UUID, error) {
	return nil, nil
}
func (r *exerciseWordRepo) ListBankWords(ctx context.Context, userID uuid.UUID, level domain.CEFRLevel, topic string, excludeWordIDs []uuid.UUID, limit int) ([]domain.Word, error) {
	return nil, nil
}
func (r *exerciseWordRepo) ListWordsByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.Word, error) {
	words := make([]domain.Word, 0, len(ids))
	for _, id := range ids {
		if word, ok := r.words[id]; ok {
			words = append(words, word)
		}
	}
	return words, nil
}

type exerciseStateRepo struct {
	weakCandidates []domain.UserWordState
}

func (r *exerciseStateRepo) Get(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) (domain.UserWordState, error) {
	return domain.UserWordState{}, domain.ErrNotFound
}
func (r *exerciseStateRepo) ListDueWithinWindow(ctx context.Context, userID uuid.UUID, start time.Time, end time.Time, learningOnly bool) ([]domain.UserWordState, error) {
	return nil, nil
}
func (r *exerciseStateRepo) ListWeakCandidates(ctx context.Context, userID uuid.UUID, excludeWordIDs []uuid.UUID, limit int) ([]domain.UserWordState, error) {
	if limit <= 0 || limit >= len(r.weakCandidates) {
		return append([]domain.UserWordState(nil), r.weakCandidates...), nil
	}
	return append([]domain.UserWordState(nil), r.weakCandidates[:limit]...), nil
}
func (r *exerciseStateRepo) ListMode4Candidates(ctx context.Context, userID uuid.UUID, limit int) ([]domain.UserWordState, error) {
	if limit <= 0 || limit >= len(r.weakCandidates) {
		return append([]domain.UserWordState(nil), r.weakCandidates...), nil
	}
	return append([]domain.UserWordState(nil), r.weakCandidates[:limit]...), nil
}
func (r *exerciseStateRepo) ListExistingWords(ctx context.Context, userID uuid.UUID) ([]domain.UserWordState, error) {
	return nil, nil
}
func (r *exerciseStateRepo) ListDictionaryEntries(ctx context.Context, userID uuid.UUID, filter domain.DictionaryFilter, query string, limit int, offset int) ([]domain.DictionaryEntry, error) {
	return nil, nil
}
func (r *exerciseStateRepo) Upsert(ctx context.Context, state domain.UserWordState) (domain.UserWordState, error) {
	return state, nil
}
func (r *exerciseStateRepo) Delete(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error {
	return nil
}
func (r *exerciseStateRepo) RefreshWeaknessScores(ctx context.Context, userID uuid.UUID) error {
	return nil
}

type exercisePackRepo struct {
	byCluster map[string]domain.ContextExercisePack
	latest    map[string]domain.ContextExercisePack
	created   []domain.ContextExercisePack
}

func (r *exercisePackRepo) GetByClusterHash(ctx context.Context, userID uuid.UUID, localDate string, clusterHash string, packType domain.ExercisePackType) (domain.ContextExercisePack, error) {
	pack, ok := r.byCluster[exercisePackKey(userID, localDate, clusterHash, packType)]
	if !ok {
		return domain.ContextExercisePack{}, domain.ErrNotFound
	}
	return pack, nil
}

func (r *exercisePackRepo) GetLatestReadyByLocalDate(ctx context.Context, userID uuid.UUID, localDate string, packType domain.ExercisePackType) (domain.ContextExercisePack, error) {
	pack, ok := r.latest[exerciseLatestKey(userID, localDate, packType)]
	if !ok {
		return domain.ContextExercisePack{}, domain.ErrNotFound
	}
	return pack, nil
}

func (r *exercisePackRepo) Create(ctx context.Context, pack domain.ContextExercisePack) (domain.ContextExercisePack, error) {
	if r.byCluster == nil {
		r.byCluster = map[string]domain.ContextExercisePack{}
	}
	if r.latest == nil {
		r.latest = map[string]domain.ContextExercisePack{}
	}
	r.byCluster[exercisePackKey(*pack.UserID, pack.LocalDate, pack.ClusterHash, pack.PackType)] = pack
	r.latest[exerciseLatestKey(*pack.UserID, pack.LocalDate, pack.PackType)] = pack
	r.created = append(r.created, pack)
	return pack, nil
}

type exerciseLLMRepo struct {
	count int
	runs  []domain.LLMGenerationRun
}

func (r *exerciseLLMRepo) Insert(ctx context.Context, run domain.LLMGenerationRun) error {
	_, err := r.InsertReturning(ctx, run)
	return err
}

func (r *exerciseLLMRepo) InsertReturning(ctx context.Context, run domain.LLMGenerationRun) (domain.LLMGenerationRun, error) {
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	r.runs = append(r.runs, run)
	return run, nil
}

func (r *exerciseLLMRepo) CountByUserLocalDateAndPrompt(ctx context.Context, userID uuid.UUID, localDate string, prompt string) (int, error) {
	if prompt != exerciseGenerationPrompt {
		return 0, nil
	}
	return r.count, nil
}

func (r *exerciseLLMRepo) ListRecentByUser(ctx context.Context, userID uuid.UUID, limit int) ([]domain.LLMGenerationRun, error) {
	return append([]domain.LLMGenerationRun(nil), r.runs...), nil
}

type exerciseGenerator struct {
	calls   int
	builder func(input ExercisePackGenerationInput) (domain.ContextExercisePayload, string, error)
}

func (g *exerciseGenerator) GenerateContextExercisePack(ctx context.Context, input ExercisePackGenerationInput) (domain.ContextExercisePayload, string, error) {
	g.calls++
	return g.builder(input)
}

func TestBuildExerciseClusterHashStableRegardlessOfOrder(t *testing.T) {
	t.Parallel()

	wordA := domain.Word{Word: "Allocate", NormalizedForm: "allocate", PartOfSpeech: "verb"}
	wordB := domain.Word{Word: "Forecast", NormalizedForm: "forecast", PartOfSpeech: "noun"}
	wordC := domain.Word{Word: "Revenue", NormalizedForm: "revenue", PartOfSpeech: "noun"}

	first := buildExerciseClusterHash([]domain.Word{wordA, wordB, wordC})
	second := buildExerciseClusterHash([]domain.Word{wordC, wordA, wordB})
	if first != second {
		t.Fatalf("expected stable cluster hash, got %q and %q", first, second)
	}
}

func TestExerciseServiceSelectWeakClusterUsesDeterministicWeakWordRanking(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	now := time.Date(2026, 3, 22, 8, 0, 0, 0, time.UTC)
	candidates, wordRepo, stateRepo := buildExerciseCandidates(userID, now)

	service := newExerciseServiceForTest(now, wordRepo, stateRepo, &exercisePackRepo{}, &exerciseLLMRepo{}, &exerciseGenerator{
		builder: makeValidExercisePayload,
	})
	selected, err := service.selectWeakCluster(context.Background(), userID)
	if err != nil {
		t.Fatalf("selectWeakCluster() error = %v", err)
	}
	if len(selected) != 5 {
		t.Fatalf("expected default cluster size 5, got %d", len(selected))
	}
	if selected[0].word.Word != candidates[0].word.Word {
		t.Fatalf("expected highest-priority review word %q first, got %q", candidates[0].word.Word, selected[0].word.Word)
	}
	for _, candidate := range selected {
		if candidate.word.Topic != "Business" {
			t.Fatalf("expected topic clustering around Business, got topic %q", candidate.word.Topic)
		}
	}
}

func TestExerciseServiceStartSessionReturnsInsufficientWordsWhenEligibleWordsBelowMinimum(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	now := time.Date(2026, 3, 22, 8, 0, 0, 0, time.UTC)
	wordRepo := &exerciseWordRepo{words: map[uuid.UUID]domain.Word{}}
	stateRepo := &exerciseStateRepo{}
	for _, word := range []string{"allocate", "forecast", "revenue"} {
		wordID := uuid.New()
		wordRepo.words[wordID] = testExerciseWord(wordID, word, "Business", domain.CEFRB2)
		stateRepo.weakCandidates = append(stateRepo.weakCandidates, testExerciseState(userID, wordID, domain.WordStatusReview, 0, 2.0, now))
	}
	generator := &exerciseGenerator{builder: makeValidExercisePayload}
	service := newExerciseServiceForTest(now, wordRepo, stateRepo, &exercisePackRepo{}, &exerciseLLMRepo{}, generator)

	response, err := service.StartSession(context.Background(), domain.User{ID: userID})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if response.State != domain.ExerciseSessionStateInsufficientWords {
		t.Fatalf("expected insufficient_words, got %q", response.State)
	}
	if response.Message != exerciseInsufficientWordsMessage {
		t.Fatalf("expected insufficient words message, got %q", response.Message)
	}
	if generator.calls != 0 {
		t.Fatalf("expected no generation when words are insufficient, got %d calls", generator.calls)
	}
}

func TestExerciseServiceStartSessionReusesExactSameDayPack(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	now := time.Date(2026, 3, 22, 8, 0, 0, 0, time.UTC)
	_, wordRepo, stateRepo := buildExerciseCandidates(userID, now)
	service := newExerciseServiceForTest(now, wordRepo, stateRepo, &exercisePackRepo{}, &exerciseLLMRepo{}, &exerciseGenerator{
		builder: makeValidExercisePayload,
	})
	selectedCandidates, err := service.selectWeakCluster(context.Background(), userID)
	if err != nil {
		t.Fatalf("selectWeakCluster() error = %v", err)
	}
	selectedWords := make([]domain.Word, 0, 5)
	for _, candidate := range selectedCandidates {
		selectedWords = append(selectedWords, candidate.word)
	}
	clusterHash := buildExerciseClusterHash(selectedWords)
	pack := readyExercisePack(userID, "2026-03-22", clusterHash, mustExercisePayload(t, ExercisePackGenerationInput{
		LocalDate:    "2026-03-22",
		Topic:        "Business",
		CEFRLevel:    domain.CEFRB2,
		ClusterWords: selectedWords,
	}))
	packRepo := &exercisePackRepo{
		byCluster: map[string]domain.ContextExercisePack{
			exercisePackKey(userID, "2026-03-22", clusterHash, domain.ExercisePackTypeContextClusterChallenge): pack,
		},
	}
	generator := &exerciseGenerator{builder: makeValidExercisePayload}
	service = newExerciseServiceForTest(now, wordRepo, stateRepo, packRepo, &exerciseLLMRepo{}, generator)

	response, err := service.StartSession(context.Background(), domain.User{ID: userID})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if response.State != domain.ExerciseSessionStateReady {
		t.Fatalf("expected ready state, got %q", response.State)
	}
	if response.Session == nil || !response.Session.Reused || response.Session.GeneratedNow {
		t.Fatalf("expected reused cached session metadata, got %+v", response.Session)
	}
	if generator.calls != 0 {
		t.Fatalf("expected cached pack reuse without generation, got %d calls", generator.calls)
	}
}

func TestExerciseServiceStartSessionAllowsSecondPackSameDayOnCacheMiss(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	now := time.Date(2026, 3, 22, 8, 0, 0, 0, time.UTC)
	_, wordRepo, stateRepo := buildExerciseCandidates(userID, now)
	packRepo := &exercisePackRepo{}
	llmRepo := &exerciseLLMRepo{count: 1}
	generator := &exerciseGenerator{builder: makeValidExercisePayload}
	service := newExerciseServiceForTest(now, wordRepo, stateRepo, packRepo, llmRepo, generator)

	response, err := service.StartSession(context.Background(), domain.User{ID: userID})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if response.State != domain.ExerciseSessionStateReady {
		t.Fatalf("expected ready state, got %q", response.State)
	}
	if response.Session == nil || !response.Session.GeneratedNow || response.Session.Reused {
		t.Fatalf("expected generated session metadata, got %+v", response.Session)
	}
	if generator.calls != 1 {
		t.Fatalf("expected exactly one generation call, got %d", generator.calls)
	}
	if len(packRepo.created) != 1 {
		t.Fatalf("expected one persisted pack, got %d", len(packRepo.created))
	}
}

func TestExerciseServiceStartSessionReusesLatestReadyPackWhenDailyCapReached(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	now := time.Date(2026, 3, 22, 8, 0, 0, 0, time.UTC)
	_, wordRepo, stateRepo := buildExerciseCandidates(userID, now)
	latestPayload := mustExercisePayload(t, ExercisePackGenerationInput{
		LocalDate: "2026-03-22",
		Topic:     "Business",
		CEFRLevel: domain.CEFRB2,
		ClusterWords: []domain.Word{
			testExerciseWord(uuid.New(), "allocate", "Business", domain.CEFRB2),
			testExerciseWord(uuid.New(), "forecast", "Business", domain.CEFRB2),
			testExerciseWord(uuid.New(), "revenue", "Business", domain.CEFRB2),
			testExerciseWord(uuid.New(), "strategy", "Business", domain.CEFRB2),
		},
	})
	packRepo := &exercisePackRepo{
		latest: map[string]domain.ContextExercisePack{
			exerciseLatestKey(userID, "2026-03-22", domain.ExercisePackTypeContextClusterChallenge): readyExercisePack(userID, "2026-03-22", "latest-hash", latestPayload),
		},
	}
	generator := &exerciseGenerator{builder: makeValidExercisePayload}
	service := newExerciseServiceForTest(now, wordRepo, stateRepo, packRepo, &exerciseLLMRepo{count: 2}, generator)

	response, err := service.StartSession(context.Background(), domain.User{ID: userID})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if response.State != domain.ExerciseSessionStateReady {
		t.Fatalf("expected ready state, got %q", response.State)
	}
	if response.Session == nil || !response.Session.Reused || response.Session.GeneratedNow {
		t.Fatalf("expected latest ready pack reuse metadata, got %+v", response.Session)
	}
	if generator.calls != 0 {
		t.Fatalf("expected no new generation after daily cap, got %d calls", generator.calls)
	}
}

func TestExerciseServiceStartSessionReturnsUnavailableWhenGeneratedPackIsInvalid(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	now := time.Date(2026, 3, 22, 8, 0, 0, 0, time.UTC)
	_, wordRepo, stateRepo := buildExerciseCandidates(userID, now)
	packRepo := &exercisePackRepo{}
	llmRepo := &exerciseLLMRepo{}
	generator := &exerciseGenerator{
		builder: func(input ExercisePackGenerationInput) (domain.ContextExercisePayload, string, error) {
			payload, _, _ := makeValidExercisePayload(input)
			payload.ClusterWords = payload.ClusterWords[:len(payload.ClusterWords)-1]
			return payload, "{}", nil
		},
	}
	service := newExerciseServiceForTest(now, wordRepo, stateRepo, packRepo, llmRepo, generator)

	response, err := service.StartSession(context.Background(), domain.User{ID: userID})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if response.State != domain.ExerciseSessionStateUnavailable {
		t.Fatalf("expected unavailable state, got %q", response.State)
	}
	if len(packRepo.created) != 0 {
		t.Fatalf("expected invalid pack not to persist, got %d created packs", len(packRepo.created))
	}
	if generator.calls != 1 {
		t.Fatalf("expected a single failed generation attempt, got %d", generator.calls)
	}
	if len(llmRepo.runs) != 1 {
		t.Fatalf("expected llm run to be recorded once, got %d", len(llmRepo.runs))
	}
}

func newExerciseServiceForTest(
	now time.Time,
	wordRepo *exerciseWordRepo,
	stateRepo *exerciseStateRepo,
	packRepo *exercisePackRepo,
	llmRepo *exerciseLLMRepo,
	generator *exerciseGenerator,
) *ExerciseService {
	userID := uuid.New()
	settings := domain.DefaultUserSettings(userID)
	settings.Timezone = "Asia/Ho_Chi_Minh"
	return NewExerciseService(
		&exerciseSettingsRepo{settings: settings},
		wordRepo,
		stateRepo,
		packRepo,
		llmRepo,
		generator,
		exerciseTestClock{now: now},
		slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	)
}

func buildExerciseCandidates(userID uuid.UUID, now time.Time) ([]exerciseCandidate, *exerciseWordRepo, *exerciseStateRepo) {
	wordRepo := &exerciseWordRepo{words: map[uuid.UUID]domain.Word{}}
	stateRepo := &exerciseStateRepo{}

	definitions := []struct {
		word          string
		topic         string
		level         domain.CEFRLevel
		status        domain.WordStatus
		learningStage int
		weakness      float64
		lastSeenHours int
	}{
		{"allocate", "Business", domain.CEFRB2, domain.WordStatusReview, 0, 2.8, 96},
		{"forecast", "Business", domain.CEFRB2, domain.WordStatusReview, 0, 2.6, 72},
		{"revenue", "Business", domain.CEFRB2, domain.WordStatusLearning, 3, 2.9, 48},
		{"strategy", "Business", domain.CEFRB2, domain.WordStatusLearning, 2, 2.4, 24},
		{"negotiate", "Business", domain.CEFRB2, domain.WordStatusReview, 0, 2.1, 12},
		{"dormant", "Health", domain.CEFRB2, domain.WordStatusReview, 0, 2.7, 36},
	}

	candidates := make([]exerciseCandidate, 0, len(definitions))
	for _, definition := range definitions {
		wordID := uuid.New()
		word := testExerciseWord(wordID, definition.word, definition.topic, definition.level)
		state := testExerciseState(userID, wordID, definition.status, definition.learningStage, definition.weakness, now.Add(-time.Duration(definition.lastSeenHours)*time.Hour))
		wordRepo.words[wordID] = word
		stateRepo.weakCandidates = append(stateRepo.weakCandidates, state)
		candidates = append(candidates, exerciseCandidate{state: state, word: word})
	}
	sort.Slice(candidates, func(i int, j int) bool {
		return compareExerciseCandidateRank(candidates[i], candidates[j]) < 0
	})
	return candidates, wordRepo, stateRepo
}

func testExerciseWord(wordID uuid.UUID, word string, topic string, level domain.CEFRLevel) domain.Word {
	return domain.Word{
		ID:                wordID,
		Word:              word,
		NormalizedForm:    NormalizeWord(word),
		CanonicalForm:     word,
		Lemma:             word,
		PartOfSpeech:      "noun",
		Level:             level,
		Topic:             topic,
		VietnameseMeaning: word + " vi",
		EnglishMeaning:    word + " en",
		ExampleSentence1:  "Example one for " + word,
		ExampleSentence2:  "Example two for " + word,
	}
}

func testExerciseState(userID uuid.UUID, wordID uuid.UUID, status domain.WordStatus, learningStage int, weakness float64, lastSeen time.Time) domain.UserWordState {
	return domain.UserWordState{
		UserID:        userID,
		WordID:        wordID,
		Status:        status,
		LearningStage: learningStage,
		WeaknessScore: weakness,
		LastSeenAt:    &lastSeen,
		CreatedAt:     lastSeen.Add(-24 * time.Hour),
	}
}

func makeValidExercisePayload(input ExercisePackGenerationInput) (domain.ContextExercisePayload, string, error) {
	clusterWords := make([]string, 0, len(input.ClusterWords))
	questions := make([]domain.ContextExerciseQuestion, 0, len(input.ClusterWords))
	for index, word := range input.ClusterWords {
		clusterWords = append(clusterWords, word.Word)
		questions = append(questions, domain.ContextExerciseQuestion{
			ID:          "q" + uuid.NewString()[:8],
			Type:        domain.ExerciseQuestionTypeBestFit,
			TargetWord:  word.Word,
			Prompt:      "Which word best fits the context for " + word.Word + "?",
			Options:     buildExerciseOptions(word.Word, clusterWords),
			Answer:      word.Word,
			Explanation: word.Word + " is the correct fit in context.",
		})
		if index >= exerciseMaxClusterSize-1 {
			break
		}
	}
	return domain.ContextExercisePayload{
		Topic:        input.Topic,
		CEFRLevel:    input.CEFRLevel,
		PackType:     domain.ExercisePackTypeContextClusterChallenge,
		ClusterWords: clusterWords,
		Title:        input.Topic + " Context Drill",
		Passage:      "A realistic passage that connects the cluster words in one scenario.",
		Questions:    questions,
		SummaryTip:   "Review the words together inside the same context.",
	}, "{}", nil
}

func buildExerciseOptions(answer string, clusterWords []string) []string {
	options := []string{answer}
	for _, word := range clusterWords {
		if word != answer {
			options = append(options, word)
		}
		if len(options) == 4 {
			return options
		}
	}
	for len(options) < 4 {
		options = append(options, answer+"_"+uuid.NewString()[:4])
	}
	return options
}

func readyExercisePack(userID uuid.UUID, localDate string, clusterHash string, payload domain.ContextExercisePayload) domain.ContextExercisePack {
	packID := uuid.New()
	payload.PackID = packID.String()
	return domain.ContextExercisePack{
		ID:          packID,
		UserID:      &userID,
		LocalDate:   localDate,
		Topic:       payload.Topic,
		CEFRLevel:   payload.CEFRLevel,
		PackType:    domain.ExercisePackTypeContextClusterChallenge,
		ClusterHash: clusterHash,
		Payload:     payload,
		Status:      domain.ExercisePackStatusReady,
	}
}

func mustExercisePayload(t *testing.T, input ExercisePackGenerationInput) domain.ContextExercisePayload {
	t.Helper()

	payload, _, err := makeValidExercisePayload(input)
	if err != nil {
		t.Fatalf("makeValidExercisePayload() error = %v", err)
	}
	return payload
}

func exercisePackKey(userID uuid.UUID, localDate string, clusterHash string, packType domain.ExercisePackType) string {
	return userID.String() + "|" + localDate + "|" + clusterHash + "|" + string(packType)
}

func exerciseLatestKey(userID uuid.UUID, localDate string, packType domain.ExercisePackType) string {
	return userID.String() + "|" + localDate + "|" + string(packType)
}
