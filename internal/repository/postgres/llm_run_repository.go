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
	_, err := r.InsertReturning(ctx, run)
	return err
}

func (r *LLMRunRepository) InsertReturning(ctx context.Context, run domain.LLMGenerationRun) (domain.LLMGenerationRun, error) {
	query := `
		INSERT INTO llm_generation_runs (
			user_id, pool_id, local_date, topic, requested_count, accepted_count, attempt, status, provider, model, prompt, raw_response, rejection_summary, error_message
		) VALUES (
			$1, $2, $3::date, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13::jsonb, $14
		)
		RETURNING id, user_id, pool_id, local_date, topic, requested_count, accepted_count, attempt, status, provider, model, prompt,
		          raw_response, rejection_summary, error_message, created_at, updated_at
	`
	return scanRun(r.pool.QueryRow(ctx, query,
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
	))
}

func (r *LLMRunRepository) CountByUserLocalDateAndPrompt(ctx context.Context, userID uuid.UUID, localDate string, prompt string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM llm_generation_runs
		WHERE user_id = $1
		  AND local_date = $2::date
		  AND prompt = $3
	`, userID, localDate, prompt).Scan(&count)
	return count, mapError(err)
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
