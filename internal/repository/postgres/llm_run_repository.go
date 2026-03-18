package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"wordbit-advanced-app/backend/internal/domain"
)

type LLMRunRepository struct {
	pool *pgxpool.Pool
}

func (r *LLMRunRepository) Insert(ctx context.Context, run domain.LLMGenerationRun) error {
	query := `
		INSERT INTO llm_generation_runs (
			user_id, pool_id, local_date, topic, requested_count, accepted_count, attempt, status, provider, model, prompt, raw_response, rejection_summary, error_message
		) VALUES (
			$1, $2, $3::date, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13::jsonb, $14
		)
	`
	_, err := r.pool.Exec(ctx, query,
		run.UserID,
		run.PoolID,
		run.LocalDate,
		run.Topic,
		run.RequestedCount,
		run.AcceptedCount,
		run.Attempt,
		run.Status,
		run.Provider,
		run.Model,
		run.Prompt,
		fromJSONMap(run.RawResponse),
		fromJSONMap(run.RejectionSummary),
		run.ErrorMessage,
	)
	return mapError(err)
}

func (r *LLMRunRepository) ListRecentByUser(ctx context.Context, userID uuid.UUID, limit int) ([]domain.LLMGenerationRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, pool_id, local_date, topic, requested_count, accepted_count, attempt, status, provider, model, prompt,
		       raw_response, rejection_summary, error_message, created_at, updated_at
		FROM llm_generation_runs
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var runs []domain.LLMGenerationRun
	for rows.Next() {
		run, scanErr := scanRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}
