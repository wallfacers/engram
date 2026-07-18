package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wallfacers/engram/memory"
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
	handle, err := a.registry.Get(ctx, input.Namespace)
	if err != nil {
		return nil, memoryWriteOutput{}, err
	}
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
	handle, err := a.registry.Get(ctx, input.Namespace)
	if err != nil {
		return nil, memorySearchOutput{}, err
	}
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
	handle, err := a.registry.Get(ctx, input.Namespace)
	if err != nil {
		return nil, memoryListOutput{}, err
	}
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
	handle, err := a.registry.Get(ctx, input.Namespace)
	if err != nil {
		return nil, memoryGetOutput{}, err
	}
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
	handle, err := a.registry.Get(ctx, input.Namespace)
	if err != nil {
		return nil, memoryDeleteOutput{}, err
	}
	err = handle.entries.Delete(ctx, input.Name)
	if errors.Is(err, store.ErrNotFound) {
		return nil, memoryDeleteOutput{Deleted: false}, nil
	}
	if err != nil {
		return nil, memoryDeleteOutput{}, err
	}
	return nil, memoryDeleteOutput{Deleted: true}, nil
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
