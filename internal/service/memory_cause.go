package service

import (
	"strings"
	"time"

	"wordbit-advanced-app/backend/internal/domain"
)

const (
	slowRecallRevealThresholdMs = 8000
	slowRecallMCQThresholdMs    = 6000
	slowRecallFillThresholdMs   = 9000
	guessedCorrectThresholdMs   = 2500
	maxStoredTypedAnswerLength  = 64
)

type MemoryCauseInput struct {
	State                            domain.UserWordState
	TargetWord                       domain.Word
	ModeUsed                         domain.ReviewMode
	AnswerCorrect                    bool
	RevealedMeaningBeforeAnswer      bool
	RevealedExampleBeforeAnswer      bool
	UsedHint                         bool
	ResponseTimeMs                   int
	InputMethod                      domain.ReviewInputMethod
	NormalizedTypedAnswer            string
	SelectedChoiceConfusableGroupKey string
}

func InferMemoryCause(input MemoryCauseInput) domain.MemoryCause {
	if input.RevealedMeaningBeforeAnswer {
		return domain.MemoryCauseForgotMeaning
	}

	if input.ModeUsed == domain.ReviewModeMultipleChoice &&
		!input.AnswerCorrect &&
		input.TargetWord.ConfusableGroupKey != "" &&
		input.SelectedChoiceConfusableGroupKey != "" &&
		input.SelectedChoiceConfusableGroupKey == input.TargetWord.ConfusableGroupKey {
		return domain.MemoryCauseMixedUpWord
	}

	if isSpellingIssue(input) {
		return domain.MemoryCauseSpellingIssue
	}

	if input.AnswerCorrect &&
		!input.RevealedMeaningBeforeAnswer &&
		input.ResponseTimeMs >= slowRecallThresholdForMode(input.ModeUsed) {
		return domain.MemoryCauseSlowRecall
	}

	if input.ModeUsed == domain.ReviewModeMultipleChoice &&
		input.AnswerCorrect &&
		!input.RevealedMeaningBeforeAnswer &&
		!input.UsedHint &&
		input.ResponseTimeMs > 0 &&
		input.ResponseTimeMs < guessedCorrectThresholdMs &&
		(input.State.Difficulty >= 0.75 || input.State.WeaknessScore >= 1.4) {
		return domain.MemoryCauseGuessedCorrect
	}

	return ""
}

func NormalizeTypedAnswerForStorage(value string) string {
	normalized := NormalizeWord(value)
	if len(normalized) <= maxStoredTypedAnswerLength {
		return normalized
	}
	return normalized[:maxStoredTypedAnswerLength]
}

func ApplyMemoryCause(state domain.UserWordState, cause domain.MemoryCause, responseTimeMs int, answerCorrect bool) domain.UserWordState {
	state.LastMemoryCause = cause
	state.LastResponseTimeMs = responseTimeMs
	state.LastAnswerCorrect = boolPointer(answerCorrect)

	switch cause {
	case domain.MemoryCauseForgotMeaning:
		state.MeaningForgetCount++
	case domain.MemoryCauseSpellingIssue:
		state.SpellingIssueCount++
	case domain.MemoryCauseMixedUpWord:
		state.ConfusableMixupCount++
	case domain.MemoryCauseSlowRecall:
		state.SlowRecallCount++
	case domain.MemoryCauseGuessedCorrect:
		state.GuessedCorrectCount++
	}

	return state
}

func ApplyMemoryCauseIntervalBias(state domain.UserWordState, cause domain.MemoryCause, now time.Time) domain.UserWordState {
	if state.Status != domain.WordStatusReview || state.NextReviewAt == nil || state.IntervalSeconds <= 0 {
		return state
	}

	multiplier := 1.0
	switch cause {
	case domain.MemoryCauseSlowRecall:
		multiplier = 0.85
	case domain.MemoryCauseGuessedCorrect:
		multiplier = 0.90
	default:
		return state
	}

	biasedSeconds := int(float64(state.IntervalSeconds) * multiplier)
	if biasedSeconds < int((4 * 60 * 60)) {
		biasedSeconds = int((4 * 60 * 60))
	}
	state.IntervalSeconds = biasedSeconds
	next := now.Add(time.Duration(biasedSeconds) * time.Second)
	state.NextReviewAt = &next
	return state
}

func slowRecallThresholdForMode(mode domain.ReviewMode) int {
	switch mode {
	case domain.ReviewModeMultipleChoice:
		return slowRecallMCQThresholdMs
	case domain.ReviewModeFillBlank:
		return slowRecallFillThresholdMs
	default:
		return slowRecallRevealThresholdMs
	}
}

func isSpellingIssue(input MemoryCauseInput) bool {
	if input.ModeUsed == domain.ReviewModeFillBlank &&
		!input.AnswerCorrect &&
		input.NormalizedTypedAnswer != "" {
		targets := normalizedAcceptedAnswers(input.TargetWord)
		minDistance := -1
		for _, target := range targets {
			distance := levenshteinDistance(input.NormalizedTypedAnswer, target)
			if minDistance < 0 || distance < minDistance {
				minDistance = distance
			}
		}
		if minDistance >= 0 {
			maxDistance := 2
			if longest := maxNormalizedLength(append(targets, input.NormalizedTypedAnswer)...); longest > 0 {
				relative := longest / 4
				if relative > maxDistance {
					maxDistance = relative
				}
			}
			if minDistance <= maxDistance {
				return true
			}
		}
	}

	if input.ModeUsed == domain.ReviewModeMultipleChoice &&
		input.AnswerCorrect &&
		input.State.LastMode == domain.ReviewModeFillBlank &&
		input.State.LastAnswerCorrect != nil &&
		!*input.State.LastAnswerCorrect {
		return true
	}

	return false
}

func normalizedAcceptedAnswers(word domain.Word) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 3)
	for _, candidate := range []string{word.Word, word.CanonicalForm, word.Lemma} {
		normalized := NormalizeWord(candidate)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func maxNormalizedLength(values ...string) int {
	maxValue := 0
	for _, value := range values {
		if length := len(strings.TrimSpace(value)); length > maxValue {
			maxValue = length
		}
	}
	return maxValue
}

func levenshteinDistance(a string, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			curr[j] = min3(
				curr[j-1]+1,
				prev[j]+1,
				prev[j-1]+cost,
			)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a int, b int, c int) int {
	if a <= b && a <= c {
		return a
	}
	if b <= a && b <= c {
		return b
	}
	return c
}

func boolPointer(value bool) *bool {
	return &value
}
