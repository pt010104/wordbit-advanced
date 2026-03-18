package service

import (
	"testing"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

func TestNormalizeWord(t *testing.T) {
	t.Parallel()

	got := NormalizeWord("  Economic!!  ")
	if got != "economic" {
		t.Fatalf("expected economic, got %q", got)
	}

	got = NormalizeWord("Café society")
	if got != "cafe society" {
		t.Fatalf("expected accent-stripped normalization, got %q", got)
	}
}

func TestFilterCandidatesRejectsHistoryAndConfusables(t *testing.T) {
	t.Parallel()

	existingWordID := uuid.New()
	result := FilterCandidates(
		[]domain.CandidateWord{
			{Word: "Effect", CanonicalForm: "effect", Lemma: "effect", Level: domain.CEFRB2, Topic: "Society", EnglishMeaning: "result", VietnameseMeaning: "kết quả"},
			{Word: "Adapt", CanonicalForm: "adapt", Lemma: "adapt", Level: domain.CEFRB2, Topic: "Work/Career", EnglishMeaning: "adjust", VietnameseMeaning: "thích nghi"},
			{Word: "Adopt", CanonicalForm: "adopt", Lemma: "adopt", Level: domain.CEFRB2, Topic: "Work/Career", EnglishMeaning: "take up", VietnameseMeaning: "nhận"},
			{Word: "Sustain", CanonicalForm: "sustain", Lemma: "sustain", Level: domain.CEFRB2, Topic: "Environment", EnglishMeaning: "maintain", VietnameseMeaning: "duy trì"},
		},
		[]domain.UserWordState{
			{WordID: existingWordID},
		},
		[]domain.Word{
			{ID: existingWordID, Word: "Affect", NormalizedForm: "affect", CanonicalForm: "affect", Lemma: "affect", ConfusableGroupKey: "affect_effect"},
		},
		nil,
		nil,
	)

	if len(result.Accepted) != 2 {
		t.Fatalf("expected 2 accepted candidates, got %d", len(result.Accepted))
	}
	if _, ok := result.Rejected["Effect"]; !ok {
		t.Fatalf("expected Effect to be rejected")
	}
	if _, ok := result.Rejected["Adopt"]; !ok {
		t.Fatalf("expected Adopt to be rejected due to confusable collision")
	}
}
