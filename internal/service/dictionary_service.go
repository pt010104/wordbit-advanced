package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

const manualDictionaryTopic = "Dictionary"

type DictionaryUpsertInput struct {
	Word               string
	CanonicalForm      string
	Lemma              string
	WordFamily         string
	ConfusableGroupKey string
	PartOfSpeech       string
	Level              domain.CEFRLevel
	Topic              string
	IPA                string
	PronunciationHint  string
	VietnameseMeaning  string
	EnglishMeaning     string
	ExampleSentence1   string
	ExampleSentence2   string
	ListStatus         domain.DictionaryListStatus
}

type DictionaryService struct {
	settingsRepo SettingsRepository
	wordRepo     WordRepository
	stateRepo    WordStateRepository
	poolRepo     PoolRepository
	clock        Clock
}

func NewDictionaryService(
	settingsRepo SettingsRepository,
	wordRepo WordRepository,
	stateRepo WordStateRepository,
	poolRepo PoolRepository,
	clock Clock,
) *DictionaryService {
	return &DictionaryService{
		settingsRepo: settingsRepo,
		wordRepo:     wordRepo,
		stateRepo:    stateRepo,
		poolRepo:     poolRepo,
		clock:        clock,
	}
}

func (s *DictionaryService) List(ctx context.Context, userID uuid.UUID, filter string, query string, limit int, offset int) (domain.DictionaryListResponse, error) {
	normalizedFilter, err := normalizeDictionaryFilter(filter)
	if err != nil {
		return domain.DictionaryListResponse{}, err
	}
	items, err := s.stateRepo.ListDictionaryEntries(ctx, userID, normalizedFilter, query, limit, offset)
	if err != nil {
		return domain.DictionaryListResponse{}, err
	}
	return domain.DictionaryListResponse{
		Filter: normalizedFilter,
		Items:  items,
	}, nil
}

func (s *DictionaryService) Create(ctx context.Context, user domain.User, input DictionaryUpsertInput) (domain.DictionaryEntry, error) {
	settings, err := s.settingsRepo.Get(ctx, user.ID)
	if err != nil {
		return domain.DictionaryEntry{}, err
	}

	candidate, listStatus, err := sanitizeDictionaryCandidate(input, settings.CEFRLevel)
	if err != nil {
		return domain.DictionaryEntry{}, err
	}

	word, err := s.wordRepo.UpsertWord(ctx, candidate)
	if err != nil {
		return domain.DictionaryEntry{}, err
	}
	if _, err := s.stateRepo.Get(ctx, user.ID, word.ID); err == nil {
		return domain.DictionaryEntry{}, fmt.Errorf("%w: word already exists in dictionary", domain.ErrValidation)
	} else if !isNotFound(err) {
		return domain.DictionaryEntry{}, err
	}

	state := s.initialStateForListStatus(user.ID, word.ID, listStatus)
	savedState, err := s.stateRepo.Upsert(ctx, state)
	if err != nil {
		return domain.DictionaryEntry{}, err
	}
	return dictionaryEntryFrom(word, savedState), nil
}

func (s *DictionaryService) Update(ctx context.Context, user domain.User, wordID uuid.UUID, input DictionaryUpsertInput) (domain.DictionaryEntry, error) {
	settings, err := s.settingsRepo.Get(ctx, user.ID)
	if err != nil {
		return domain.DictionaryEntry{}, err
	}

	state, err := s.stateRepo.Get(ctx, user.ID, wordID)
	if err != nil {
		return domain.DictionaryEntry{}, err
	}
	candidate, listStatus, err := sanitizeDictionaryCandidate(input, settings.CEFRLevel)
	if err != nil {
		return domain.DictionaryEntry{}, err
	}

	word, err := s.wordRepo.UpdateWord(ctx, wordID, candidate)
	if err != nil {
		return domain.DictionaryEntry{}, err
	}

	updatedState := s.applyListStatusChange(state, listStatus)
	savedState, err := s.stateRepo.Upsert(ctx, updatedState)
	if err != nil {
		return domain.DictionaryEntry{}, err
	}

	return dictionaryEntryFrom(word, savedState), nil
}

