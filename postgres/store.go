// Package postgres provides a pgx-backed chronica.Store implementation.
package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.naturallyfunny.dev/chronica"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const migrateLockKey = 9928371

// Querier is the minimal pgx-compatible surface Store needs.
//
// *pgxpool.Pool, *pgx.Conn, and pgx.Tx satisfy this interface.
type Querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type transactioner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Store persists chronica sessions and acta in PostgreSQL.
type Store struct {
	db          Querier
	autoMigrate bool
}

var (
	_ chronica.Store           = (*Store)(nil)
	_ chronica.IdempotentStore = (*Store)(nil)
)

// Option configures a Store.
type Option func(*Store)

// WithAutoMigrate runs embedded, idempotent migrations during NewStore.
func WithAutoMigrate() Option {
	return func(s *Store) {
		s.autoMigrate = true
	}
}

// NewStore creates a PostgreSQL-backed store.
//
// By default, NewStore validates that the required schema already exists. Pass
// WithAutoMigrate to run embedded migrations before validation.
func NewStore(ctx context.Context, db Querier, opts ...Option) (*Store, error) {
	if db == nil {
		return nil, errors.New("postgres: NewStore called with nil Querier")
	}

	s := &Store{db: db}
	for _, opt := range opts {
		opt(s)
	}

	if s.autoMigrate {
		if err := s.migrate(ctx); err != nil {
			return nil, fmt.Errorf("postgres: auto-migrate: %w", err)
		}
	}

	if err := s.validateSchema(ctx); err != nil {
		return nil, fmt.Errorf("postgres: validate schema: %w", err)
	}

	return s, nil
}

// Create implements chronica.Store.
func (s *Store) Create(ctx context.Context, c chronica.Chronicum) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(ctx,
		`INSERT INTO "chronica" (id, owner_id, started_at, last_activity_at) VALUES ($1, $2, $3, $4)`,
		c.ID, c.OwnerID, now, now,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("%w: %s", chronica.ErrChronicumExists, c.ID)
		}
		return fmt.Errorf("postgres: create chronicum: %w", err)
	}
	return nil
}

// Record implements chronica.Store.
func (s *Store) Record(ctx context.Context, a chronica.Actum) (chronica.Actum, error) {
	insertAt := time.Now().UTC()
	query := `
		WITH ins AS (
			INSERT INTO "acta" (id, chronicum_id, kind, actor_kind, actor, content, at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING seq, id, chronicum_id, kind, actor_kind, actor, content, at
		), upd AS (
			UPDATE "chronica"
			SET last_activity_at = $7
			WHERE id = $2
		)
		SELECT seq, id, chronicum_id, kind, actor_kind, actor, content, at
		FROM ins
	`

	stored, err := scanActum(s.db.QueryRow(ctx, query,
		a.ID, a.ChronicumID, a.Kind, a.ActorKind, a.Actor, a.Content, insertAt,
	))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return chronica.Actum{}, fmt.Errorf("%w: %s", chronica.ErrChronicumNotFound, a.ChronicumID)
		}
		return chronica.Actum{}, fmt.Errorf("postgres: record actum: %w", err)
	}

	return stored, nil
}

