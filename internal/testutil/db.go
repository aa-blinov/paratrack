package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
)

// OpenTest gives a test its own throwaway database on the Postgres at
// PARATRACK_TEST_PG (scripts/test.sh starts one), dropped when the test ends.
// The database is a copy of a template migrated once per test binary: a
// fresh migration cost ~1.8s a test, a template copy ~0.15s, since the copy skips the DDL pass.
func OpenTest(tb testing.TB) (*db.DB, error) {
	tb.Helper()
	base := os.Getenv("PARATRACK_TEST_PG")
	if base == "" {
		tb.Fatal("PARATRACK_TEST_PG is not set: run scripts/test.sh")
	}
	template, err := migratedTemplate(base)
	if err != nil {
		return nil, err
	}
	admin, err := sql.Open("pgx", base)
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("t_%d_%d", time.Now().UnixNano(), rand.Int63())
	if _, err := admin.Exec(`CREATE DATABASE ` + name + ` TEMPLATE ` + template); err != nil {
		admin.Close()
		return nil, err
	}
	config := testConfig(databaseURL(base, name))
	config.SchemaCurrent = true
	d, err := db.OpenConfiguredContext(tb.Context(), config)
	tb.Cleanup(func() {
		if d != nil {
			d.Close()
		}
		_, _ = admin.Exec(`DROP DATABASE ` + name + ` WITH (FORCE)`)
		admin.Close()
	})
	return d, err
}

var (
	templateOnce sync.Once
	templateName string
	templateErr  error
)

// migratedTemplate creates and migrates this binary's template database once.
// ponytail: the template outlives the run; the test Postgres is thrown away
// with it, so nothing drops it.
func migratedTemplate(base string) (string, error) {
	templateOnce.Do(func() {
		name := fmt.Sprintf("tpl_%d_%d", os.Getpid(), rand.Int63())
		admin, err := sql.Open("pgx", base)
		if err != nil {
			templateErr = err
			return
		}
		defer admin.Close()
		if _, err := admin.Exec(`CREATE DATABASE ` + name); err != nil {
			templateErr = err
			return
		}
		ext, err := sql.Open("pgx", databaseURL(base, name))
		if err != nil {
			templateErr = err
			return
		}
		_, err = ext.Exec(`CREATE EXTENSION IF NOT EXISTS citext`)
		ext.Close()
		if err != nil {
			templateErr = err
			return
		}
		d, err := db.OpenConfiguredContext(context.Background(), testConfig(databaseURL(base, name)))
		if err != nil {
			templateErr = err
			return
		}
		d.Close()
		templateName = name
	})
	return templateName, templateErr
}

func databaseURL(base, name string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	u.Path = "/" + name
	return u.String()
}

func testConfig(dsn string) db.Config {
	return db.Config{
		URL:       dsn,
		SecretKey: os.Getenv("PARATRACK_SECRET_KEY"),
		Logger:    log.New(io.Discard, "", 0),
		Now:       time.Now,
	}
}
