package memory

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wallfacers/engram/store"
	"modernc.org/sqlite"
)

const projectionCounterDriverName = "engram-projection-query-counter"

var (
	projectionCounterDriverOnce sync.Once
	projectionQueryCount        atomic.Int64
)

// queryCountingDriver is test-only instrumentation around modernc SQLite. It
// counts only QueryContext calls made through the projection lookup DB, so the
// assertion catches a per-candidate query regression without changing product
// code or relying on timing thresholds.
type queryCountingDriver struct {
	inner driver.Driver
}

func (d queryCountingDriver) Open(dsn string) (driver.Conn, error) {
	conn, err := d.inner.Open(dsn)
	if err != nil {
		return nil, err
	}
	return &queryCountingConn{Conn: conn}, nil
}

type queryCountingConn struct {
	driver.Conn
}

func (c *queryCountingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	projectionQueryCount.Add(1)
	queryer, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return queryer.QueryContext(ctx, query, args)
}

func openProjectionCountingDB(t testing.TB, dsn string) *sql.DB {
	t.Helper()
	projectionCounterDriverOnce.Do(func() {
		sql.Register(projectionCounterDriverName, queryCountingDriver{inner: &sqlite.Driver{}})
	})
	db, err := sql.Open(projectionCounterDriverName, dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedProjectionFixture(t testing.TB, size int) (*store.Store, string, []string) {
	t.Helper()
	ctx := context.Background()
	dsn := fmt.Sprintf("file:projection-fixture-%d?mode=memory&cache=shared", time.Now().UnixNano())
	opened, err := store.Open(ctx, store.Options{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	tx, err := opened.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck
	evidenceStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO memory_evidence(
			id, source_type, external_source_id, source_session_id, speaker, ordinal,
			content, occurred_at, recorded_at, content_digest
		) VALUES (?, 'message', ?, 'fixture-session', 'user', ?, 'fixture source', NULL, ?, 'fixture')`)
	if err != nil {
		t.Fatal(err)
	}
	defer evidenceStmt.Close() //nolint:errcheck
	headStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO memory_evidence_heads(evidence_id, state, last_seq, revision, changed_at)
		VALUES (?, 'active', 1, 1, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	defer headStmt.Close() //nolint:errcheck
	projectionStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO memory_projections(
			id, kind, object_key, state, builder, builder_version, config_hash, built_at, revision
		) VALUES (?, 'atomic_fact', ?, 'active', 'fixture', '1', 'fixture', ?, 1)`)
	if err != nil {
		t.Fatal(err)
	}
	defer projectionStmt.Close() //nolint:errcheck
	sourceStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO memory_projection_sources(
			projection_id, source_order, evidence_id, full_source, start_char, end_char, span_digest, relation
		) VALUES (?, 0, ?, 1, NULL, NULL, NULL, 'supports')`)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceStmt.Close() //nolint:errcheck

	now := time.Now().UTC().UnixMicro()
	projectionIDs := make([]string, size)
	for index := range projectionIDs {
		evidenceID := fmt.Sprintf("fixture-evidence-%06d", index)
		projectionID := fmt.Sprintf("fixture-projection-%06d", index)
		projectionIDs[index] = projectionID
		if _, err := evidenceStmt.ExecContext(ctx, evidenceID, evidenceID, index, now); err != nil {
			t.Fatal(err)
		}
		if _, err := headStmt.ExecContext(ctx, evidenceID, now); err != nil {
			t.Fatal(err)
		}
		if _, err := projectionStmt.ExecContext(ctx, projectionID, fmt.Sprintf("fixture-object-%06d", index), now); err != nil {
			t.Fatal(err)
		}
		if _, err := sourceStmt.ExecContext(ctx, projectionID, evidenceID); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return opened, dsn, projectionIDs
}

func TestProjectionSourcesBatchLookupHasNoPerCandidateQueries(t *testing.T) {
	_, dsn, projectionIDs := seedProjectionFixture(t, 1201)
	countingDB := openProjectionCountingDB(t, dsn)
	projectionQueryCount.Store(0)
	sources, err := NewProjectionStore(countingDB).SourcesByProjectionIDs(context.Background(), projectionIDs)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != len(projectionIDs) {
		t.Fatalf("source result count = %d, want %d", len(sources), len(projectionIDs))
	}
	for _, projectionID := range projectionIDs {
		if len(sources[projectionID]) != 1 {
			t.Fatalf("projection %q sources = %+v, want one", projectionID, sources[projectionID])
		}
	}
	if got, want := projectionQueryCount.Load(), int64(3); got != want {
		t.Fatalf("batch lineage queries = %d, want exactly %d for 1,201 candidates", got, want)
	}
}

func BenchmarkProjectionSourcesBatch100K(b *testing.B) {
	_, dsn, projectionIDs := seedProjectionFixture(b, 100_000)
	countingDB := openProjectionCountingDB(b, dsn)
	projections := NewProjectionStore(countingDB)
	const expectedQueries = int64(200)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		projectionQueryCount.Store(0)
		sources, err := projections.SourcesByProjectionIDs(context.Background(), projectionIDs)
		if err != nil {
			b.Fatal(err)
		}
		if len(sources) != len(projectionIDs) {
			b.Fatalf("source result count = %d, want %d", len(sources), len(projectionIDs))
		}
		if got := projectionQueryCount.Load(); got != expectedQueries {
			b.Fatalf("batch lineage queries = %d, want exactly %d for 100k candidates", got, expectedQueries)
		}
	}
}
