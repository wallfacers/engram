package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/pipeline"
	"github.com/wallfacers/engram/store"
)

type toolAdapter struct {
	registry *Registry
}

type memoryWriteInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"target namespace; empty uses default"`
	Name      string `json:"name" jsonschema:"unique non-empty memory name"`
	Content   string `json:"content" jsonschema:"memory content"`
	Trigger   string `json:"trigger,omitempty" jsonschema:"optional retrieval trigger"`
	Category  string `json:"category,omitempty" jsonschema:"optional memory category"`
	Pinned    bool   `json:"pinned,omitempty" jsonschema:"whether this memory is pinned"`
}

type memoryWriteOutput struct {
	Name    string `json:"name"`
	Written bool   `json:"written"`
}

type memorySearchInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"target namespace; empty uses default"`
	Query     string `json:"query" jsonschema:"search query"`
	Limit     *int   `json:"limit,omitempty" jsonschema:"positive result limit; omitted uses 8"`
}

type memorySearchOutput struct {
	Results  []searchResultOutput `json:"results"`
	Degraded degradedOutput       `json:"degraded"`
}

type searchResultOutput struct {
	Name      string     `json:"name"`
	Trigger   string     `json:"trigger"`
	Snippet   string     `json:"snippet"`
	Content   string     `json:"content"`
	Score     float64    `json:"score"`
	EventDate *time.Time `json:"event_date"`
	CreatedAt time.Time  `json:"created_at"`
}

type degradedOutput struct {
	Semantic bool   `json:"semantic"`
	Reason   string `json:"reason"`
}

type memoryListInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"target namespace; empty uses default"`
}

type memoryListOutput struct {
	Entries []entryOutput `json:"entries"`
}

type memoryGetInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"target namespace; empty uses default"`
	Name      string `json:"name" jsonschema:"memory name"`
}

type memoryGetOutput struct {
	Entry entryOutput `json:"entry"`
}

type memoryDeleteInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"target namespace; empty uses default"`
	Name      string `json:"name" jsonschema:"memory name"`
}

type memoryDeleteOutput struct {
	Deleted bool `json:"deleted"`
}

type memoryIngestInput struct {
	Namespace string                `json:"namespace,omitempty" jsonschema:"target namespace; empty uses default"`
	Messages  []memoryIngestMessage `json:"messages" jsonschema:"conversation turns to extract facts from"`
}

type memoryIngestMessage struct {
	Role string `json:"role" jsonschema:"message author: user or assistant"`
	Text string `json:"text" jsonschema:"message text"`
}

type memoryIngestOutput struct {
	ExtractedCount int                 `json:"extracted_count"`
	Entries        []ingestEntryOutput `json:"entries"`
}

// memoryIngestV2Input is the lossless ingest contract. Unlike the legacy
// memory_ingest tool, it is available without an extractor and requires the
// caller's stable turn identity and session ordering.
type memoryIngestV2Input struct {
	Namespace string                  `json:"namespace,omitempty" jsonschema:"target namespace; empty uses default"`
	SessionID string                  `json:"session_id" jsonschema:"non-empty caller session ID"`
	Messages  []memoryIngestV2Message `json:"messages" jsonschema:"source-identified conversation turns in ordinal order"`
	Extract   *bool                   `json:"extract,omitempty" jsonschema:"whether to run optional fact extraction; defaults to true"`
}

type memoryIngestV2Message struct {
	SourceID   string     `json:"source_id" jsonschema:"stable caller source ID within the session"`
	Role       string     `json:"role" jsonschema:"message author: user or assistant"`
	Text       string     `json:"text" jsonschema:"immutable original message text"`
	Ordinal    int        `json:"ordinal" jsonschema:"zero-based position in this ingest batch"`
	OccurredAt *time.Time `json:"occurred_at,omitempty" jsonschema:"optional RFC3339 message timestamp"`
}

type memoryIngestV2Output struct {
	Evidence       []evidenceReceiptOutput `json:"evidence"`
	ExtractedCount int                     `json:"extracted_count"`
	Entries        []ingestEntryOutput     `json:"entries"`
	Degraded       []string                `json:"degraded"`
}

type evidenceReceiptOutput struct {
	SourceID   string `json:"source_id"`
	EvidenceID string `json:"evidence_id"`
	State      string `json:"state"`
}

type memoryEvidenceGetInput struct {
	Namespace   string   `json:"namespace,omitempty" jsonschema:"target namespace; empty uses default"`
	EvidenceIDs []string `json:"evidence_ids" jsonschema:"active evidence IDs in desired output order"`
}

type memoryEvidenceGetOutput struct {
	Evidence []evidenceOutput `json:"evidence"`
}

// evidenceOutput intentionally contains source content only for an active
// explicit get. Lifecycle events and errors never echo content.
type evidenceOutput struct {
	EvidenceID string     `json:"evidence_id"`
	SourceID   string     `json:"source_id"`
	SessionID  string     `json:"session_id"`
	Role       string     `json:"role"`
	Ordinal    int        `json:"ordinal"`
	Content    string     `json:"content"`
	OccurredAt *time.Time `json:"occurred_at"`
	RecordedAt time.Time  `json:"recorded_at"`
	State      string     `json:"state"`
}

type memoryEvidenceLifecycleInput struct {
	Namespace  string `json:"namespace,omitempty" jsonschema:"target namespace; empty uses default"`
	EvidenceID string `json:"evidence_id" jsonschema:"evidence ID"`
	RequestID  string `json:"request_id,omitempty" jsonschema:"optional idempotency request ID"`
	ReasonCode string `json:"reason_code" jsonschema:"non-empty non-content lifecycle reason code"`
}

type memoryEvidenceLifecycleOutput struct {
	EvidenceID        string `json:"evidence_id"`
	State             string `json:"state"`
	CheckpointPending bool   `json:"checkpoint_pending,omitempty"`
}

type ingestEntryOutput struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type entryOutput struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Trigger         string     `json:"trigger"`
	Content         string     `json:"content"`
	Pinned          bool       `json:"pinned"`
	Durability      string     `json:"durability"`
	Category        string     `json:"category"`
	HitCount        int        `json:"hit_count"`
	LastUsedAt      *time.Time `json:"last_used_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CharCount       int        `json:"char_count"`
	SourceSessionID string     `json:"source_session_id"`
	EventDate       *time.Time `json:"event_date"`
	EventStart      *time.Time `json:"event_start"`
	EventEnd        *time.Time `json:"event_end"`
	FactSource      string     `json:"fact_source"`
}

func (a *toolAdapter) memoryWrite(ctx context.Context, _ *mcp.CallToolRequest, input memoryWriteInput) (*mcp.CallToolResult, memoryWriteOutput, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, memoryWriteOutput{}, errors.New("memory name is required")
	}
	budgets := memory.DefaultBudgets()
	if err := budgets.CheckEntryContent(input.Content); err != nil {
		var tooLarge memory.ErrMemoryTooLarge
		if errors.As(err, &tooLarge) {
			return nil, memoryWriteOutput{}, fmt.Errorf("memory content rejected: limit=%d actual=%d", tooLarge.Limit, tooLarge.Actual)
		}
		return nil, memoryWriteOutput{}, err
	}
	if err := budgets.CheckTrigger(input.Trigger); err != nil {
		return nil, memoryWriteOutput{}, err
	}
	handle, release, err := a.registry.Acquire(ctx, input.Namespace)
	if err != nil {
		return nil, memoryWriteOutput{}, err
	}
	defer release()
	entry := &memory.Entry{
		Name:      input.Name,
		Trigger:   input.Trigger,
		Content:   input.Content,
		Category:  input.Category,
		Pinned:    input.Pinned,
		CharCount: memory.CharCount(input.Content),
	}
	if err := handle.entries.Upsert(ctx, entry); err != nil {
		var tooLarge memory.ErrMemoryTooLarge
		if errors.As(err, &tooLarge) {
			return nil, memoryWriteOutput{}, fmt.Errorf("memory content rejected: limit=%d actual=%d", tooLarge.Limit, tooLarge.Actual)
		}
		return nil, memoryWriteOutput{}, err
	}
	handle.embedder.Enqueue(entry.Name)
	if handle.curator != nil {
		handle.curator.Notify()
	}
	return nil, memoryWriteOutput{Name: entry.Name, Written: true}, nil
}

