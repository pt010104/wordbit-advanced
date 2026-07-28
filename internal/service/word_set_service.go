package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

type WordSetService struct {
	wordSets WordSetRepository
	settings SettingsRepository
	states   WordStateRepository
	clock    Clock
}

func NewWordSetService(wordSets WordSetRepository, settings SettingsRepository, states WordStateRepository, clock Clock) *WordSetService {
	return &WordSetService{wordSets: wordSets, settings: settings, states: states, clock: clock}
}

type WordSetUpsertInput struct {
	Name string
	Icon string
	Mode domain.WordSetMode
}

type WordSetPreferencesInput struct {
	AutoGenerateNewWords bool
	EnabledReviewModes   []domain.ReviewMode
}

// ReviewModePreferences contains both the enabled modes and the ownership
// context needed by the scheduler. Custom sets intentionally use a different
// progression from the Default/new-words set.
type ReviewModePreferences struct {
	EnabledModes []domain.ReviewMode
	IsCustomSet  bool
}

func (s *WordSetService) List(ctx context.Context, userID uuid.UUID) ([]domain.WordSet, error) {
	if _, err := s.EnsureDefault(ctx, userID); err != nil {
		return nil, err
	}
	sets, err := s.wordSets.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	defaultSet, err := s.wordSets.GetDefault(ctx, userID)
	if err != nil {
		return nil, err
	}
	dueCounts, err := s.computeDueCounts(ctx, userID, defaultSet.ID)
	if err != nil {
		return nil, err
	}
	for idx := range sets {
		sets[idx].DueCount = dueCounts[sets[idx].ID]
	}
	return sets, nil
}

func (s *WordSetService) EnsureDefault(ctx context.Context, userID uuid.UUID) (domain.WordSet, error) {
	set, err := s.wordSets.EnsureDefault(ctx, userID)
	if err != nil {
		return domain.WordSet{}, err
	}
	if err := s.states.BackfillDefaultWordSet(ctx, userID, set.ID); err != nil {
		return domain.WordSet{}, err
	}
	settings, err := s.settings.Get(ctx, userID)
	if err == nil && settings.ActiveWordSetID == nil {
		settings.ActiveWordSetID = &set.ID
		if _, err := s.settings.Upsert(ctx, settings); err != nil {
			return domain.WordSet{}, err
		}
	}
	return set, nil
}

func (s *WordSetService) Create(ctx context.Context, userID uuid.UUID, input WordSetUpsertInput) (domain.WordSet, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.WordSet{}, fmt.Errorf("%w: name is required", domain.ErrValidation)
	}
	mode, err := normalizeWordSetMode(input.Mode)
	if err != nil {
		return domain.WordSet{}, err
	}
	if _, err := s.EnsureDefault(ctx, userID); err != nil {
		return domain.WordSet{}, err
	}
	if mode == domain.WordSetModeNewWords {
		if err := s.ensureSoleNewWordsMode(ctx, userID, uuid.Nil); err != nil {
			return domain.WordSet{}, err
		}
	}
	return s.wordSets.Create(ctx, domain.WordSet{
		UserID:             userID,
		Name:               name,
		Icon:               strings.TrimSpace(input.Icon),
		Mode:               mode,
		EnabledReviewModes: allReviewModes(),
	})
}

func (s *WordSetService) UpdatePreferences(ctx context.Context, userID uuid.UUID, setID uuid.UUID, input WordSetPreferencesInput) (domain.WordSet, error) {
	set, err := s.wordSets.Get(ctx, userID, setID)
	if err != nil {
		return domain.WordSet{}, err
	}
	if input.AutoGenerateNewWords && !set.IsDefault {
		return domain.WordSet{}, fmt.Errorf("%w: only the default set can auto-generate new words", domain.ErrValidation)
	}
	modes, err := normalizeEnabledReviewModes(input.EnabledReviewModes)
	if err != nil {
		return domain.WordSet{}, err
	}
	set.AutoGenerateNewWords = input.AutoGenerateNewWords
	set.EnabledReviewModes = modes
	return s.wordSets.Update(ctx, set)
}