// RecordIdempotent implements chronica.IdempotentStore.
func (s *Store) RecordIdempotent(ctx context.Context, a chronica.Actum, key string) (chronica.Actum, error) {
	if key == "" {
		return s.Record(ctx, a)
	}

	insertAt := time.Now().UTC()
	queryInsert := `
		WITH ins AS (
			INSERT INTO "acta" (id, chronicum_id, kind, actor_kind, actor, content, at, idempotency_key)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (chronicum_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
			RETURNING seq, id, chronicum_id, kind, actor_kind, actor, content, at
		), upd AS (
			UPDATE "chronica"
			SET last_activity_at = $7
			WHERE id = $2 AND EXISTS (SELECT 1 FROM ins)
		)
		SELECT seq, id, chronicum_id, kind, actor_kind, actor, content, at
		FROM ins
	`

	stored, err := scanActum(s.db.QueryRow(ctx, queryInsert,
		a.ID, a.ChronicumID, a.Kind, a.ActorKind, a.Actor, a.Content, insertAt, key,
	))
	if err == nil {
		return stored, nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return chronica.Actum{}, fmt.Errorf("%w: %s", chronica.ErrChronicumNotFound, a.ChronicumID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return chronica.Actum{}, fmt.Errorf("postgres: record idempotent actum: %w", err)
	}

	querySelect := `
		SELECT seq, id, chronicum_id, kind, actor_kind, actor, content, at
		FROM "acta"
		WHERE chronicum_id = $1 AND idempotency_key = $2
	`
	stored, err = scanActum(s.db.QueryRow(ctx, querySelect, a.ChronicumID, key))
	if err != nil {
		return chronica.Actum{}, fmt.Errorf("postgres: select idempotent actum: %w", err)
	}
	return stored, nil
}

// Acta implements chronica.Store.
func (s *Store) Acta(ctx context.Context, chronicumID string, q chronica.ActaQuery) ([]chronica.Actum, error) {
	var exists bool
	if err := s.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM "chronica" WHERE id = $1)`,
		chronicumID,
	).Scan(&exists); err != nil {
		return nil, fmt.Errorf("postgres: check chronicum existence: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("%w: %s", chronica.ErrChronicumNotFound, chronicumID)
	}

	sqlQuery, args := buildActaQuery(chronicumID, q)
	rows, err := s.db.Query(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: query acta: %w", err)
	}
	defer rows.Close()

	var result []chronica.Actum
	for rows.Next() {
		var seq int64
		var a chronica.Actum
		if err := rows.Scan(
			&seq,
			&a.ID,
			&a.ChronicumID,
			&a.Kind,
			&a.ActorKind,
			&a.Actor,
			&a.Content,
			&a.At,
		); err != nil {
			return nil, fmt.Errorf("postgres: scan actum row: %w", err)
		}
		a.At = a.At.UTC()
		result = append(result, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate acta: %w", err)
	}

	return result, nil
}

// Get implements chronica.Store.
func (s *Store) Get(ctx context.Context, chronicumID string) (chronica.Chronicum, error) {
	var c chronica.Chronicum
	err := s.db.QueryRow(ctx,
		`SELECT id, owner_id, started_at, last_activity_at FROM "chronica" WHERE id = $1`,
		chronicumID,
	).Scan(&c.ID, &c.OwnerID, &c.StartedAt, &c.LastActivityAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return chronica.Chronicum{}, fmt.Errorf("%w: %s", chronica.ErrChronicumNotFound, chronicumID)
		}
		return chronica.Chronicum{}, fmt.Errorf("postgres: get chronicum: %w", err)
	}

	c.StartedAt = c.StartedAt.UTC()
	c.LastActivityAt = c.LastActivityAt.UTC()
	return c, nil
}

func (s *Store) migrate(ctx context.Context) error {
	btx, ok := s.db.(transactioner)
	if !ok {
		return s.runMigrations(ctx, s.db)
	}

	tx, err := btx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrateLockKey); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}

	if err := s.runMigrations(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration transaction: %w", err)
	}
	return nil
}

func (s *Store) runMigrations(ctx context.Context, q Querier) error {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		content, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		if _, err := q.Exec(ctx, string(content)); err != nil {
			return fmt.Errorf("run migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (s *Store) validateSchema(ctx context.Context) error {
	rowChronica := s.db.QueryRow(ctx, `SELECT id, owner_id, started_at, last_activity_at FROM "chronica" LIMIT 0`)
	if err := rowChronica.Scan(); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf(`table "chronica": %w`, err)
	}

	rowActa := s.db.QueryRow(ctx, `SELECT seq, id, chronicum_id, kind, actor_kind, actor, content, at, idempotency_key FROM "acta" LIMIT 0`)
	if err := rowActa.Scan(); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf(`table "acta": %w`, err)
	}

	return nil
}

func scanActum(row pgx.Row) (chronica.Actum, error) {
	var seq int64
	var a chronica.Actum
	err := row.Scan(
		&seq,
		&a.ID,
		&a.ChronicumID,
		&a.Kind,
		&a.ActorKind,
		&a.Actor,
		&a.Content,
		&a.At,
	)
	if err != nil {
		return chronica.Actum{}, err
	}
	a.At = a.At.UTC()
	return a, nil
}

func buildActaQuery(chronicumID string, q chronica.ActaQuery) (string, []any) {
	args := []any{chronicumID}
	nextPlaceholder := 2
	conditions := []string{"chronicum_id = $1"}

	if len(q.Kinds) > 0 {
		kinds := make([]string, len(q.Kinds))
		for i, kind := range q.Kinds {
			kinds[i] = string(kind)
		}
		conditions = append(conditions, fmt.Sprintf("kind = ANY($%d::text[])", nextPlaceholder))
		args = append(args, kinds)
		nextPlaceholder++
	}

	if len(q.ActorKinds) > 0 {
		actorKinds := make([]string, len(q.ActorKinds))
		for i, actorKind := range q.ActorKinds {
			actorKinds[i] = string(actorKind)
		}
		conditions = append(conditions, fmt.Sprintf("actor_kind = ANY($%d::text[])", nextPlaceholder))
		args = append(args, actorKinds)
		nextPlaceholder++
	}

	limitClause := ""
	if q.LastN > 0 {
		limitClause = fmt.Sprintf("LIMIT $%d", nextPlaceholder)
		args = append(args, q.LastN)
	}

	sqlQuery := fmt.Sprintf(`
		SELECT seq, id, chronicum_id, kind, actor_kind, actor, content, at
		FROM (
			SELECT seq, id, chronicum_id, kind, actor_kind, actor, content, at
			FROM "acta"
			WHERE %s
			ORDER BY seq DESC
			%s
		) sub
		ORDER BY seq ASC
	`, strings.Join(conditions, " AND "), limitClause)

	return sqlQuery, args
}
