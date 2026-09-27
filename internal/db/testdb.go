package db

import (
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"
)

// OpenTest gives a test its own throwaway schema on the Postgres at
// PARATRACK_TEST_PG (scripts/test.sh starts one), dropped when the
// test ends.
func OpenTest(tb testing.TB) (*DB, error) {
	tb.Helper()
	url := os.Getenv("PARATRACK_TEST_PG")
	if url == "" {
		tb.Fatal("PARATRACK_TEST_PG is not set: run scripts/test.sh")
	}
	admin, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("t_%d_%d", time.Now().UnixNano(), rand.Int63())
	for _, st := range []string{`CREATE EXTENSION IF NOT EXISTS citext`, `CREATE SCHEMA ` + name} {
		if _, err := admin.Exec(st); err != nil {
			admin.Close()
			return nil, err
		}
	}
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	// search_path keeps citext (in public) visible.
	d, err := Open(url + sep + "search_path=" + name + ",public")
	tb.Cleanup(func() {
		if d != nil {
			d.Close()
		}
		_, _ = admin.Exec(`DROP SCHEMA ` + name + ` CASCADE`)
		admin.Close()
	})
	return d, err
}
