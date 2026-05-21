package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

type WordSetService struct {
	wordSets WordSetRepository
	settings SettingsRepository
	states   WordStateRepository
}

func NewWordSetService(wordSets WordSetRepository, settings SettingsRepository, states WordStateRepository) *WordSetService {
	return &WordSetService{wordSets: wordSets, settings: settings, states: states}
}

type WordSetUpsertInput struct {
	Name string
	Icon string
	Mode domain.WordSetMode
}

func (s *WordSetService) List(ctx context.Context, userID uuid.UUID) ([]domain.WordSet, error) {
	if _, err := s.EnsureDefault(ctx, userID); err != nil {
		return nil, err
	}
	return s.wordSets.List(ctx, userID)
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
		UserID: userID,
		Name:   name,
		Icon:   strings.TrimSpace(input.Icon),
		Mode:   mode,
	})
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
