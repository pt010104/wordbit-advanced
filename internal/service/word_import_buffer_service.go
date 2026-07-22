package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

const importBufferTopic = "Browser import"

type WordImportBufferAddInput struct {
	Word      string
	WordSetID uuid.UUID
	SourceURL string
}

type WordImportBufferService struct {
	repo       WordImportBufferRepository
	wordSets   WordSetRepository
	settings   SettingsRepository
	dictionary *DictionaryService
	generator  CandidateGenerator
	pools      PoolRepository
	clock      Clock
}

func NewWordImportBufferService(
	repo WordImportBufferRepository,
	wordSets WordSetRepository,
	settings SettingsRepository,
	dictionary *DictionaryService,
	generator CandidateGenerator,
	pools PoolRepository,
	clock Clock,
) *WordImportBufferService {
	return &WordImportBufferService{
		repo:       repo,
		wordSets:   wordSets,
		settings:   settings,
		dictionary: dictionary,
		generator:  generator,
		pools:      pools,
		clock:      clock,
	}
}

func (s *WordImportBufferService) List(ctx context.Context, userID uuid.UUID, setID *uuid.UUID) ([]domain.WordImportBufferItem, error) {
	if setID != nil {
		if _, err := s.wordSets.Get(ctx, userID, *setID); err != nil {
			return nil, err
		}
	}
	return s.repo.List(ctx, userID, setID)
}

func (s *WordImportBufferService) Add(ctx context.Context, userID uuid.UUID, input WordImportBufferAddInput) (domain.WordImportBufferItem, error) {
	word := strings.TrimSpace(input.Word)
	if word == "" {
		return domain.WordImportBufferItem{}, fmt.Errorf("%w: word is required", domain.ErrValidation)
	}
	if len([]rune(word)) > 100 {
		return domain.WordImportBufferItem{}, fmt.Errorf("%w: word is too long", domain.ErrValidation)
	}
	if _, err := s.wordSets.Get(ctx, userID, input.WordSetID); err != nil {
		return domain.WordImportBufferItem{}, err
	}
	return s.repo.Create(ctx, domain.WordImportBufferItem{
		UserID:    userID,
		WordSetID: input.WordSetID,
		RawWord:   word,
		SourceURL: strings.TrimSpace(input.SourceURL),
	})
}

func (s *WordImportBufferService) Generate(ctx context.Context, user domain.User, itemID uuid.UUID) (domain.WordImportBufferItem, error) {
	if s.generator == nil {
		return domain.WordImportBufferItem{}, fmt.Errorf("%w: AI generator is unavailable", domain.ErrValidation)
	}
	item, err := s.repo.Get(ctx, user.ID, itemID)
	if err != nil {
		return domain.WordImportBufferItem{}, err
	}
	if _, err := s.wordSets.Get(ctx, user.ID, item.WordSetID); err != nil {
		return domain.WordImportBufferItem{}, err
	}
	settings, err := s.settings.Get(ctx, user.ID)
	if err != nil {
		return domain.WordImportBufferItem{}, err
	}
	candidates, _, err := s.generator.GenerateCandidates(ctx, GenerationInput{
		UserID:            user.ID,
		CEFRLevel:         settings.CEFRLevel,
		Topic:             importBufferTopic,
		RequestedCount:    1,
		PreferredLanguage: settings.PreferredMeaningLanguage,
		RequiredWord:      item.RawWord,
	})
	if err != nil {
		return domain.WordImportBufferItem{}, err
	}
	if len(candidates) == 0 {
		return domain.WordImportBufferItem{}, fmt.Errorf("%w: AI did not return a candidate", domain.ErrValidation)
	}
	candidate := prepareImportBufferCandidate(candidates[0], item.RawWord, item.SourceURL)
	if _, _, err := sanitizeDictionaryCandidate(candidateToDictionaryInput(candidate, item.WordSetID), settings.CEFRLevel); err != nil {
		return domain.WordImportBufferItem{}, err
	}
	return s.repo.UpdateCandidate(ctx, user.ID, itemID, candidate)
}

func (s *WordImportBufferService) SaveCandidate(ctx context.Context, userID uuid.UUID, itemID uuid.UUID, candidate domain.CandidateWord) (domain.WordImportBufferItem, error) {
	item, err := s.repo.Get(ctx, userID, itemID)
	if err != nil {
		return domain.WordImportBufferItem{}, err
	}
	settings, err := s.settings.Get(ctx, userID)
	if err != nil {
		return domain.WordImportBufferItem{}, err
	}
	candidate = prepareImportBufferCandidate(candidate, item.RawWord, item.SourceURL)
	if _, _, err := sanitizeDictionaryCandidate(candidateToDictionaryInput(candidate, item.WordSetID), settings.CEFRLevel); err != nil {
		return domain.WordImportBufferItem{}, err
	}
	return s.repo.UpdateCandidate(ctx, userID, itemID, candidate)
}

