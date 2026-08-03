package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"wordbit-advanced-app/backend/internal/domain"
)

// RecordingRepository stores only metadata and a private R2 object key. A
// single current recording per user/word prevents unbounded storage growth.
type RecordingRepository struct {
	pool *pgxpool.Pool
}

func (r *RecordingRepository) Upsert(ctx context.Context, recording domain.UserWordRecording) (domain.UserWordRecording, error) {
	return scanRecording(r.pool.QueryRow(ctx, `
		INSERT INTO user_word_recordings (user_id, word_id, object_key, content_type, size_bytes, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, word_id) DO UPDATE
		SET object_key = EXCLUDED.object_key,
		    content_type = EXCLUDED.content_type,
		    size_bytes = EXCLUDED.size_bytes,
		    recorded_at = EXCLUDED.recorded_at
		RETURNING user_id, word_id, object_key, content_type, size_bytes, recorded_at, updated_at
	`, recording.UserID, recording.WordID, recording.ObjectKey, recording.ContentType, recording.SizeBytes, recording.RecordedAt))
}

func (r *RecordingRepository) Get(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) (domain.UserWordRecording, error) {
	return scanRecording(r.pool.QueryRow(ctx, `
		SELECT user_id, word_id, object_key, content_type, size_bytes, recorded_at, updated_at
		FROM user_word_recordings
		WHERE user_id = $1 AND word_id = $2
	`, userID, wordID))
}

func scanRecording(row interface{ Scan(...any) error }) (domain.UserWordRecording, error) {
	var recording domain.UserWordRecording
	err := row.Scan(
		&recording.UserID,
		&recording.WordID,
		&recording.ObjectKey,
		&recording.ContentType,
		&recording.SizeBytes,
		&recording.RecordedAt,
		&recording.UpdatedAt,
	)
	return recording, mapError(err)
}