func (s *WordSetService) Update(ctx context.Context, userID uuid.UUID, setID uuid.UUID, input WordSetUpsertInput) (domain.WordSet, error) {
	existing, err := s.wordSets.Get(ctx, userID, setID)
	if err != nil {
		return domain.WordSet{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.WordSet{}, fmt.Errorf("%w: name is required", domain.ErrValidation)
	}
	mode, err := normalizeWordSetMode(input.Mode)
	if err != nil {
		return domain.WordSet{}, err
	}
	if mode == domain.WordSetModeNewWords && existing.Mode != domain.WordSetModeNewWords {
		if err := s.ensureSoleNewWordsMode(ctx, userID, setID); err != nil {
			return domain.WordSet{}, err
		}
	}
	existing.Name = name
	existing.Icon = strings.TrimSpace(input.Icon)
	existing.Mode = mode
	return s.wordSets.Update(ctx, existing)
}

func (s *WordSetService) Delete(ctx context.Context, userID uuid.UUID, setID uuid.UUID) error {
	existing, err := s.wordSets.Get(ctx, userID, setID)
	if err != nil {
		return err
	}
	if existing.IsDefault {
		return fmt.Errorf("%w: the default set cannot be deleted", domain.ErrValidation)
	}
	settings, err := s.settings.Get(ctx, userID)
	if err != nil {
		return err
	}
	if settings.ActiveWordSetID != nil && *settings.ActiveWordSetID == setID {
		defaultSet, err := s.wordSets.GetDefault(ctx, userID)
		if err != nil {
			return err
		}
		settings.ActiveWordSetID = &defaultSet.ID
		if _, err := s.settings.Upsert(ctx, settings); err != nil {
			return err
		}
	}
	return s.wordSets.Delete(ctx, userID, setID)
}

func (s *WordSetService) SetActive(ctx context.Context, userID uuid.UUID, setID uuid.UUID) (domain.UserSettings, error) {
	if _, err := s.wordSets.Get(ctx, userID, setID); err != nil {
		return domain.UserSettings{}, err
	}
	settings, err := s.settings.Get(ctx, userID)
	if err != nil {
		return domain.UserSettings{}, err
	}
	settings.ActiveWordSetID = &setID
	return s.settings.Upsert(ctx, settings)
}

func (s *WordSetService) ResolveActiveSet(ctx context.Context, userID uuid.UUID) (domain.WordSet, error) {
	settings, err := s.settings.Get(ctx, userID)
	if err != nil {
		return domain.WordSet{}, err
	}
	if settings.ActiveWordSetID != nil {
		if set, err := s.wordSets.Get(ctx, userID, *settings.ActiveWordSetID); err == nil {
			return set, nil
		}
	}
	return s.EnsureDefault(ctx, userID)
}

// EnabledReviewModesForWords resolves the preference of the set that owns
// each word. Legacy states without an owner keep the Default set's choices.
func (s *WordSetService) EnabledReviewModesForWords(ctx context.Context, userID uuid.UUID, wordIDs []uuid.UUID) (map[uuid.UUID][]domain.ReviewMode, error) {
	preferences, err := s.ReviewModePreferencesForWords(ctx, userID, wordIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID][]domain.ReviewMode, len(preferences))
	for wordID, preference := range preferences {
		result[wordID] = preference.EnabledModes
	}
	return result, nil
}

// ReviewModePreferencesForWords resolves set ownership as well as the enabled
// modes. States without an explicit owner retain Default-set behaviour.
func (s *WordSetService) ReviewModePreferencesForWords(ctx context.Context, userID uuid.UUID, wordIDs []uuid.UUID) (map[uuid.UUID]ReviewModePreferences, error) {
	defaultSet, err := s.EnsureDefault(ctx, userID)
	if err != nil {
		return nil, err
	}
	sets, err := s.wordSets.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]ReviewModePreferences{
		defaultSet.ID: {EnabledModes: defaultSet.EnabledReviewModes, IsCustomSet: defaultSet.Mode == domain.WordSetModeCustom},
	}
	for _, set := range sets {
		byID[set.ID] = ReviewModePreferences{EnabledModes: set.EnabledReviewModes, IsCustomSet: set.Mode == domain.WordSetModeCustom}
	}
	setIDs, err := s.states.GetWordSetIDsForWords(ctx, userID, wordIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]ReviewModePreferences, len(wordIDs))
	for _, wordID := range wordIDs {
		preference := byID[defaultSet.ID]
		if setID, ok := setIDs[wordID]; ok {
			if configured, found := byID[setID]; found {
				preference = configured
			}
		}
		result[wordID] = preference
	}
	return result, nil
}

