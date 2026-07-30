package evidencecompiler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/wallfacers/engram/memory"
)

// Evidence is the immutable Ledger record used by the narrow resolver bridge.
// It is an alias, not a second source representation or a copied store model.
type Evidence = memory.Evidence

// SourceResolver is intentionally the only read authority available to the
// Compiler. Its finite ID list comes from frozen candidate lineage.
type SourceResolver interface {
	Resolve(ctx context.Context, sourceIDs []string) ([]Evidence, error)
}

// EvidenceBatchReader is the active-Ledger capability required by
// LedgerResolver. GetMany is already batched by LedgerStore and fails if any
// requested record is unavailable.
type EvidenceBatchReader interface {
	GetMany(ctx context.Context, evidenceIDs []string) (map[string]memory.Evidence, error)
}

// LedgerResolver adapts the Ledger's active-only batch API to SourceResolver.
// It has no query text or discovery method, so it cannot expand a frozen pool.
type LedgerResolver struct {
	Reader EvidenceBatchReader
}

func (resolver LedgerResolver) Resolve(ctx context.Context, sourceIDs []string) ([]Evidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if resolver.Reader == nil {
		return nil, fmt.Errorf("%w: nil ledger reader", ErrSourceUnavailable)
	}
	if err := validateResolveIDs(sourceIDs); err != nil {
		return nil, err
	}
	if len(sourceIDs) == 0 {
		return []Evidence{}, nil
	}
	records, err := resolver.Reader.GetMany(ctx, append([]string(nil), sourceIDs...))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: active ledger batch: %v", ErrSourceUnavailable, err)
	}
	resolved := make([]Evidence, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		record, ok := records[sourceID]
		if !ok {
			return nil, fmt.Errorf("%w: ledger omitted source %q", ErrSourceUnavailable, sourceID)
		}
		resolved = append(resolved, record)
	}
	return resolved, nil
}

func resolveSources(ctx context.Context, resolver SourceResolver, allowlist map[string]bool, sourceIDs []string) (map[string]Source, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if resolver == nil {
		return nil, fmt.Errorf("%w: nil resolver", ErrSourceUnavailable)
	}
	if err := validateResolveIDs(sourceIDs); err != nil {
		return nil, err
	}
	for _, sourceID := range sourceIDs {
		if !allowlist[sourceID] {
			return nil, fmt.Errorf("%w: source %q is outside frozen lineage", ErrSourceUnavailable, sourceID)
		}
	}
	records, err := resolver.Resolve(ctx, append([]string(nil), sourceIDs...))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: resolve frozen lineage: %v", ErrSourceUnavailable, err)
	}
	if len(records) != len(sourceIDs) {
		return nil, fmt.Errorf("%w: resolver returned %d sources for %d requested IDs", ErrSourceUnavailable, len(records), len(sourceIDs))
	}
	resolved := make(map[string]Source, len(records))
	for _, record := range records {
		if !allowlist[record.ID] {
			return nil, fmt.Errorf("%w: resolver returned source %q outside lineage", ErrSourceUnavailable, record.ID)
		}
		if _, duplicate := resolved[record.ID]; duplicate {
			return nil, fmt.Errorf("%w: resolver returned duplicate source %q", ErrSourceUnavailable, record.ID)
		}
		if record.State != memory.EvidenceActive {
			return nil, fmt.Errorf("%w: evidence %q is %s", ErrSourceUnavailable, record.ID, record.State)
		}
		source := Source{
			ID:              record.ID,
			SourceSessionID: record.SourceSessionID,
			Speaker:         record.Speaker,
			Ordinal:         record.Ordinal,
			Content:         record.Content,
			ContentDigest:   record.ContentDigest,
			OccurredAt:      cloneSourceTime(record.OccurredAt),
		}
		if _, err := validSource(source.ID, allowlist, map[string]Source{source.ID: source}); err != nil {
			return nil, err
		}
		resolved[source.ID] = source
	}
	for _, sourceID := range sourceIDs {
		if _, ok := resolved[sourceID]; !ok {
			return nil, fmt.Errorf("%w: resolver omitted source %q", ErrSourceUnavailable, sourceID)
		}
	}
	return resolved, nil
}

func validateResolveIDs(sourceIDs []string) error {
	seen := make(map[string]bool, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		if sourceID == "" || seen[sourceID] {
			return fmt.Errorf("%w: resolver IDs must be non-empty and unique", ErrSourceUnavailable)
		}
		seen[sourceID] = true
	}
	return nil
}

func cloneSourceTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	clone := value.UTC()
	return &clone
}

// canonicalResolverIDs is kept at the boundary so callers always pass a
// stable list, which makes resolver receipts auditable and testable.
func canonicalResolverIDs(allowlist map[string]bool) []string {
	ids := make([]string, 0, len(allowlist))
	for sourceID := range allowlist {
		ids = append(ids, sourceID)
	}
	sort.Strings(ids)
	return ids
}
