package service

import (
	"testing"
	"time"
)

func TestTopicForDateRotatesAcrossExpandedCatalog(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 3, 20, 9, 0, 0, 0, time.FixedZone("ICT", 7*3600))
	seen := make(map[string]struct{}, len(rotatingTopics))

	for i := 0; i < len(rotatingTopics); i++ {
		topic := TopicForDate(start.AddDate(0, 0, i))
		seen[topic] = struct{}{}
	}

	if len(seen) != len(rotatingTopics) {
		t.Fatalf("expected %d unique rotating topics over %d days, got %d", len(rotatingTopics), len(rotatingTopics), len(seen))
	}
}