func (a *toolAdapter) memorySearch(ctx context.Context, _ *mcp.CallToolRequest, input memorySearchInput) (*mcp.CallToolResult, memorySearchOutput, error) {
	limit := 8
	if input.Limit != nil {
		if *input.Limit <= 0 {
			return nil, memorySearchOutput{}, errors.New("search limit must be greater than zero")
		}
		limit = *input.Limit
	}
	handle, release, err := a.registry.Acquire(ctx, input.Namespace)
	if err != nil {
		return nil, memorySearchOutput{}, err
	}
	defer release()
	results, err := handle.retriever.Search(ctx, input.Query, limit)
	if err != nil {
		return nil, memorySearchOutput{}, err
	}
	output := memorySearchOutput{
		Results: make([]searchResultOutput, 0, len(results)),
		Degraded: degradedOutput{
			Semantic: a.registry.embClient == nil,
		},
	}
	if output.Degraded.Semantic {
		output.Degraded.Reason = offlineDegradedReason
	}
	for _, result := range results {
		output.Results = append(output.Results, searchResultOutput{
			Name:      result.Name,
			Trigger:   result.Trigger,
			Snippet:   makeSnippet(result.Content),
			Content:   result.Content,
			Score:     result.Score,
			EventDate: result.EventDate,
			CreatedAt: result.CreatedAt,
		})
	}
	return nil, output, nil
}

func (a *toolAdapter) memoryList(ctx context.Context, _ *mcp.CallToolRequest, input memoryListInput) (*mcp.CallToolResult, memoryListOutput, error) {
	handle, release, err := a.registry.Acquire(ctx, input.Namespace)
	if err != nil {
		return nil, memoryListOutput{}, err
	}
	defer release()
	entries, err := handle.entries.List(ctx)
	if err != nil {
		return nil, memoryListOutput{}, err
	}
	output := memoryListOutput{Entries: make([]entryOutput, 0, len(entries))}
	for _, entry := range entries {
		output.Entries = append(output.Entries, toEntryOutput(entry))
	}
	return nil, output, nil
}

func (a *toolAdapter) memoryGet(ctx context.Context, _ *mcp.CallToolRequest, input memoryGetInput) (*mcp.CallToolResult, memoryGetOutput, error) {
	handle, release, err := a.registry.Acquire(ctx, input.Namespace)
	if err != nil {
		return nil, memoryGetOutput{}, err
	}
	defer release()
	entry, err := handle.entries.GetByName(ctx, input.Name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, memoryGetOutput{}, fmt.Errorf("memory %q: %w", input.Name, store.ErrNotFound)
		}
		return nil, memoryGetOutput{}, err
	}
	return nil, memoryGetOutput{Entry: toEntryOutput(entry)}, nil
}

func (a *toolAdapter) memoryDelete(ctx context.Context, _ *mcp.CallToolRequest, input memoryDeleteInput) (*mcp.CallToolResult, memoryDeleteOutput, error) {
	handle, release, err := a.registry.Acquire(ctx, input.Namespace)
	if err != nil {
		return nil, memoryDeleteOutput{}, err
	}
	defer release()
	err = handle.entries.Delete(ctx, input.Name)
	if errors.Is(err, store.ErrNotFound) {
		return nil, memoryDeleteOutput{Deleted: false}, nil
	}
	if err != nil {
		return nil, memoryDeleteOutput{}, err
	}
	return nil, memoryDeleteOutput{Deleted: true}, nil
}

