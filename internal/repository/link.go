package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/farahty/url-shorten/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("link not found")

type LinkRepository struct {
	db *pgxpool.Pool
}

func NewLinkRepository(db *pgxpool.Pool) *LinkRepository {
	return &LinkRepository{db: db}
}

func (r *LinkRepository) NextID(ctx context.Context) (int64, error) {
	var id int64
	err := r.db.QueryRow(ctx, "SELECT nextval('link_id_seq')").Scan(&id)
	return id, err
}

func (r *LinkRepository) Create(ctx context.Context, link *model.Link) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO links (id, code, original_url, is_alias, expires_at, api_key_id, og_title, og_desc, og_image, og_site)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		link.ID, link.Code, link.OriginalURL, link.IsAlias, link.ExpiresAt,
		link.APIKeyID, link.OGTitle, link.OGDesc, link.OGImage, link.OGSite,
	)
	return err
}

func (r *LinkRepository) UpdateOGData(ctx context.Context, code string, title, desc, image, site *string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE links SET og_title=$1, og_desc=$2, og_image=$3, og_site=$4 WHERE code=$5`,
		title, desc, image, site, code,
	)
	return err
}

func (r *LinkRepository) GetByCode(ctx context.Context, code string) (*model.Link, error) {
	link := &model.Link{}
	err := r.db.QueryRow(ctx,
		`SELECT l.id, l.code, l.original_url, l.is_alias, l.expires_at, l.click_count, l.api_key_id,
		        l.og_title, l.og_desc, l.og_image, l.og_site, l.created_at, a.base_url
		 FROM links l
		 LEFT JOIN api_keys a ON a.id = l.api_key_id
		 WHERE l.code = $1`, code,
	).Scan(
		&link.ID, &link.Code, &link.OriginalURL, &link.IsAlias, &link.ExpiresAt,
		&link.ClickCount, &link.APIKeyID, &link.OGTitle, &link.OGDesc,
		&link.OGImage, &link.OGSite, &link.CreatedAt, &link.AppBaseURL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return link, err
}

func (r *LinkRepository) List(ctx context.Context, apiKeyID string, page, limit int) ([]model.Link, int, error) {
	offset := (page - 1) * limit

	var total int
	err := r.db.QueryRow(ctx,
		"SELECT COUNT(*) FROM links WHERE api_key_id = $1", apiKeyID,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := r.db.Query(ctx,
		`SELECT id, code, original_url, is_alias, expires_at, click_count, api_key_id, created_at
		 FROM links WHERE api_key_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3`, apiKeyID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var links []model.Link
	for rows.Next() {
		var l model.Link
		if err := rows.Scan(&l.ID, &l.Code, &l.OriginalURL, &l.IsAlias,
			&l.ExpiresAt, &l.ClickCount, &l.APIKeyID, &l.CreatedAt); err != nil {
			return nil, 0, err
		}
		links = append(links, l)
	}
	return links, total, rows.Err()
}

func (r *LinkRepository) Delete(ctx context.Context, code string, apiKeyID string) error {
	tag, err := r.db.Exec(ctx,
		"DELETE FROM links WHERE code = $1 AND api_key_id = $2", code, apiKeyID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *LinkRepository) IncrementClickCount(ctx context.Context, codes []string) error {
	if len(codes) == 0 {
		return nil
	}

	// Batch increment using unnest
	_, err := r.db.Exec(ctx,
		`UPDATE links SET click_count = click_count + batch.cnt
		 FROM (
		     SELECT unnest($1::text[]) AS code, unnest($2::bigint[]) AS cnt
		 ) AS batch
		 WHERE links.code = batch.code`,
		uniqueCodes(codes), countOccurrences(codes),
	)
	return err
}

func (r *LinkRepository) GetAPIKeyByHash(ctx context.Context, keyHash string) (*model.APIKey, error) {
	key := &model.APIKey{}
	err := r.db.QueryRow(ctx,
		`SELECT id, key_hash, app_name, base_url, is_active, created_at, updated_at
		 FROM api_keys WHERE key_hash = $1`, keyHash,
	).Scan(&key.ID, &key.KeyHash, &key.AppName, &key.BaseURL, &key.IsActive, &key.CreatedAt, &key.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return key, err
}

func (r *LinkRepository) CreateAPIKey(ctx context.Context, keyHash, appName string, baseURL *string) (*model.APIKey, error) {
	key := &model.APIKey{}
	err := r.db.QueryRow(ctx,
		`INSERT INTO api_keys (key_hash, app_name, base_url)
		 VALUES ($1, $2, $3)
		 RETURNING id, key_hash, app_name, base_url, is_active, created_at, updated_at`,
		keyHash, appName, baseURL,
	).Scan(&key.ID, &key.KeyHash, &key.AppName, &key.BaseURL, &key.IsActive, &key.CreatedAt, &key.UpdatedAt)
	return key, err
}

func (r *LinkRepository) ListAPIKeys(ctx context.Context) ([]model.APIKey, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, app_name, base_url, is_active, created_at, updated_at
		 FROM api_keys ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []model.APIKey
	for rows.Next() {
		var k model.APIKey
		if err := rows.Scan(&k.ID, &k.AppName, &k.BaseURL, &k.IsActive, &k.CreatedAt, &k.UpdatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (r *LinkRepository) DeactivateAPIKey(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx,
		"UPDATE api_keys SET is_active = false, updated_at = now() WHERE id = $1", id,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *LinkRepository) CodeExists(ctx context.Context, code string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM links WHERE code = $1)", code,
	).Scan(&exists)
	return exists, err
}

// RunMigrations runs SQL migrations against the database.
func (r *LinkRepository) RunMigrations(ctx context.Context, sql string) error {
	_, err := r.db.Exec(ctx, sql)
	if err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}
	return nil
}

func uniqueCodes(codes []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, c := range codes {
		if !seen[c] {
			seen[c] = true
			result = append(result, c)
		}
	}
	return result
}

func countOccurrences(codes []string) []int64 {
	counts := make(map[string]int64)
	var order []string
	for _, c := range codes {
		if counts[c] == 0 {
			order = append(order, c)
		}
		counts[c]++
	}
	result := make([]int64, len(order))
	for i, c := range order {
		result[i] = counts[c]
	}
	return result
}
