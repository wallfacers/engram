// Package resolve implements the narrow, bounded source-resolution bridge:
// it reads exactly the frozen source IDs through a SourceResolver and rejects
// anything missing, inactive, drifted, or outside the candidate lineage. It
// has no query text or discovery method, so it cannot expand a frozen pool.
package resolve

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/contracts"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/validate"
)

// LedgerResolver adapts the Ledger's active-only batch API to SourceResolver.
// It has no query text or discovery method, so it cannot expand a frozen pool.
type LedgerResolver struct {
	Reader contracts.EvidenceBatchReader
}

func (resolver LedgerResolver) Resolve(ctx context.Context, sourceIDs []string) ([]contracts.Evidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if resolver.Reader == nil {
		return nil, fmt.Errorf("%w: nil ledger reader", contracts.ErrSourceUnavailable)
	}
	if err := validateResolveIDs(sourceIDs); err != nil {
		return nil, err
	}
	if len(sourceIDs) == 0 {
		return []contracts.Evidence{}, nil
	}
	records, err := resolver.Reader.GetMany(ctx, append([]string(nil), sourceIDs...))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: active ledger batch: %v", contracts.ErrSourceUnavailable, err)
	}
	resolved := make([]contracts.Evidence, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		record, ok := records[sourceID]
		if !ok {
			return nil, fmt.Errorf("%w: ledger omitted source %q", contracts.ErrSourceUnavailable, sourceID)
		}
		resolved = append(resolved, record)
	}
	return resolved, nil
}

// ResolveSources resolves the frozen allowlist IDs and rebuilds canonical
// Source records, failing closed on any missing, inactive, drifted, duplicate,
// or out-of-lineage record.
func ResolveSources(ctx context.Context, resolver contracts.SourceResolver, allowlist map[string]bool, sourceIDs []string) (map[string]contracts.Source, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if resolver == nil {
		return nil, fmt.Errorf("%w: nil resolver", contracts.ErrSourceUnavailable)
	}
	if err := validateResolveIDs(sourceIDs); err != nil {
		return nil, err
	}
	for _, sourceID := range sourceIDs {
		if !allowlist[sourceID] {
			return nil, fmt.Errorf("%w: source %q is outside frozen lineage", contracts.ErrSourceUnavailable, sourceID)
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
		return nil, fmt.Errorf("%w: resolve frozen lineage: %v", contracts.ErrSourceUnavailable, err)
	}
	if len(records) != len(sourceIDs) {
		return nil, fmt.Errorf("%w: resolver returned %d sources for %d requested IDs", contracts.ErrSourceUnavailable, len(records), len(sourceIDs))
	}
	resolved := make(map[string]contracts.Source, len(records))
	for _, record := range records {
		if !allowlist[record.ID] {
			return nil, fmt.Errorf("%w: resolver returned source %q outside lineage", contracts.ErrSourceUnavailable, record.ID)
		}
		if _, duplicate := resolved[record.ID]; duplicate {
			return nil, fmt.Errorf("%w: resolver returned duplicate source %q", contracts.ErrSourceUnavailable, record.ID)
		}
		if record.State != memory.EvidenceActive {
			return nil, fmt.Errorf("%w: evidence %q is %s", contracts.ErrSourceUnavailable, record.ID, record.State)
		}
		source := contracts.Source{
			ID:              record.ID,
			SourceSessionID: record.SourceSessionID,
			Speaker:         record.Speaker,
			Ordinal:         record.Ordinal,
			Content:         record.Content,
			ContentDigest:   record.ContentDigest,
			OccurredAt:      cloneSourceTime(record.OccurredAt),
		}
		if _, err := validate.ValidSource(source.ID, allowlist, map[string]contracts.Source{source.ID: source}); err != nil {
			return nil, err
		}
		resolved[source.ID] = source
	}
	for _, sourceID := range sourceIDs {
		if _, ok := resolved[sourceID]; !ok {
			return nil, fmt.Errorf("%w: resolver omitted source %q", contracts.ErrSourceUnavailable, sourceID)
		}
	}
	return resolved, nil
}

func validateResolveIDs(sourceIDs []string) error {
	seen := make(map[string]bool, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		if sourceID == "" || seen[sourceID] {
			return fmt.Errorf("%w: resolver IDs must be non-empty and unique", contracts.ErrSourceUnavailable)
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

// CanonicalResolverIDs is kept at the boundary so callers always pass a
// stable list, which makes resolver receipts auditable and testable.
func CanonicalResolverIDs(allowlist map[string]bool) []string {
	ids := make([]string, 0, len(allowlist))
	for sourceID := range allowlist {
		ids = append(ids, sourceID)
	}
	sort.Strings(ids)
	return ids
}