func (a *toolAdapter) memoryIngest(ctx context.Context, _ *mcp.CallToolRequest, input memoryIngestInput) (*mcp.CallToolResult, memoryIngestOutput, error) {
	handle, release, err := a.registry.Acquire(ctx, input.Namespace)
	if err != nil {
		return nil, memoryIngestOutput{}, err
	}
	defer release()
	if handle.pipe == nil {
		return nil, memoryIngestOutput{}, errors.New("memory_ingest requires an LLM provider")
	}
	messages := make([]pipeline.Message, 0, len(input.Messages))
	for i, message := range input.Messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role != "user" && role != "assistant" {
			return nil, memoryIngestOutput{}, fmt.Errorf("messages[%d].role must be user or assistant", i)
		}
		messages = append(messages, pipeline.Message{Role: role, Text: message.Text})
	}
	before, err := handle.entries.List(ctx)
	if err != nil {
		return nil, memoryIngestOutput{}, err
	}
	count, err := handle.pipe.Ingest(ctx, time.Time{}, "", messages)
	if err != nil {
		return nil, memoryIngestOutput{}, err
	}
	after, err := handle.entries.List(ctx)
	if err != nil {
		return nil, memoryIngestOutput{}, err
	}
	known := make(map[string]struct{}, len(before))
	for _, entry := range before {
		known[entry.Name] = struct{}{}
	}
	entries := make([]ingestEntryOutput, 0, count)
	for _, entry := range after {
		if _, existed := known[entry.Name]; existed {
			continue
		}
		entries = append(entries, ingestEntryOutput{Name: entry.Name, Content: entry.Content})
	}
	return nil, memoryIngestOutput{ExtractedCount: count, Entries: entries}, nil
}

// memoryIngestV2 persists source material even when extraction is unavailable.
// It delegates all ledger, source validation, and projection semantics to the
// engine; this adapter only maps the versioned MCP contract to public types.
func (a *toolAdapter) memoryIngestV2(ctx context.Context, _ *mcp.CallToolRequest, input memoryIngestV2Input) (*mcp.CallToolResult, memoryIngestV2Output, error) {
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		return nil, memoryIngestV2Output{}, errors.New("session_id is required")
	}
	messages := make([]pipeline.Message, 0, len(input.Messages))
	seenSources := make(map[string]struct{}, len(input.Messages))
	for index, message := range input.Messages {
		sourceID := strings.TrimSpace(message.SourceID)
		if sourceID == "" {
			return nil, memoryIngestV2Output{}, fmt.Errorf("messages[%d].source_id is required", index)
		}
		if _, exists := seenSources[sourceID]; exists {
			return nil, memoryIngestV2Output{}, fmt.Errorf("messages[%d].source_id duplicates %q", index, sourceID)
		}
		seenSources[sourceID] = struct{}{}
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role != "user" && role != "assistant" {
			return nil, memoryIngestV2Output{}, fmt.Errorf("messages[%d].role must be user or assistant", index)
		}
		if message.Ordinal != index {
			return nil, memoryIngestV2Output{}, fmt.Errorf("messages[%d].ordinal must equal %d", index, index)
		}
		messages = append(messages, pipeline.Message{
			Role:             role,
			Text:             message.Text,
			ExternalSourceID: sourceID,
			Ordinal:          message.Ordinal,
			OccurredAt:       message.OccurredAt,
		})
	}
	if len(messages) == 0 {
		return nil, memoryIngestV2Output{}, errors.New("messages must contain at least one turn")
	}

	handle, release, err := a.registry.Acquire(ctx, input.Namespace)
	if err != nil {
		return nil, memoryIngestV2Output{}, err
	}
	defer release()
	extract := input.Extract == nil || *input.Extract
	result := pipeline.IngestResult{Degraded: make([]string, 0)}
	if extract && handle.pipe != nil {
		result, err = handle.pipe.IngestDetailed(ctx, time.Time{}, sessionID, messages)
		if err != nil {
			return nil, memoryIngestV2Output{}, err
		}
	} else {
		inputs := make([]memory.EvidenceInput, 0, len(messages))
		recordedAt := time.Now().UTC()
		for _, message := range messages {
			inputs = append(inputs, memory.EvidenceInput{
				ExternalSourceID: message.ExternalSourceID,
				SourceType:       memory.EvidenceMessage,
				SourceSessionID:  sessionID,
				Speaker:          message.Role,
				Ordinal:          message.Ordinal,
				Content:          message.Text,
				OccurredAt:       message.OccurredAt,
				RecordedAt:       recordedAt,
			})
		}
		result.Evidence, err = handle.ledger.AppendBatch(ctx, inputs)
		if err != nil {
			return nil, memoryIngestV2Output{}, err
		}
		if extract {
			result.Degraded = append(result.Degraded, "extraction_unavailable")
		}
	}

	output := memoryIngestV2Output{
		Evidence:       make([]evidenceReceiptOutput, 0, len(result.Evidence)),
		ExtractedCount: len(result.Entries),
		Entries:        make([]ingestEntryOutput, 0, len(result.Entries)),
		Degraded:       result.Degraded,
	}
	for _, evidence := range result.Evidence {
		output.Evidence = append(output.Evidence, evidenceReceiptOutput{
			SourceID: evidence.ExternalSourceID, EvidenceID: evidence.ID, State: string(evidence.State),
		})
	}
	for _, entry := range result.Entries {
		output.Entries = append(output.Entries, ingestEntryOutput{Name: entry.Name, Content: entry.Content})
	}
	return nil, output, nil
}