func (s *WordImportBufferService) Confirm(ctx context.Context, user domain.User, itemID uuid.UUID) (domain.DictionaryEntry, error) {
	item, err := s.repo.Get(ctx, user.ID, itemID)
	if err != nil {
		return domain.DictionaryEntry{}, err
	}
	if item.Candidate == nil {
		return domain.DictionaryEntry{}, fmt.Errorf("%w: generate or edit the word card before confirming", domain.ErrValidation)
	}
	entry, err := s.dictionary.Create(ctx, user, candidateToDictionaryInput(*item.Candidate, item.WordSetID))
	if err != nil {
		return domain.DictionaryEntry{}, err
	}
	_ = s.appendImmediateNewCard(ctx, user.ID, entry.Word.ID)
	if _, err := s.repo.MarkImported(ctx, user.ID, itemID); err != nil {
		return domain.DictionaryEntry{}, err
	}
	return entry, nil
}

func (s *WordImportBufferService) Delete(ctx context.Context, userID uuid.UUID, itemID uuid.UUID) error {
	return s.repo.Delete(ctx, userID, itemID)
}

func (s *WordImportBufferService) appendImmediateNewCard(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error {
	settings, err := s.settings.Get(ctx, userID)
	if err != nil {
		return err
	}
	localDate, _, _, _, err := domain.BoundsForLocalDate(s.clock.Now(), settings.Timezone)
	if err != nil {
		return err
	}
	pool, _, err := s.pools.GetByLocalDate(ctx, userID, localDate)
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	ordinal, err := s.pools.GetLastOrdinal(ctx, pool.ID)
	if err != nil {
		return err
	}
	if _, err := s.pools.AppendPoolItem(ctx, domain.DailyLearningPoolItem{
		PoolID:                pool.ID,
		UserID:                userID,
		WordID:                wordID,
		Ordinal:               ordinal + 1,
		ItemType:              domain.PoolItemTypeNew,
		ReviewMode:            domain.ReviewModeReveal,
		Status:                domain.PoolItemStatusPending,
		IsReview:              false,
		FirstExposureRequired: true,
		Metadata: domain.JSONMap{
			"source": "import_buffer",
		},
	}); err != nil {
		return err
	}
	return s.pools.IncrementNewCount(ctx, pool.ID, 1)
}

func prepareImportBufferCandidate(candidate domain.CandidateWord, rawWord string, sourceURL string) domain.CandidateWord {
	word := strings.TrimSpace(candidate.Word)
	if word == "" {
		word = strings.TrimSpace(rawWord)
	}
	candidate.Word = word
	if strings.TrimSpace(candidate.CanonicalForm) == "" {
		candidate.CanonicalForm = word
	}
	if strings.TrimSpace(candidate.Lemma) == "" {
		candidate.Lemma = candidate.CanonicalForm
	}
	if strings.TrimSpace(candidate.Topic) == "" {
		candidate.Topic = importBufferTopic
	}
	candidate.SourceProvider = "browser_import"
	candidate.SourceMetadata = domain.JSONMap{
		"source":     "import_buffer",
		"source_url": strings.TrimSpace(sourceURL),
	}
	candidate.NormalizedForm = NormalizeWord(candidate.Word)
	if strings.TrimSpace(candidate.ConfusableGroupKey) == "" {
		candidate.ConfusableGroupKey = ConfusableGroupFor(candidate.Word, candidate.CanonicalForm, candidate.Lemma)
	}
	return candidate
}

func candidateToDictionaryInput(candidate domain.CandidateWord, setID uuid.UUID) DictionaryUpsertInput {
	return DictionaryUpsertInput{
		Word:               candidate.Word,
		CanonicalForm:      candidate.CanonicalForm,
		Lemma:              candidate.Lemma,
		WordFamily:         candidate.WordFamily,
		ConfusableGroupKey: candidate.ConfusableGroupKey,
		PartOfSpeech:       candidate.PartOfSpeech,
		Level:              candidate.Level,
		Topic:              candidate.Topic,
		IPA:                candidate.IPA,
		PronunciationHint:  candidate.PronunciationHint,
		VietnameseMeaning:  candidate.VietnameseMeaning,
		EnglishMeaning:     candidate.EnglishMeaning,
		ExampleSentence1:   candidate.ExampleSentence1,
		ExampleSentence2:   candidate.ExampleSentence2,
		CommonRate:         candidate.CommonRate,
		SourceProvider:     candidate.SourceProvider,
		SourceMetadata:     candidate.SourceMetadata,
		ListStatus:         domain.DictionaryListStatusUnknown,
		WordSetID:          &setID,
	}
}
