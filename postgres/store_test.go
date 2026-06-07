package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.naturallyfunny.dev/chronica"
	"go.naturallyfunny.dev/chronica/postgres"
	"go.naturallyfunny.dev/chronica/storeconformance"
)

func TestConformance(t *testing.T) {
	dsn := os.Getenv("CHRONICA_TEST_DSN")
	if dsn == "" {
		t.Skip("CHRONICA_TEST_DSN is not set; skipping PostgreSQL conformance tests")
	}

	ctx := context.Background()
	newStore := func() *postgres.Store {
		t.Helper()

		schemaName := randomSchemaName(t)
		config, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			t.Fatalf("parse CHRONICA_TEST_DSN: %v", err)
		}
		if config.ConnConfig.RuntimeParams == nil {
			config.ConnConfig.RuntimeParams = make(map[string]string)
		}
		config.ConnConfig.RuntimeParams["search_path"] = schemaName

		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatalf("create pgx pool: %v", err)
		}

		if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)); err != nil {
			pool.Close()
			t.Fatalf("create schema %s: %v", schemaName, err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, fmt.Sprintf(`DROP SCHEMA "%s" CASCADE`, schemaName))
			pool.Close()
		})

		store, err := postgres.NewStore(ctx, pool, postgres.WithAutoMigrate())
		if err != nil {
			t.Fatalf("new PostgreSQL store: %v", err)
		}
		return store
	}

	storeconformance.RunTest(t, func() chronica.Store {
		return newStore()
	})
	storeconformance.RunIdempotentTest(t, func() chronica.IdempotentStore {
		return newStore()
	})
}

func TestBuildActaQuery(t *testing.T) {
	q1, args1 := postgres.ExportBuildActaQuery("session-1", chronica.ActaQuery{})
	if !strings.Contains(q1, "WHERE chronicum_id = $1") {
		t.Error("SQL should include the chronicum filter")
	}
	if strings.Contains(q1, "ANY") || strings.Contains(q1, "LIMIT") {
		t.Error("SQL should not include inactive filters or limits")
	}
	if len(args1) != 1 || args1[0] != "session-1" {
		t.Fatalf("args: want [session-1], got %#v", args1)
	}

	q2, args2 := postgres.ExportBuildActaQuery("session-1", chronica.ActaQuery{
		LastN: 5,
		Kinds: []chronica.ActumKind{chronica.ActumMessage},
	})
	if !strings.Contains(q2, "kind = ANY($2::text[])") {
		t.Error("SQL should include kind filter")
	}
	if !strings.Contains(q2, "LIMIT $3") {
		t.Error("SQL should include limit after kind filter")
	}
	if len(args2) != 3 {
		t.Fatalf("args: want 3, got %d: %#v", len(args2), args2)
	}

	q3, args3 := postgres.ExportBuildActaQuery("session-1", chronica.ActaQuery{
		LastN:      5,
		ActorKinds: []chronica.ActorKind{chronica.ActorHuman, chronica.ActorAgent},
	})
	if !strings.Contains(q3, "actor_kind = ANY($2::text[])") {
		t.Error("SQL should include actor kind filter")
	}
	if !strings.Contains(q3, "LIMIT $3") {
		t.Error("SQL should include limit after actor kind filter")
	}
	if len(args3) != 3 {
		t.Fatalf("args: want 3, got %d: %#v", len(args3), args3)
	}
}

func randomSchemaName(t *testing.T) string {
	t.Helper()

	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	return "chronica_test_" + hex.EncodeToString(b[:])
}