func (s *WordSetService) ensureSoleNewWordsMode(ctx context.Context, userID uuid.UUID, excludeID uuid.UUID) error {
	sets, err := s.wordSets.List(ctx, userID)
	if err != nil {
		return err
	}
	for _, set := range sets {
		if set.Mode == domain.WordSetModeNewWords && set.ID != excludeID {
			return fmt.Errorf("%w: only one set can use the 'new_words' mode at a time", domain.ErrValidation)
		}
	}
	return nil
}

func normalizeWordSetMode(value domain.WordSetMode) (domain.WordSetMode, error) {
	switch domain.WordSetMode(strings.ToLower(strings.TrimSpace(string(value)))) {
	case "":
		return domain.WordSetModeCustom, nil
	case domain.WordSetModeNewWords:
		return domain.WordSetModeNewWords, nil
	case domain.WordSetModeCustom:
		return domain.WordSetModeCustom, nil
	default:
		return "", errors.New("invalid word set mode")
	}
}

func allReviewModes() []domain.ReviewMode {
	return []domain.ReviewMode{
		domain.ReviewModeReveal,
		domain.ReviewModeMultipleChoice,
		domain.ReviewModeBuildWord,
		domain.ReviewModeFillBlank,
		domain.ReviewModeListening,
		domain.ReviewModeDefinitionFirst,
	}
}

func normalizeEnabledReviewModes(input []domain.ReviewMode) ([]domain.ReviewMode, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("%w: at least Mode 1 must be enabled", domain.ErrValidation)
	}
	allowed := map[domain.ReviewMode]bool{}
	for _, mode := range allReviewModes() {
		allowed[mode] = true
	}
	seen := map[domain.ReviewMode]bool{}
	for _, mode := range input {
		if !allowed[mode] {
			return nil, fmt.Errorf("%w: invalid review mode", domain.ErrValidation)
		}
		seen[mode] = true
	}
	if !seen[domain.ReviewModeReveal] {
		return nil, fmt.Errorf("%w: Mode 1 must remain enabled", domain.ErrValidation)
	}
	modes := make([]domain.ReviewMode, 0, len(seen))
	for _, mode := range allReviewModes() {
		if seen[mode] {
			modes = append(modes, mode)
		}
	}
	return modes, nil
}

func (s *WordSetService) computeDueCounts(ctx context.Context, userID uuid.UUID, defaultSetID uuid.UUID) (map[uuid.UUID]int, error) {
	now := time.Now().UTC()
	if s.clock != nil {
		now = s.clock.Now()
	}

	shortTermStates, err := s.states.ListDueWithinWindow(ctx, userID, time.Time{}, now, true)
	if err != nil {
		return nil, err
	}
	reviewStates, err := s.states.ListDueWithinWindow(ctx, userID, time.Time{}, now, false)
	if err != nil {
		return nil, err
	}

	wordIDs := make([]uuid.UUID, 0, len(shortTermStates)+len(reviewStates))
	seenWords := make(map[uuid.UUID]struct{}, len(shortTermStates)+len(reviewStates))
	for _, state := range shortTermStates {
		if _, seen := seenWords[state.WordID]; seen {
			continue
		}
		seenWords[state.WordID] = struct{}{}
		wordIDs = append(wordIDs, state.WordID)
	}
	for _, state := range reviewStates {
		if _, seen := seenWords[state.WordID]; seen {
			continue
		}
		seenWords[state.WordID] = struct{}{}
		wordIDs = append(wordIDs, state.WordID)
	}
	if len(wordIDs) == 0 {
		return map[uuid.UUID]int{}, nil
	}

	setMap, err := s.states.GetWordSetIDsForWords(ctx, userID, wordIDs)
	if err != nil {
		return nil, err
	}

	counts := make(map[uuid.UUID]int)
	for _, wordID := range wordIDs {
		setID, ok := setMap[wordID]
		if !ok {
			setID = defaultSetID
		}
		counts[setID]++
	}
	return counts, nil
}
