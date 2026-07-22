package postgres

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wordbit-advanced-app/backend/internal/domain"
)

type WordImportBufferRepository struct {
	pool *pgxpool.Pool
}

func (r *WordImportBufferRepository) List(ctx context.Context, userID uuid.UUID, setID *uuid.UUID) ([]domain.WordImportBufferItem, error) {
	query := `
		SELECT id, user_id, word_set_id, raw_word, source_url, candidate, status, created_at, updated_at
		FROM word_import_buffer_items
		WHERE user_id = $1
		  AND status <> 'imported'
	`
	args := []any{userID}
	if setID != nil {
		query += ` AND word_set_id = $2`
		args = append(args, *setID)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	items := []domain.WordImportBufferItem{}
	for rows.Next() {
		item, scanErr := scanWordImportBufferItem(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *WordImportBufferRepository) Get(ctx context.Context, userID uuid.UUID, itemID uuid.UUID) (domain.WordImportBufferItem, error) {
	return scanWordImportBufferItem(r.pool.QueryRow(ctx, `
		SELECT id, user_id, word_set_id, raw_word, source_url, candidate, status, created_at, updated_at
		FROM word_import_buffer_items
		WHERE user_id = $1 AND id = $2
	`, userID, itemID))
}

func (r *WordImportBufferRepository) Create(ctx context.Context, item domain.WordImportBufferItem) (domain.WordImportBufferItem, error) {
	return scanWordImportBufferItem(r.pool.QueryRow(ctx, `
		INSERT INTO word_import_buffer_items (user_id, word_set_id, raw_word, source_url, status)
		VALUES ($1, $2, $3, $4, 'pending')
		RETURNING id, user_id, word_set_id, raw_word, source_url, candidate, status, created_at, updated_at
	`, item.UserID, item.WordSetID, item.RawWord, item.SourceURL))
}

func (r *WordImportBufferRepository) UpdateCandidate(ctx context.Context, userID uuid.UUID, itemID uuid.UUID, candidate domain.CandidateWord) (domain.WordImportBufferItem, error) {
	return scanWordImportBufferItem(r.pool.QueryRow(ctx, `
		UPDATE word_import_buffer_items
		SET candidate = $3::jsonb,
		    status = 'generated'
		WHERE user_id = $1 AND id = $2 AND status <> 'imported'
		RETURNING id, user_id, word_set_id, raw_word, source_url, candidate, status, created_at, updated_at
	`, userID, itemID, marshalJSONValue(candidate, "{}")))
}

func (r *WordImportBufferRepository) MarkImported(ctx context.Context, userID uuid.UUID, itemID uuid.UUID) (domain.WordImportBufferItem, error) {
	return scanWordImportBufferItem(r.pool.QueryRow(ctx, `
		UPDATE word_import_buffer_items
		SET status = 'imported'
		WHERE user_id = $1 AND id = $2
		RETURNING id, user_id, word_set_id, raw_word, source_url, candidate, status, created_at, updated_at
	`, userID, itemID))
}

func (r *WordImportBufferRepository) Delete(ctx context.Context, userID uuid.UUID, itemID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM word_import_buffer_items
		WHERE user_id = $1 AND id = $2
	`, userID, itemID)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

type wordImportBufferScanner interface {
	Scan(dest ...any) error
}

func scanWordImportBufferItem(row wordImportBufferScanner) (domain.WordImportBufferItem, error) {
	var item domain.WordImportBufferItem
	var candidateBytes []byte
	if err := row.Scan(
		&item.ID,
		&item.UserID,
		&item.WordSetID,
		&item.RawWord,
		&item.SourceURL,
		&candidateBytes,
		&item.Status,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domain.WordImportBufferItem{}, mapError(err)
	}
	if len(candidateBytes) > 0 && string(candidateBytes) != "null" {
		var candidate domain.CandidateWord
		if err := json.Unmarshal(candidateBytes, &candidate); err == nil {
			item.Candidate = &candidate
		}
	}
	return item, nil
}

var _ wordImportBufferScanner = pgx.Row(nil)