func (s *DictionaryService) Delete(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error {
	if err := s.stateRepo.Delete(ctx, userID, wordID); err != nil {
		return err
	}
	if err := s.poolRepo.DeleteItemsForUserWord(ctx, userID, wordID); err != nil {
		return err
	}
	return nil
}

func (s *DictionaryService) initialStateForListStatus(userID uuid.UUID, wordID uuid.UUID, listStatus domain.DictionaryListStatus) domain.UserWordState {
	now := s.clock.Now()
	state := domain.UserWordState{
		UserID:     userID,
		WordID:     wordID,
		Status:     domain.WordStatusLearning,
		Difficulty: 0.5,
		Stability:  0.5,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if listStatus == domain.DictionaryListStatusKnown {
		return ApplyFirstExposureKnown(state, now, 0)
	}
	return ApplyFirstExposureUnknown(state, now, 0)
}

func (s *DictionaryService) applyListStatusChange(state domain.UserWordState, listStatus domain.DictionaryListStatus) domain.UserWordState {
	now := s.clock.Now()
	switch listStatus {
	case domain.DictionaryListStatusKnown:
		if state.Status == domain.WordStatusKnown {
			return state
		}
		return ApplyFirstExposureKnown(state, now, 0)
	default:
		if state.Status != domain.WordStatusKnown {
			return state
		}
		return ApplyFirstExposureUnknown(state, now, 0)
	}
}

func sanitizeDictionaryCandidate(input DictionaryUpsertInput, defaultLevel domain.CEFRLevel) (domain.CandidateWord, domain.DictionaryListStatus, error) {
	word := strings.TrimSpace(input.Word)
	if word == "" {
		return domain.CandidateWord{}, "", fmt.Errorf("%w: word is required", domain.ErrValidation)
	}
	if strings.TrimSpace(input.EnglishMeaning) == "" {
		return domain.CandidateWord{}, "", fmt.Errorf("%w: english meaning is required", domain.ErrValidation)
	}
	if strings.TrimSpace(input.VietnameseMeaning) == "" {
		return domain.CandidateWord{}, "", fmt.Errorf("%w: vietnamese meaning is required", domain.ErrValidation)
	}

	level := input.Level
	if level == "" {
		level = defaultLevel
	}
	switch level {
	case domain.CEFRB1, domain.CEFRB2, domain.CEFRC1, domain.CEFRC2:
	default:
		return domain.CandidateWord{}, "", fmt.Errorf("%w: invalid level", domain.ErrValidation)
	}

	listStatus, err := normalizeDictionaryListStatus(string(input.ListStatus))
	if err != nil {
		return domain.CandidateWord{}, "", err
	}

	canonical := strings.TrimSpace(input.CanonicalForm)
	if canonical == "" {
		canonical = word
	}
	lemma := strings.TrimSpace(input.Lemma)
	if lemma == "" {
		lemma = canonical
	}
	topic := strings.TrimSpace(input.Topic)
	if topic == "" {
		topic = manualDictionaryTopic
	}

	candidate := domain.CandidateWord{
		Word:               word,
		CanonicalForm:      canonical,
		Lemma:              lemma,
		WordFamily:         strings.TrimSpace(input.WordFamily),
		ConfusableGroupKey: strings.TrimSpace(input.ConfusableGroupKey),
		PartOfSpeech:       strings.TrimSpace(input.PartOfSpeech),
		Level:              level,
		Topic:              topic,
		IPA:                strings.TrimSpace(input.IPA),
		PronunciationHint:  strings.TrimSpace(input.PronunciationHint),
		VietnameseMeaning:  strings.TrimSpace(input.VietnameseMeaning),
		EnglishMeaning:     strings.TrimSpace(input.EnglishMeaning),
		ExampleSentence1:   strings.TrimSpace(input.ExampleSentence1),
		ExampleSentence2:   strings.TrimSpace(input.ExampleSentence2),
		SourceProvider:     "manual",
		SourceMetadata: domain.JSONMap{
			"source": "dictionary_manual",
		},
	}
	candidate.NormalizedForm = NormalizeWord(candidate.Word)
	if candidate.ConfusableGroupKey == "" {
		candidate.ConfusableGroupKey = ConfusableGroupFor(candidate.Word, candidate.CanonicalForm, candidate.Lemma)
	}
	return candidate, listStatus, nil
}

func normalizeDictionaryFilter(value string) (domain.DictionaryFilter, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(domain.DictionaryFilterUnknown):
		return domain.DictionaryFilterUnknown, nil
	case string(domain.DictionaryFilterKnown):
		return domain.DictionaryFilterKnown, nil
	case string(domain.DictionaryFilterAll):
		return domain.DictionaryFilterAll, nil
	default:
		return "", fmt.Errorf("%w: invalid dictionary filter", domain.ErrValidation)
	}
}

func normalizeDictionaryListStatus(value string) (domain.DictionaryListStatus, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(domain.DictionaryListStatusUnknown):
		return domain.DictionaryListStatusUnknown, nil
	case string(domain.DictionaryListStatusKnown):
		return domain.DictionaryListStatusKnown, nil
	default:
		return "", fmt.Errorf("%w: invalid dictionary status", domain.ErrValidation)
	}
}

func dictionaryEntryFrom(word domain.Word, state domain.UserWordState) domain.DictionaryEntry {
	return domain.DictionaryEntry{
		Word:           word,
		ListStatus:     listStatusForState(state.Status),
		LearningStatus: state.Status,
		FirstSeenAt:    state.FirstSeenAt,
		LastSeenAt:     state.LastSeenAt,
		KnownAt:        state.KnownAt,
		NextReviewAt:   state.NextReviewAt,
		ReviewCount:    state.ReviewCount,
		WeaknessScore:  state.WeaknessScore,
		UpdatedAt:      maxTime(state.UpdatedAt, word.UpdatedAt),
	}
}

func listStatusForState(status domain.WordStatus) domain.DictionaryListStatus {
	if status == domain.WordStatusKnown {
		return domain.DictionaryListStatusKnown
	}
	return domain.DictionaryListStatusUnknown
}

func maxTime(a time.Time, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
