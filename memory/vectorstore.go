package memory

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/wallfacers/engram/embedding"
)

// VectorStore is the SQLite accessor for memory_embeddings (migration v8). It
// mirrors EntryStore's design: a thin wrapper over the shared *sql.DB, keeping
// the vector lifecycle local to the memory package.
type VectorStore struct {
	db *sql.DB
}

// NewVectorStore wraps the shared *sql.DB.
func NewVectorStore(db *sql.DB) *VectorStore {
	return &VectorStore{db: db}
}

// Put upserts the vector for an entry, tagging it with the producing model so a
// later model change is detectable.
func (v *VectorStore) Put(ctx context.Context, entryName, model string, vec []float32, at time.Time) error {
	_, err := v.db.ExecContext(ctx,
		`INSERT INTO memory_embeddings(entry_name, model, dims, vec, updated_at)
		 VALUES (?,?,?,?,?)
		 ON CONFLICT(entry_name) DO UPDATE SET
			model      = excluded.model,
			dims       = excluded.dims,
			vec        = excluded.vec,
			updated_at = excluded.updated_at`,
		entryName, model, len(vec), embedding.EncodeVector(vec), at.UTC().UnixMicro())
	if err != nil {
		return fmt.Errorf("memory: put embedding %q: %w", entryName, err)
	}
	return nil
}

// putIfOwnerUnchanged atomically upserts a base or shadow vector only while its
// source entry is still the same revision that produced the embedding input.
// This prevents a slow write-behind call from recreating an orphan vector after
// Delete/Merge, or publishing a stale vector after a same-name rewrite.
func (v *VectorStore) putIfOwnerUnchanged(ctx context.Context, vectorName string, owner *Entry, model string, vec []float32, at time.Time) (bool, error) {
	if owner == nil {
		return false, nil
	}
	res, err := v.db.ExecContext(ctx,
		`INSERT INTO memory_embeddings(entry_name, model, dims, vec, updated_at)
		 SELECT ?,?,?,?,?
		 WHERE EXISTS (
			SELECT 1 FROM memory_entries
			WHERE name = ? AND id = ? AND revision = ?
		 )
		 ON CONFLICT(entry_name) DO UPDATE SET
			model      = excluded.model,
			dims       = excluded.dims,
			vec        = excluded.vec,
			updated_at = excluded.updated_at`,
		vectorName, model, len(vec), embedding.EncodeVector(vec), at.UTC().UnixMicro(),
		owner.Name, owner.ID, owner.Revision)
	if err != nil {
		return false, fmt.Errorf("memory: put embedding %q if owner unchanged: %w", vectorName, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("memory: put embedding %q rows: %w", vectorName, err)
	}
	return n == 1, nil
}

// LoadAllForModel returns every stored vector whose model matches the given
// model, decoded. Rows for other models are skipped (they are stale and will be
// rebuilt). Used to assemble the semantic-search candidate set.
func (v *VectorStore) LoadAllForModel(ctx context.Context, model string) (map[string][]float32, error) {
	rows, err := v.db.QueryContext(ctx,
		`SELECT entry_name, vec FROM memory_embeddings WHERE model = ?`, model)
	if err != nil {
		return nil, fmt.Errorf("memory: load embeddings: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	out := make(map[string][]float32)
	for rows.Next() {
		var name string
		var blob []byte
		if err := rows.Scan(&name, &blob); err != nil {
			return nil, fmt.Errorf("memory: scan embedding: %w", err)
		}
		vec, err := embedding.DecodeVector(blob)
		if err != nil {
			// A single corrupt row must not break the whole search.
			continue
		}
		out[name] = vec
	}
	return out, rows.Err()
}

// NamesMissingModel returns entry names that have no vector row for the given
// model (either no row at all, or a row tagged with a different model). These
// are the entries the backfill sweep must (re-)embed.
func (v *VectorStore) NamesMissingModel(ctx context.Context, model string) ([]string, error) {
	rows, err := v.db.QueryContext(ctx,
		`SELECT e.name
		   FROM memory_entries e
		   LEFT JOIN memory_embeddings m
		     ON m.entry_name = e.name AND m.model = ?
		  WHERE m.entry_name IS NULL
		  ORDER BY e.name`, model)
	if err != nil {
		return nil, fmt.Errorf("memory: names missing model: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("memory: scan missing name: %w", err)
		}
		out = append(out, name)
	}
	return out, rows.Err()
}