func (a *toolAdapter) memoryEvidenceGet(ctx context.Context, _ *mcp.CallToolRequest, input memoryEvidenceGetInput) (*mcp.CallToolResult, memoryEvidenceGetOutput, error) {
	if len(input.EvidenceIDs) == 0 {
		return nil, memoryEvidenceGetOutput{}, errors.New("evidence_ids must not be empty")
	}
	handle, release, err := a.registry.Acquire(ctx, input.Namespace)
	if err != nil {
		return nil, memoryEvidenceGetOutput{}, err
	}
	defer release()
	items, err := handle.ledger.GetMany(ctx, input.EvidenceIDs)
	if err != nil {
		return nil, memoryEvidenceGetOutput{}, err
	}
	output := memoryEvidenceGetOutput{Evidence: make([]evidenceOutput, 0, len(input.EvidenceIDs))}
	for _, evidenceID := range input.EvidenceIDs {
		output.Evidence = append(output.Evidence, toEvidenceOutput(items[evidenceID]))
	}
	return nil, output, nil
}

func (a *toolAdapter) memoryEvidenceTombstone(ctx context.Context, _ *mcp.CallToolRequest, input memoryEvidenceLifecycleInput) (*mcp.CallToolResult, memoryEvidenceLifecycleOutput, error) {
	return a.memoryEvidenceTransition(ctx, input, "tombstone")
}

func (a *toolAdapter) memoryEvidenceRestore(ctx context.Context, _ *mcp.CallToolRequest, input memoryEvidenceLifecycleInput) (*mcp.CallToolResult, memoryEvidenceLifecycleOutput, error) {
	return a.memoryEvidenceTransition(ctx, input, "restore")
}

