package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

const recordingDownloadTTL = 15 * time.Minute

type RecordingService struct {
	recordings RecordingRepository
	pools      PoolRepository
	wordSets   *WordSetService
	storage    ObjectStorage
	clock      Clock
}

func NewRecordingService(recordings RecordingRepository, pools PoolRepository, wordSets *WordSetService, storage ObjectStorage, clock Clock) *RecordingService {
	return &RecordingService{recordings: recordings, pools: pools, wordSets: wordSets, storage: storage, clock: clock}
}

type RecordingPlayback struct {
	URL        string    `json:"url"`
	RecordedAt time.Time `json:"recorded_at"`
}

// Save replaces the user's previous take for this word. It accepts only Mode 1
// pool cards and sets whose owner explicitly enabled the feature.
func (s *RecordingService) Save(ctx context.Context, userID uuid.UUID, poolItemID uuid.UUID, audio []byte, contentType string) (RecordingPlayback, error) {
	if s.storage == nil {
		return RecordingPlayback{}, fmt.Errorf("%w: voice recording storage is not configured", domain.ErrServiceUnavailable)
	}
	if len(audio) == 0 {
		return RecordingPlayback{}, fmt.Errorf("%w: audio is required", domain.ErrValidation)
	}
	contentType = normalizedRecordingContentType(contentType)
	if contentType == "" {
		return RecordingPlayback{}, fmt.Errorf("%w: unsupported recording format", domain.ErrValidation)
	}
	item, err := s.pools.GetPoolItem(ctx, userID, poolItemID)
	if err != nil {
		return RecordingPlayback{}, err
	}
	if item.ReviewMode != domain.ReviewModeReveal {
		return RecordingPlayback{}, fmt.Errorf("%w: voice recording is available in Mode 1 only", domain.ErrValidation)
	}
	if s.wordSets == nil {
		return RecordingPlayback{}, fmt.Errorf("%w: word set preferences are unavailable", domain.ErrServiceUnavailable)
	}
	activeSet, err := s.wordSets.ResolveActiveSet(ctx, userID)
	if err != nil {
		return RecordingPlayback{}, err
	}
	if !activeSet.RecordingEnabled {
		return RecordingPlayback{}, fmt.Errorf("%w: enable voice recording in this set's settings first", domain.ErrForbidden)
	}

	objectKey := fmt.Sprintf("wordbit/voice-recordings/%s/%s/latest.m4a", userID, item.WordID)
	if err := s.storage.Put(ctx, objectKey, contentType, audio); err != nil {
		return RecordingPlayback{}, err
	}
	recordedAt := s.clock.Now()
	recording, err := s.recordings.Upsert(ctx, domain.UserWordRecording{
		UserID:      userID,
		WordID:      item.WordID,
		ObjectKey:   objectKey,
		ContentType: contentType,
		SizeBytes:   int64(len(audio)),
		RecordedAt:  recordedAt,
	})
	if err != nil {
		return RecordingPlayback{}, err
	}
	return s.playback(ctx, recording)
}

func (s *RecordingService) GetPlayback(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) (RecordingPlayback, error) {
	if s.storage == nil {
		return RecordingPlayback{}, fmt.Errorf("%w: voice recording storage is not configured", domain.ErrServiceUnavailable)
	}
	recording, err := s.recordings.Get(ctx, userID, wordID)
	if err != nil {
		return RecordingPlayback{}, err
	}
	return s.playback(ctx, recording)
}

func (s *RecordingService) playback(ctx context.Context, recording domain.UserWordRecording) (RecordingPlayback, error) {
	url, err := s.storage.PresignDownload(ctx, recording.ObjectKey, recordingDownloadTTL)
	if err != nil {
		return RecordingPlayback{}, err
	}
	return RecordingPlayback{URL: url, RecordedAt: recording.RecordedAt}, nil
}

func normalizedRecordingContentType(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	switch value {
	case "audio/mp4", "audio/m4a", "audio/aac":
		return "audio/mp4"
	default:
		return ""
	}
}
