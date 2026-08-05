package eventstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Project is the rebuildable on-disk event projection for one conversation.
// It is a pure derived view over Evidence: deleting or rebuilding it never
// touches the Ledger (FR-003). A config-hash change invalidates it wholesale.
type Project struct {
	ConfigHash     string            `json:"config_hash"`
	ConversationID string            `json:"conversation_id"`
	BuiltAt        string            `json:"built_at"`
	Events         []Event           `json:"events"`
	Summaries      []RelationSummary `json:"summaries,omitempty"`
}

// BuildProject assembles a project from extracted events. Events are sorted by
// conversation + ordinal of their first source id for a deterministic render.
func BuildProject(conversationID, configHash string, events []Event, summaries []RelationSummary) *Project {
	evs := append([]Event(nil), events...)
	sort.SliceStable(evs, func(i, j int) bool {
		return strings.Join(evs[i].SourceLedgerIDs, ",") < strings.Join(evs[j].SourceLedgerIDs, ",")
	})
	return &Project{
		ConfigHash:     configHash,
		ConversationID: conversationID,
		BuiltAt:        time.Now().UTC().Format(time.RFC3339),
		Events:         evs,
		Summaries:      summaries,
	}
}

// Write persists the projection to path (atomically via temp+rename). The
// caller is responsible for a config-hash-namespaced path.
func (p *Project) Write(path string) error {
	if p == nil {
		return fmt.Errorf("eventstore: nil project")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("eventstore: mkdir project dir: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("eventstore: marshal project: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("eventstore: write project tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("eventstore: rename project: %w", err)
	}
	return nil
}

// LoadProject reads a projection back from disk. A missing file yields an
// os.IsNotExist error so callers can treat it as "not built yet".
func LoadProject(path string) (*Project, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Project
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("eventstore: unmarshal project %s: %w", path, err)
	}
	return &p, nil
}

// Render produces the answerer context text for this projection: one line per
// event with speaker, absolute/relative time, facts and relations, followed by
// any relation summaries. The output is bounded by maxRunes (FR-007).
func (p *Project) Render(maxRunes int) string {
	if p == nil {
		return ""
	}
	if maxRunes <= 0 {
		maxRunes = 4000
	}
	var b strings.Builder
	for i := range p.Events {
		ev := &p.Events[i]
		fmt.Fprintf(&b, "[EVENT %d] (speaker: %s", i+1, ev.Speaker)
		if ev.AbsoluteTS != "" {
			fmt.Fprintf(&b, ", %s", ev.AbsoluteTS)
		}
		if ev.RelativeRef != "" {
			fmt.Fprintf(&b, ", rel: %q", ev.RelativeRef)
		}
		b.WriteString(")\n")
		for _, f := range ev.FactEntries {
			fmt.Fprintf(&b, "  FACT: %s\n", oneLine(f.Text))
		}
		for _, r := range ev.RelationEntries {
			fmt.Fprintf(&b, "  RELATION (%s): %s\n", r.RelationType, oneLine(r.Text))
		}
		if b.Len() > maxRunes {
			trimProject(&b, maxRunes)
			break
		}
	}
	for i := range p.Summaries {
		s := &p.Summaries[i]
		fmt.Fprintf(&b, "[SUMMARY] %s\n", oneLine(s.Text))
		if b.Len() > maxRunes {
			trimProject(&b, maxRunes)
			break
		}
	}
	return b.String()
}

func trimProject(b *strings.Builder, maxRunes int) {
	s := []rune(b.String())
	if len(s) > maxRunes {
		cut := s[:maxRunes]
		b.Reset()
		b.WriteString(string(cut))
		b.WriteString("\n…(truncated)")
	}
}