func (a *toolAdapter) memoryEvidenceTransition(ctx context.Context, input memoryEvidenceLifecycleInput, action string) (*mcp.CallToolResult, memoryEvidenceLifecycleOutput, error) {
	if strings.TrimSpace(input.EvidenceID) == "" {
		return nil, memoryEvidenceLifecycleOutput{}, errors.New("evidence_id is required")
	}
	if strings.TrimSpace(input.ReasonCode) == "" {
		return nil, memoryEvidenceLifecycleOutput{}, errors.New("reason_code is required")
	}
	handle, release, err := a.registry.Acquire(ctx, input.Namespace)
	if err != nil {
		return nil, memoryEvidenceLifecycleOutput{}, err
	}
	defer release()
	req := memory.LifecycleRequest{EvidenceID: input.EvidenceID, RequestID: input.RequestID, ReasonCode: input.ReasonCode}
	if action == "tombstone" {
		err = handle.ledger.Tombstone(ctx, req)
	} else {
		err = handle.ledger.Restore(ctx, req)
	}
	if err != nil {
		return nil, memoryEvidenceLifecycleOutput{}, err
	}
	state := string(memory.EvidenceTombstoned)
	if action == "restore" {
		state = string(memory.EvidenceActive)
	}
	return nil, memoryEvidenceLifecycleOutput{EvidenceID: input.EvidenceID, State: state}, nil
}

func (a *toolAdapter) memoryEvidencePurge(ctx context.Context, _ *mcp.CallToolRequest, input memoryEvidenceLifecycleInput) (*mcp.CallToolResult, memoryEvidenceLifecycleOutput, error) {
	if strings.TrimSpace(input.EvidenceID) == "" {
		return nil, memoryEvidenceLifecycleOutput{}, errors.New("evidence_id is required")
	}
	if strings.TrimSpace(input.ReasonCode) == "" {
		return nil, memoryEvidenceLifecycleOutput{}, errors.New("reason_code is required")
	}
	handle, release, err := a.registry.Acquire(ctx, input.Namespace)
	if err != nil {
		return nil, memoryEvidenceLifecycleOutput{}, err
	}
	defer release()
	result, err := handle.ledger.Purge(ctx, memory.LifecycleRequest{
		EvidenceID: input.EvidenceID, RequestID: input.RequestID, ReasonCode: input.ReasonCode,
	})
	if err != nil {
		if errors.Is(err, memory.ErrPurgeIncomplete) {
			return nil, memoryEvidenceLifecycleOutput{}, errors.New("evidence purge checkpoint incomplete; retry the same request")
		}
		return nil, memoryEvidenceLifecycleOutput{}, err
	}
	return nil, memoryEvidenceLifecycleOutput{
		EvidenceID: result.EvidenceID, State: string(memory.EvidencePurged), CheckpointPending: result.CheckpointPending,
	}, nil
}

func toEvidenceOutput(evidence memory.Evidence) evidenceOutput {
	return evidenceOutput{
		EvidenceID: evidence.ID,
		SourceID:   evidence.ExternalSourceID,
		SessionID:  evidence.SourceSessionID,
		Role:       evidence.Speaker,
		Ordinal:    evidence.Ordinal,
		Content:    evidence.Content,
		OccurredAt: evidence.OccurredAt,
		RecordedAt: evidence.RecordedAt,
		State:      string(evidence.State),
	}
}

func toEntryOutput(entry *memory.Entry) entryOutput {
	return entryOutput{
		ID:              entry.ID,
		Name:            entry.Name,
		Trigger:         entry.Trigger,
		Content:         entry.Content,
		Pinned:          entry.Pinned,
		Durability:      entry.Durability,
		Category:        entry.Category,
		HitCount:        entry.HitCount,
		LastUsedAt:      entry.LastUsedAt,
		CreatedAt:       entry.CreatedAt,
		UpdatedAt:       entry.UpdatedAt,
		CharCount:       entry.CharCount,
		SourceSessionID: entry.SourceSessionID,
		EventDate:       entry.EventDate,
		EventStart:      entry.EventStart,
		EventEnd:        entry.EventEnd,
		FactSource:      entry.FactSource,
	}
}

func makeSnippet(content string) string {
	const maxRunes = 200
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	return string(runes[:maxRunes]) + "..."
}
