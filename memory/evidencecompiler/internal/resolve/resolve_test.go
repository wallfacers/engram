package resolve

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/contracts"
)

func TestLedgerResolverReadsRequestedActiveEvidenceInOneBatch(t *testing.T) {
	reader := &sourceTestReader{records: map[string]memory.Evidence{
		"src-a": activeTestEvidence("src-a", "Alice met Bob."),
		"src-b": activeTestEvidence("src-b", "Bob chose tea."),
	}}
	resolver := LedgerResolver{Reader: reader}
	resolved, err := resolver.Resolve(context.Background(), []string{"src-b", "src-a"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if reader.calls != 1 || len(reader.requested) != 2 || reader.requested[0] != "src-b" || reader.requested[1] != "src-a" {
		t.Fatalf("batch reader calls/IDs = %d/%v, want one request in frozen order", reader.calls, reader.requested)
	}
	if len(resolved) != 2 || resolved[0].ID != "src-b" || resolved[1].ID != "src-a" {
		t.Fatalf("Resolve() = %+v, want requested deterministic order", resolved)
	}
}

func TestResolveSourcesFailsClosedForMissingUnavailableOrDriftedEvidence(t *testing.T) {
	allowlist := map[string]bool{"src-a": true}
	for name, records := range map[string][]memory.Evidence{
		"missing":      nil,
		"tombstoned":   {{ID: "src-a", Content: "Alice", ContentDigest: sha256Hex("Alice"), State: memory.EvidenceTombstoned}},
		"purged":       {{ID: "src-a", Content: "Alice", ContentDigest: sha256Hex("Alice"), State: memory.EvidencePurged}},
		"digest-drift": {{ID: "src-a", Content: "Alice", ContentDigest: sha256Hex("Bob"), State: memory.EvidenceActive}},
	} {
		t.Run(name, func(t *testing.T) {
			resolver := sourceTestResolver{records: records}
			if _, err := ResolveSources(context.Background(), resolver, allowlist, []string{"src-a"}); !errors.Is(err, contracts.ErrSourceUnavailable) {
				t.Fatalf("ResolveSources() error = %v, want ErrSourceUnavailable", err)
			}
		})
	}
}

func activeTestEvidence(id, content string) memory.Evidence {
	return memory.Evidence{ID: id, Content: content, ContentDigest: sha256Hex(content), State: memory.EvidenceActive}
}

type sourceTestReader struct {
	records   map[string]memory.Evidence
	calls     int
	requested []string
}

func (reader *sourceTestReader) GetMany(_ context.Context, ids []string) (map[string]memory.Evidence, error) {
	reader.calls++
	reader.requested = append([]string(nil), ids...)
	return reader.records, nil
}

type sourceTestResolver struct {
	records []memory.Evidence
	err     error
}

func (resolver sourceTestResolver) Resolve(_ context.Context, _ []string) ([]contracts.Evidence, error) {
	if resolver.err != nil {
		return nil, resolver.err
	}
	return resolver.records, nil
}

func sha256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:])
}
