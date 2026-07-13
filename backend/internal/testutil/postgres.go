package testutil

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

type PostgresSchema struct {
	Name string
	URL  string
}

func NewPostgresSchema(t testing.TB) PostgresSchema {
	t.Helper()

	databaseURL := strings.TrimSpace(os.Getenv("ASTER_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("ASTER_TEST_DATABASE_URL is not set")
	}

	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse ASTER_TEST_DATABASE_URL: %v", err)
	}

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("generate test schema name: %v", err)
	}
	schemaName := "asterrouter_test_" + hex.EncodeToString(suffix[:])

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open test postgres: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+quoteIdentifier(schemaName)); err != nil {
		t.Fatalf("create test schema: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		cleanupDB, openErr := sql.Open("postgres", databaseURL)
		if openErr != nil {
			t.Errorf("open postgres for schema cleanup: %v", openErr)
			return
		}
		defer cleanupDB.Close()
		if _, dropErr := cleanupDB.ExecContext(cleanupCtx, `DROP SCHEMA IF EXISTS `+quoteIdentifier(schemaName)+` CASCADE`); dropErr != nil {
			t.Errorf("drop test schema %s: %v", schemaName, dropErr)
		}
	})

	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()
	return PostgresSchema{Name: schemaName, URL: parsed.String()}
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func OpenPostgres(t testing.TB, databaseURL string) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("ping postgres: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close postgres: %v", err)
		}
	})
	return db
}

func UniqueID(prefix string) string {
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		panic(fmt.Sprintf("generate unique id: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(suffix[:])
}
