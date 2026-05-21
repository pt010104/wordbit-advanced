package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wordbit-advanced-app/backend/internal/domain"
)

type WordSetRepository struct {
	pool *pgxpool.Pool
}

func (r *WordSetRepository) List(ctx context.Context, userID uuid.UUID) ([]domain.WordSet, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, name, icon, mode, is_default, created_at, updated_at
		FROM word_sets
		WHERE user_id = $1
		ORDER BY is_default DESC, lower(name) ASC
	`, userID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var sets []domain.WordSet
	for rows.Next() {
		set, scanErr := scanWordSet(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		sets = append(sets, set)
	}
	return sets, rows.Err()
}

func (r *WordSetRepository) Get(ctx context.Context, userID uuid.UUID, setID uuid.UUID) (domain.WordSet, error) {
	return scanWordSet(r.pool.QueryRow(ctx, `
		SELECT id, user_id, name, icon, mode, is_default, created_at, updated_at
		FROM word_sets
		WHERE user_id = $1 AND id = $2
	`, userID, setID))
}

func (r *WordSetRepository) GetDefault(ctx context.Context, userID uuid.UUID) (domain.WordSet, error) {
	return scanWordSet(r.pool.QueryRow(ctx, `
		SELECT id, user_id, name, icon, mode, is_default, created_at, updated_at
		FROM word_sets
		WHERE user_id = $1 AND is_default = TRUE
	`, userID))
}

func (r *WordSetRepository) Create(ctx context.Context, set domain.WordSet) (domain.WordSet, error) {
	mode := set.Mode
	if mode == "" {
		mode = domain.WordSetModeCustom
	}
	name := strings.TrimSpace(set.Name)
	if name == "" {
		return domain.WordSet{}, errors.New("word set name is required")
	}
	return scanWordSet(r.pool.QueryRow(ctx, `
		INSERT INTO word_sets (user_id, name, icon, mode, is_default)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, user_id, name, icon, mode, is_default, created_at, updated_at
	`, set.UserID, name, set.Icon, string(mode), set.IsDefault))
}

func (r *WordSetRepository) Update(ctx context.Context, set domain.WordSet) (domain.WordSet, error) {
	mode := set.Mode
	if mode == "" {
		mode = domain.WordSetModeCustom
	}
	name := strings.TrimSpace(set.Name)
	if name == "" {
		return domain.WordSet{}, errors.New("word set name is required")
	}
	return scanWordSet(r.pool.QueryRow(ctx, `
		UPDATE word_sets
		SET name = $3, icon = $4, mode = $5
		WHERE user_id = $1 AND id = $2
		RETURNING id, user_id, name, icon, mode, is_default, created_at, updated_at
	`, set.UserID, set.ID, name, set.Icon, string(mode)))
}

func (r *WordSetRepository) Delete(ctx context.Context, userID uuid.UUID, setID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM word_sets
		WHERE user_id = $1 AND id = $2 AND is_default = FALSE
	`, userID, setID)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *WordSetRepository) EnsureDefault(ctx context.Context, userID uuid.UUID) (domain.WordSet, error) {
	existing, err := r.GetDefault(ctx, userID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.WordSet{}, err
	}
	return scanWordSet(r.pool.QueryRow(ctx, `
		INSERT INTO word_sets (user_id, name, icon, mode, is_default)
		VALUES ($1, 'Default', 'default', 'new_words', TRUE)
		ON CONFLICT DO NOTHING
		RETURNING id, user_id, name, icon, mode, is_default, created_at, updated_at
	`, userID))
}

func scanWordSet(row pgx.Row) (domain.WordSet, error) {
	var set domain.WordSet
	var mode string
	err := row.Scan(
		&set.ID,
		&set.UserID,
		&set.Name,
		&set.Icon,
		&mode,
		&set.IsDefault,
		&set.CreatedAt,
		&set.UpdatedAt,
	)
	set.Mode = domain.WordSetMode(mode)
	return set, mapError(err)
}
