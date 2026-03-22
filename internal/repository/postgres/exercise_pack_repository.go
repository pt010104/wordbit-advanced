package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"wordbit-advanced-app/backend/internal/domain"
)

type ExercisePackRepository struct {
	pool *pgxpool.Pool
}

func (r *ExercisePackRepository) GetByClusterHash(ctx context.Context, userID uuid.UUID, localDate string, clusterHash string, packType domain.ExercisePackType) (domain.ContextExercisePack, error) {
	return scanContextExercisePack(r.pool.QueryRow(ctx, `
		SELECT id, user_id, local_date, topic, cefr_level, pack_type, cluster_hash, source_words_json, payload_json, status, llm_run_id, created_at, updated_at
		FROM context_exercise_packs
		WHERE user_id = $1
		  AND local_date = $2::date
		  AND cluster_hash = $3
		  AND pack_type = $4
		  AND status = 'ready'
		ORDER BY created_at DESC
		LIMIT 1
	`, userID, localDate, clusterHash, packType))
}

func (r *ExercisePackRepository) GetLatestReadyByLocalDate(ctx context.Context, userID uuid.UUID, localDate string, packType domain.ExercisePackType) (domain.ContextExercisePack, error) {
	return scanContextExercisePack(r.pool.QueryRow(ctx, `
		SELECT id, user_id, local_date, topic, cefr_level, pack_type, cluster_hash, source_words_json, payload_json, status, llm_run_id, created_at, updated_at
		FROM context_exercise_packs
		WHERE user_id = $1
		  AND local_date = $2::date
		  AND pack_type = $3
		  AND status = 'ready'
		ORDER BY created_at DESC
		LIMIT 1
	`, userID, localDate, packType))
}

func (r *ExercisePackRepository) Create(ctx context.Context, pack domain.ContextExercisePack) (domain.ContextExercisePack, error) {
	return scanContextExercisePack(r.pool.QueryRow(ctx, `
		INSERT INTO context_exercise_packs (
			id, user_id, local_date, topic, cefr_level, pack_type, cluster_hash, source_words_json, payload_json, status, llm_run_id
		) VALUES (
			$1, $2, $3::date, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10, $11
		)
		ON CONFLICT (user_id, local_date, cluster_hash, pack_type) DO UPDATE SET
			topic = EXCLUDED.topic,
			cefr_level = EXCLUDED.cefr_level,
			source_words_json = EXCLUDED.source_words_json,
			payload_json = EXCLUDED.payload_json,
			status = EXCLUDED.status,
			llm_run_id = EXCLUDED.llm_run_id
		RETURNING id, user_id, local_date, topic, cefr_level, pack_type, cluster_hash, source_words_json, payload_json, status, llm_run_id, created_at, updated_at
	`,
		pack.ID,
		pack.UserID,
		pack.LocalDate,
		pack.Topic,
		pack.CEFRLevel,
		pack.PackType,
		pack.ClusterHash,
		marshalJSONValue(pack.SourceWords, "[]"),
		marshalJSONValue(pack.Payload, "{}"),
		pack.Status,
		pack.LLMRunID,
	))
}
