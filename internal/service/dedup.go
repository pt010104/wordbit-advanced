package service

import (
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

type DedupResult struct {
	Accepted   []domain.CandidateWord
	Rejected   map[string][]string
	RejectList []string
}

func ValidateCandidate(candidate domain.CandidateWord) []string {
	var issues []string
	if strings.TrimSpace(candidate.Word) == "" {
		issues = append(issues, "missing word")
	}
	if candidate.Level == "" {
		issues = append(issues, "missing level")
	}
	if strings.TrimSpace(candidate.Topic) == "" {
		issues = append(issues, "missing topic")
	}
	if strings.TrimSpace(candidate.EnglishMeaning) == "" {
		issues = append(issues, "missing english meaning")
	}
	if strings.TrimSpace(candidate.VietnameseMeaning) == "" {
		issues = append(issues, "missing vietnamese meaning")
	}
	candidate.NormalizedForm = NormalizeWord(candidate.Word)
	if candidate.NormalizedForm == "" {
		issues = append(issues, "empty normalized form")
	}
	if candidate.CommonRate != nil {
		if _, ok := domain.ParseWordCommonRate(string(*candidate.CommonRate)); !ok {
			issues = append(issues, "invalid common rate")
		}
	}
	return issues
}

func FilterCandidates(
	candidates []domain.CandidateWord,
	existingStates []domain.UserWordState,
	existingWords []domain.Word,
	seenNewWordIDs []uuid.UUID,
	sameDayItems []domain.DailyLearningPoolItem,
) DedupResult {
	result := DedupResult{
		Accepted: []domain.CandidateWord{},
		Rejected: map[string][]string{},
	}

	existingNormalized := map[string]struct{}{}
	existingLemma := map[string]struct{}{}
	existingGroup := map[string]struct{}{}
	existingWordIDMap := map[uuid.UUID]struct{}{}

	for _, word := range existingWords {
		existingNormalized[word.NormalizedForm] = struct{}{}
		if word.CanonicalForm != "" {
			existingNormalized[NormalizeWord(word.CanonicalForm)] = struct{}{}
		}
		if word.Lemma != "" {
			existingLemma[NormalizeWord(word.Lemma)] = struct{}{}
		}
		if word.ConfusableGroupKey != "" {
			existingGroup[word.ConfusableGroupKey] = struct{}{}
		}
	}
	for _, state := range existingStates {
		existingWordIDMap[state.WordID] = struct{}{}
	}
	for _, item := range sameDayItems {
		if item.Word != nil {
			existingNormalized[item.Word.NormalizedForm] = struct{}{}
			if item.Word.ConfusableGroupKey != "" {
				existingGroup[item.Word.ConfusableGroupKey] = struct{}{}
			}
		}
		existingWordIDMap[item.WordID] = struct{}{}
	}
	for _, id := range seenNewWordIDs {
		existingWordIDMap[id] = struct{}{}
	}

	localNormalized := map[string]struct{}{}
	localGroups := map[string]struct{}{}
	localLemmas := map[string]struct{}{}

	for _, candidate := range candidates {
		if candidate.CommonRate != nil {
			trimmed := strings.TrimSpace(string(*candidate.CommonRate))
			if trimmed == "" {
				candidate.CommonRate = nil
			} else if normalized, ok := domain.ParseWordCommonRate(trimmed); ok {
				candidate.CommonRate = &normalized
			}
		}
		reasons := ValidateCandidate(candidate)
		normalized := NormalizeWord(candidate.Word)
		candidate.NormalizedForm = normalized
		if candidate.ConfusableGroupKey == "" {
			candidate.ConfusableGroupKey = ConfusableGroupFor(candidate.Word, candidate.CanonicalForm, candidate.Lemma)
		}
		lemma := NormalizeWord(candidate.Lemma)
		canonical := NormalizeWord(candidate.CanonicalForm)
		if _, exists := existingNormalized[normalized]; exists {
			reasons = append(reasons, "history normalized duplicate")
		}
		if canonical != "" {
			if _, exists := existingNormalized[canonical]; exists {
				reasons = append(reasons, "history canonical duplicate")
			}
		}
		if lemma != "" {
			if _, exists := existingLemma[lemma]; exists {
				reasons = append(reasons, "history lemma duplicate")
			}
		}
		if _, exists := localNormalized[normalized]; exists {
			reasons = append(reasons, "batch normalized duplicate")
		}
		if candidate.ConfusableGroupKey != "" {
			if _, exists := existingGroup[candidate.ConfusableGroupKey]; exists {
				reasons = append(reasons, "confusable group collision")
			}
			if _, exists := localGroups[candidate.ConfusableGroupKey]; exists {
				reasons = append(reasons, "batch confusable collision")
			}
		}
		if lemma != "" {
			if _, exists := localLemmas[lemma]; exists {
				reasons = append(reasons, "batch lemma duplicate")
			}
		}
		if len(reasons) > 0 {
			result.Rejected[candidate.Word] = reasons
			result.RejectList = append(result.RejectList, candidate.Word)
			continue
		}
		candidate.RankingScore = rankCandidate(candidate)
		result.Accepted = append(result.Accepted, candidate)
		localNormalized[normalized] = struct{}{}
		if candidate.ConfusableGroupKey != "" {
			localGroups[candidate.ConfusableGroupKey] = struct{}{}
		}
		if lemma != "" {
			localLemmas[lemma] = struct{}{}
		}
	}

	sort.SliceStable(result.Accepted, func(i int, j int) bool {
		return result.Accepted[i].RankingScore > result.Accepted[j].RankingScore
	})

	return result
}

func rankCandidate(candidate domain.CandidateWord) float64 {
	score := 0.0
	if candidate.Level != "" {
		score += 2.0
	}
	if candidate.Topic != "" {
		score += 1.5
	}
	if candidate.EnglishMeaning != "" && candidate.VietnameseMeaning != "" {
		score += 1.5
	}
	if candidate.ExampleSentence1 != "" {
		score += 0.5
	}
	if candidate.ExampleSentence2 != "" {
		score += 0.5
	}
	if candidate.IPA != "" {
		score += 0.25
	}
	if candidate.PartOfSpeech != "" {
		score += 0.25
	}
	return score
}

func BuildGenerationExclusions(words []domain.Word, states []domain.UserWordState, poolItems []domain.DailyLearningPoolItem) (wordsOut []string, lemmas []string, groups []string) {
	wordSet := map[string]struct{}{}
	lemmaSet := map[string]struct{}{}
	groupSet := map[string]struct{}{}
	for _, word := range words {
		addNonEmpty(wordSet, word.Word)
		addNonEmpty(wordSet, word.CanonicalForm)
		addNonEmpty(lemmaSet, word.Lemma)
		addNonEmpty(groupSet, word.ConfusableGroupKey)
	}
	for _, item := range poolItems {
		if item.Word != nil {
			addNonEmpty(wordSet, item.Word.Word)
			addNonEmpty(wordSet, item.Word.CanonicalForm)
			addNonEmpty(lemmaSet, item.Word.Lemma)
			addNonEmpty(groupSet, item.Word.ConfusableGroupKey)
		}
	}
	for _, state := range states {
		_ = state
	}
	return mapKeys(wordSet), mapKeys(lemmaSet), mapKeys(groupSet)
}

func mapKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func addNonEmpty(target map[string]struct{}, value string) {
	value = NormalizeWord(value)
	if value == "" {
		return
	}
	target[value] = struct{}{}
}

func SeenNewWordLookback(now time.Time) time.Time {
	return now.Add(-30 * 24 * time.Hour)
}
