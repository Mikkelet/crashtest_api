package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	ErrAPINotFound      = errors.New("api not found")
	ErrAPIAlreadyExists = errors.New("api already exists")
)

type API struct {
	ID          string
	Name        string
	BaseURL     string
	Description sql.NullString
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type APIInput struct {
	ID          string
	Name        string
	BaseURL     string
	Description sql.NullString
	Enabled     bool
}

type APIUpdate struct {
	Name        *string
	BaseURL     *string
	Description *sql.NullString
	Enabled     *bool
}

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	conn.SetMaxOpenConns(20)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(time.Hour)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := conn.PingContext(pingCtx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return &Store{db: conn}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) GetAPI(ctx context.Context, id string) (*API, error) {
	const q = `
		SELECT id, name, base_url, description, enabled, created_at, updated_at
		FROM apis
		WHERE id = $1 AND enabled = TRUE
	`
	return s.queryOne(ctx, q, id)
}

func (s *Store) GetAPIByID(ctx context.Context, id string) (*API, error) {
	const q = `
		SELECT id, name, base_url, description, enabled, created_at, updated_at
		FROM apis
		WHERE id = $1
	`
	return s.queryOne(ctx, q, id)
}

func (s *Store) ListAPIs(ctx context.Context) ([]API, error) {
	const q = `
		SELECT id, name, base_url, description, enabled, created_at, updated_at
		FROM apis
		ORDER BY created_at DESC
	`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list apis: %w", err)
	}
	defer rows.Close()

	var out []API
	for rows.Next() {
		var a API
		if err := rows.Scan(
			&a.ID, &a.Name, &a.BaseURL, &a.Description, &a.Enabled, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan api: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate apis: %w", err)
	}
	return out, nil
}

func (s *Store) CreateAPI(ctx context.Context, in APIInput) (*API, error) {
	const q = `
		INSERT INTO apis (id, name, base_url, description, enabled)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, base_url, description, enabled, created_at, updated_at
	`
	a, err := s.queryOne(ctx, q, in.ID, in.Name, in.BaseURL, in.Description, in.Enabled)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAPIAlreadyExists
		}
		return nil, err
	}
	return a, nil
}

func (s *Store) UpdateAPI(ctx context.Context, id string, in APIUpdate) (*API, error) {
	const q = `
		UPDATE apis SET
			name        = COALESCE($2, name),
			base_url    = COALESCE($3, base_url),
			description = CASE WHEN $4::boolean THEN $5 ELSE description END,
			enabled     = COALESCE($6, enabled),
			updated_at  = NOW()
		WHERE id = $1
		RETURNING id, name, base_url, description, enabled, created_at, updated_at
	`
	descProvided := in.Description != nil
	var descValue sql.NullString
	if descProvided {
		descValue = *in.Description
	}
	return s.queryOne(ctx, q, id, in.Name, in.BaseURL, descProvided, descValue, in.Enabled)
}

func (s *Store) DeleteAPI(ctx context.Context, id string) error {
	const q = `DELETE FROM apis WHERE id = $1`
	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("delete api: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrAPINotFound
	}
	return nil
}

func (s *Store) queryOne(ctx context.Context, q string, args ...any) (*API, error) {
	var a API
	err := s.db.QueryRowContext(ctx, q, args...).Scan(
		&a.ID, &a.Name, &a.BaseURL, &a.Description, &a.Enabled, &a.CreatedAt, &a.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAPINotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query api: %w", err)
	}
	return &a, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
