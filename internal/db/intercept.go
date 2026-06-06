package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

var ErrInterceptRuleNotFound = errors.New("intercept rule not found")

type InterceptRule struct {
	ID              string
	APIID           string
	Name            string
	Method          string
	PathPattern     string
	Priority        int
	StatusCode      int
	ResponseHeaders map[string]string
	ResponseBody    string
	DelayMS         int
	Enabled         bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type InterceptRuleInput struct {
	ID              string
	APIID           string
	Name            string
	Method          string
	PathPattern     string
	Priority        int
	StatusCode      int
	ResponseHeaders map[string]string
	ResponseBody    string
	DelayMS         int
	Enabled         bool
}

type InterceptRuleUpdate struct {
	Name            *string
	Method          *string
	PathPattern     *string
	Priority        *int
	StatusCode      *int
	ResponseHeaders *map[string]string
	ResponseBody    *string
	DelayMS         *int
	Enabled         *bool
}

const interceptColumns = `
	id, api_id, name, method, path_pattern, priority, status_code,
	response_headers, response_body, delay_ms, enabled, created_at, updated_at
`

func (s *Store) ListInterceptRules(ctx context.Context, apiID string) ([]InterceptRule, error) {
	q := `SELECT ` + interceptColumns + `
		FROM intercept_rules
		WHERE api_id = $1
		ORDER BY priority DESC, created_at ASC`
	return s.queryInterceptRules(ctx, q, apiID)
}

func (s *Store) ListEnabledInterceptRules(ctx context.Context, apiID string) ([]InterceptRule, error) {
	q := `SELECT ` + interceptColumns + `
		FROM intercept_rules
		WHERE api_id = $1 AND enabled = TRUE
		ORDER BY priority DESC, created_at ASC`
	return s.queryInterceptRules(ctx, q, apiID)
}

func (s *Store) GetInterceptRule(ctx context.Context, apiID, id string) (*InterceptRule, error) {
	q := `SELECT ` + interceptColumns + `
		FROM intercept_rules
		WHERE api_id = $1 AND id = $2`
	r, err := scanInterceptRule(s.db.QueryRowContext(ctx, q, apiID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInterceptRuleNotFound
	}
	return r, err
}

func (s *Store) CreateInterceptRule(ctx context.Context, in InterceptRuleInput) (*InterceptRule, error) {
	headers, err := marshalHeaders(in.ResponseHeaders)
	if err != nil {
		return nil, err
	}
	q := `INSERT INTO intercept_rules (
			id, api_id, name, method, path_pattern, priority, status_code,
			response_headers, response_body, delay_ms, enabled
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11)
		RETURNING ` + interceptColumns
	r, err := scanInterceptRule(s.db.QueryRowContext(ctx, q,
		in.ID, in.APIID, in.Name, in.Method, in.PathPattern, in.Priority, in.StatusCode,
		headers, in.ResponseBody, in.DelayMS, in.Enabled,
	))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return nil, ErrAPINotFound
		}
		return nil, fmt.Errorf("create intercept rule: %w", err)
	}
	return r, nil
}

func (s *Store) UpdateInterceptRule(ctx context.Context, apiID, id string, in InterceptRuleUpdate) (*InterceptRule, error) {
	var (
		headersJSON     any
		headersProvided bool
	)
	if in.ResponseHeaders != nil {
		h, err := marshalHeaders(*in.ResponseHeaders)
		if err != nil {
			return nil, err
		}
		headersJSON = h
		headersProvided = true
	}
	q := `UPDATE intercept_rules SET
			name             = COALESCE($3,  name),
			method           = COALESCE($4,  method),
			path_pattern     = COALESCE($5,  path_pattern),
			priority         = COALESCE($6,  priority),
			status_code      = COALESCE($7,  status_code),
			response_headers = CASE WHEN $8::boolean THEN $9::jsonb ELSE response_headers END,
			response_body    = COALESCE($10, response_body),
			delay_ms         = COALESCE($11, delay_ms),
			enabled          = COALESCE($12, enabled),
			updated_at       = NOW()
		WHERE api_id = $1 AND id = $2
		RETURNING ` + interceptColumns
	r, err := scanInterceptRule(s.db.QueryRowContext(ctx, q,
		apiID, id,
		in.Name, in.Method, in.PathPattern, in.Priority, in.StatusCode,
		headersProvided, headersJSON,
		in.ResponseBody, in.DelayMS, in.Enabled,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInterceptRuleNotFound
	}
	return r, err
}

func (s *Store) DeleteInterceptRule(ctx context.Context, apiID, id string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM intercept_rules WHERE api_id = $1 AND id = $2`, apiID, id)
	if err != nil {
		return fmt.Errorf("delete intercept rule: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrInterceptRuleNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanInterceptRule(rs rowScanner) (*InterceptRule, error) {
	var (
		r          InterceptRule
		headersRaw []byte
	)
	if err := rs.Scan(
		&r.ID, &r.APIID, &r.Name, &r.Method, &r.PathPattern, &r.Priority, &r.StatusCode,
		&headersRaw, &r.ResponseBody, &r.DelayMS, &r.Enabled, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(headersRaw) > 0 {
		if err := json.Unmarshal(headersRaw, &r.ResponseHeaders); err != nil {
			return nil, fmt.Errorf("unmarshal headers: %w", err)
		}
	}
	if r.ResponseHeaders == nil {
		r.ResponseHeaders = map[string]string{}
	}
	return &r, nil
}

func (s *Store) queryInterceptRules(ctx context.Context, q string, args ...any) ([]InterceptRule, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query intercept rules: %w", err)
	}
	defer rows.Close()

	out := make([]InterceptRule, 0)
	for rows.Next() {
		r, err := scanInterceptRule(rows)
		if err != nil {
			return nil, fmt.Errorf("scan intercept rule: %w", err)
		}
		out = append(out, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate intercept rules: %w", err)
	}
	return out, nil
}

func marshalHeaders(h map[string]string) (string, error) {
	if h == nil {
		h = map[string]string{}
	}
	b, err := json.Marshal(h)
	if err != nil {
		return "", fmt.Errorf("marshal headers: %w", err)
	}
	return string(b), nil
}
