// Command planner-build is the Go side of the 023 synthetic-data pipeline
// (specs/023 data-model.md §3). It consumes the convos.jsonl emitted by
// training/planner/data_build.py — fictional multi-session memory dialogs with
// per-turn stable ids and annotated recall queries — ingests each conversation
// into engram offline (extraction + indexing), retrieves the frozen candidate
// set per query, resolves the gold source turns to Ledger Evidence, and emits
// one candidate line per query. label.py turns that into training samples.
//
// It reuses only the engine's public API (store / memory / pipeline / embedding
// / provider); the engine itself is untouched.
//
// Usage:
//
//	planner-build -convos data/raw/convos.jsonl \
//	    -out data/processed/candidates.jsonl \
//	    -build-version 023-b20260803-r1 \
//	    -extract-base-url http://localhost:8000/v1 -extract-model Qwen2.5-7B-Instruct \
//	    [-embed-base-url http://localhost:8010/v1 -embed-model bge-m3] \
//	    [-store-dir /root/autodl-tmp/023-runs/stores] [-top-k 30]
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/pipeline"
	"github.com/wallfacers/engram/provider"
	"github.com/wallfacers/engram/provider/openai"
	"github.com/wallfacers/engram/store"
)

// ---------------------------------------------------------------------------
// convos.jsonl input shapes (data_build.py)

type convoRecord struct {
	ConversationID string       `json:"conversation_id"`
	Persona        string       `json:"persona"`
	Sessions       []sessionRec `json:"sessions"`
	Queries        []queryRec   `json:"queries"`
}

type sessionRec struct {
	SessionID string    `json:"session_id"`
	Date      string    `json:"date"`
	Turns     []turnRec `json:"turns"`
}

type turnRec struct {
	TurnID  string `json:"turn_id"`
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
}

type queryRec struct {
	QuestionID        string   `json:"question_id"`
	Query             string   `json:"query"`
	Type              string   `json:"type"`
	GoldAnswer        string   `json:"gold_answer"`
	GoldSourceTurnIDs []string `json:"gold_source_turn_ids"`
}

// ---------------------------------------------------------------------------
// candidates.jsonl output shapes (data-model.md §1)

type candidateLine struct {
	ID                string               `json:"id"`
	ConversationID    string               `json:"conversation_id"`
	Query             string               `json:"query"`
	QueryDate         string               `json:"query_date"`
	Category          string               `json:"category"`
	QueryDigest       string               `json:"query_digest"`
	GoldAnswer        string               `json:"gold_answer"`
	GoldSourceTurnIDs []string             `json:"gold_source_turn_ids"`
	Candidates        []candidateRec       `json:"candidates"`
	Sources           map[string]sourceRec `json:"sources"`
	GoldCoverage      goldCoverage         `json:"gold_coverage"`
	BuildVersion      string               `json:"build_version"`
}

type candidateRec struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Rank       int      `json:"rank"`
	Score      float64  `json:"score"`
	Text       string   `json:"text"`
	TextDigest string   `json:"text_digest"`
	SourceIDs  []string `json:"source_ids"`
}

type sourceRec struct {
	SessionID     string `json:"session_id"`
	Ordinal       int    `json:"ordinal"`
	ContentDigest string `json:"content_digest"`
	OccurredAt    string `json:"occurred_at"`
}

type goldCoverage struct {
	GoldSourceEvidenceIDs  []string `json:"gold_source_evidence_ids"`
	CandidateEvidenceUnion []string `json:"candidate_evidence_union"`
	CoveredSourceCount     int      `json:"covered_source_count"`
	CandidateCovered       bool     `json:"candidate_covered"`
}

// categoryFromType maps the data_build.py query type onto the residual-cohort
// category vocabulary so 023 training distribution can be compared to the
// LoCoMo residual classes (temporal / single-hop / multi-hop / open-domain).
func categoryFromType(qt string) string {
	switch qt {
	case "direct":
		return "single-hop"
	case "time":
		return "temporal"
	case "multi_hop":
		return "multi-hop"
	case "update":
		return "open-domain"
	default:
		return qt
	}
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// model caller (extraction sidecar)

func buildModelCaller(baseURL, model, apiKey string) (pipeline.ModelCaller, error) {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("extraction base-url and model are required")
	}
	prov := openai.New(openai.Options{APIKey: apiKey, BaseURL: baseURL, IncludeUsage: true})
	return func(ctx context.Context, system, user string) (string, error) {
		req := provider.Request{
			Model:     model,
			System:    system,
			MaxTokens: 2048,
			Messages: []provider.Message{{
				Role:    provider.RoleUser,
				Content: []provider.ContentBlock{{Type: provider.BlockText, Text: user}},
			}},
		}
		ch, err := prov.Stream(ctx, req)
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		for ev := range ch {
			switch ev.Type {
			case provider.EventTextDelta:
				sb.WriteString(ev.TextDelta)
			case provider.EventError:
				if ev.Error != nil {
					return "", ev.Error
				}
			}
		}
		return sb.String(), nil
	}, nil
}

// ---------------------------------------------------------------------------
// parsing

func readConvos(path string) ([]convoRecord, error) {
	f, err := os.Open(path) //nolint:gosec // operator-supplied dataset path
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var convos []convoRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var c convoRecord
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("parse convo line: %w", err)
		}
		if c.ConversationID == "" || len(c.Queries) == 0 {
			continue // conversations without annotated queries are not training material
		}
		convos = append(convos, c)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return convos, nil
}

// sessionMessages maps a synthetic session's turns onto pipeline Messages,
// keeping the stable turn_id as the Evidence ExternalSourceID so gold source
// turns can be traced back to Ledger Evidence (data-model.md §3).
func sessionMessages(s sessionRec) []pipeline.Message {
	out := make([]pipeline.Message, 0, len(s.Turns))
	date, _ := time.Parse("2006-01-02", s.Date)
	for i, t := range s.Turns {
		if strings.TrimSpace(t.Text) == "" {
			continue
		}
		out = append(out, pipeline.Message{
			Role:             t.Speaker,
			Text:             t.Text,
			ExternalSourceID: t.TurnID,
			Ordinal:          i,
			OccurredAt:       &date,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// store building

func buildStore(ctx context.Context, conv convoRecord, extractCall pipeline.ModelCaller, embClient embedding.Client, storeDir string, ci int) (*store.Store, *memory.EntryStore, *memory.VectorStore, error) {
	dsn := ":memory:"
	if storeDir != "" {
		if err := os.MkdirAll(storeDir, 0o755); err != nil {
			return nil, nil, nil, err
		}
		dsn = filepath.Join(storeDir, fmt.Sprintf("conv%d.db", ci))
	}
	st, err := store.Open(ctx, store.Options{DSN: dsn})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open store: %w", err)
	}

	es := memory.NewEntryStore(st.DB())
	vectors := memory.NewVectorStore(st.DB())
	embedder := memory.NewEmbedder(es, vectors, embClient, memory.DefaultEmbedBuffer)

	pipe := pipeline.New(pipeline.Config{
		Entries:  es,
		Embedder: embedder,
		Call:     extractCall,
		Budgets:  memory.DefaultBudgets(),
	})
	if pipe == nil {
		st.Close()
		return nil, nil, nil, fmt.Errorf("pipeline inert: nil extraction caller")
	}
	for _, s := range conv.Sessions {
		date, _ := time.Parse("2006-01-02", s.Date)
		if _, err := pipe.Ingest(ctx, date, s.SessionID, sessionMessages(s)); err != nil {
			st.Close()
			return nil, nil, nil, fmt.Errorf("ingest session %s: %w", s.SessionID, err)
		}
	}
	if embedder != nil {
		if err := embedder.Backfill(ctx); err != nil {
			// Embedding is a graceful-degradation signal; a failed backfill is
			// not fatal to keyword retrieval, but surface it honestly.
			fmt.Fprintf(os.Stderr, "planner-build: embedder backfill: %v\n", err)
		}
		embedder.Close()
	}
	return st, es, vectors, nil
}

// lastSessionDate returns the latest session date as the approximate "now" the
// synthetic queries are asked against, in YYYY-MM-DD. Empty when unparseable.
func lastSessionDate(conv convoRecord) string {
	var latest time.Time
	for _, s := range conv.Sessions {
		if t, err := time.Parse("2006-01-02", s.Date); err == nil {
			if t.After(latest) {
				latest = t
			}
		}
	}
	if latest.IsZero() {
		return ""
	}
	return latest.Format("2006-01-02")
}

// ---------------------------------------------------------------------------
// candidate + coverage

// buildCandidateLine retrieves the frozen top-K candidates for one query,
// resolves gold source turns to Ledger Evidence, and reports oracle coverage.
func buildCandidateLine(ctx context.Context, es *memory.EntryStore, vectors *memory.VectorStore, conv convoRecord, q queryRec, qi int, queryDate string, topK int, buildVersion string) (candidateLine, error) {
	now, _ := time.Parse("2006-01-02", queryDate)
	retriever := memory.NewRetriever(es, vectors, nil)
	if !now.IsZero() {
		retriever = memory.NewRetrieverWithOptions(es, vectors, nil, nil, memory.RetrieverOptions{Now: now})
	}
	results, err := retriever.Search(ctx, q.Query, topK)
	if err != nil {
		return candidateLine{}, fmt.Errorf("search %s: %w", q.QuestionID, err)
	}

	// Resolve gold source turns → Ledger Evidence, and index every Evidence for
	// the sources map (data-model.md §1 lineage).
	allEv, err := es.Ledger().ListActiveEvidence(ctx)
	if err != nil {
		return candidateLine{}, fmt.Errorf("list evidence: %w", err)
	}
	turnToEvID := make(map[string]string, len(allEv))
	evByID := make(map[string]memory.Evidence, len(allEv))
	for _, e := range allEv {
		if e.ExternalSourceID != "" {
			turnToEvID[e.ExternalSourceID] = e.ID
		}
		evByID[e.ID] = e
	}
	goldEvidence := make([]string, 0, len(q.GoldSourceTurnIDs))
	for _, tid := range q.GoldSourceTurnIDs {
		if id, ok := turnToEvID[tid]; ok {
			goldEvidence = append(goldEvidence, id)
		}
	}
	sort.Strings(goldEvidence)

	candidates := make([]candidateRec, 0, len(results))
	allCandEv := make(map[string]struct{})
	for rank, r := range results {
		refs, err := es.SourceRefs(ctx, r.ID)
		if err != nil {
			continue
		}
		srcIDs := make([]string, 0, len(refs))
		for _, ref := range refs {
			srcIDs = append(srcIDs, ref.EvidenceID)
			allCandEv[ref.EvidenceID] = struct{}{}
		}
		sort.Strings(srcIDs)
		candidates = append(candidates, candidateRec{
			ID:         r.ID,
			Kind:       "atomic_fact",
			Rank:       rank,
			Score:      r.Score,
			Text:       r.Content,
			TextDigest: sha256hex(r.Content),
			SourceIDs:  srcIDs,
		})
	}

	candEvUnion := make([]string, 0, len(allCandEv))
	for id := range allCandEv {
		candEvUnion = append(candEvUnion, id)
	}
	sort.Strings(candEvUnion)
	covered := 0
	for _, id := range goldEvidence {
		if _, ok := allCandEv[id]; ok {
			covered++
		}
	}

	sources := make(map[string]sourceRec, len(evByID))
	addSource := func(id string) {
		e, ok := evByID[id]
		if !ok {
			return
		}
		occurred := ""
		if e.OccurredAt != nil {
			occurred = e.OccurredAt.UTC().Format(time.RFC3339)
		}
		sources[id] = sourceRec{
			SessionID:     e.SourceSessionID,
			Ordinal:       e.Ordinal,
			ContentDigest: e.ContentDigest,
			OccurredAt:    occurred,
		}
	}
	for _, id := range candEvUnion {
		addSource(id)
	}
	for _, id := range goldEvidence {
		addSource(id)
	}

	line := candidateLine{
		ID:                fmt.Sprintf("%s-%s", conv.ConversationID, q.QuestionID),
		ConversationID:    conv.ConversationID,
		Query:             q.Query,
		QueryDate:         queryDate,
		Category:          categoryFromType(q.Type),
		QueryDigest:       sha256hex(q.Query),
		GoldAnswer:        q.GoldAnswer,
		GoldSourceTurnIDs: q.GoldSourceTurnIDs,
		Candidates:        candidates,
		Sources:           sources,
		GoldCoverage: goldCoverage{
			GoldSourceEvidenceIDs:  goldEvidence,
			CandidateEvidenceUnion: candEvUnion,
			CoveredSourceCount:     covered,
			CandidateCovered:       covered > 0 && len(goldEvidence) > 0,
		},
		BuildVersion: buildVersion,
	}
	return line, nil
}

// ---------------------------------------------------------------------------
// main

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "planner-build:", err)
		os.Exit(1)
	}
}

func run() error {
	convoFile := flag.String("convos", "", "input convos.jsonl (data_build.py output; required)")
	outFile := flag.String("out", "data/processed/candidates.jsonl", "output candidates.jsonl")
	storeDir := flag.String("store-dir", "", "data-disk store dir; empty = in-memory per conversation")
	buildVersion := flag.String("build-version", "", "frozen build version (FR-015; required)")
	topK := flag.Int("top-k", 30, "frozen candidates per query")
	extractBase := flag.String("extract-base-url", os.Getenv("EXTRACT_BASE_URL"), "extraction LLM base URL (OpenAI-compatible)")
	extractModel := flag.String("extract-model", os.Getenv("EXTRACT_MODEL"), "extraction model")
	extractAPIKey := flag.String("extract-api-key", os.Getenv("EXTRACT_API_KEY"), "extraction API key")
	embedBase := flag.String("embed-base-url", os.Getenv("EMBED_BASE_URL"), "embedding base URL (optional; empty = keyword-only)")
	embedModel := flag.String("embed-model", os.Getenv("EMBED_MODEL"), "embedding model")
	embedAPIKey := flag.String("embed-api-key", os.Getenv("EMBED_API_KEY"), "embedding API key")
	flag.Parse()

	if *convoFile == "" {
		return fmt.Errorf("-convos is required")
	}
	if strings.TrimSpace(*buildVersion) == "" {
		return fmt.Errorf("-build-version is required (frozen FR-015)")
	}

	extractCall, err := buildModelCaller(*extractBase, *extractModel, *extractAPIKey)
	if err != nil {
		return err
	}
	var embClient embedding.Client
	if *embedBase != "" {
		hc, cerr := embedding.New(embedding.Config{BaseURL: *embedBase, Model: *embedModel, APIKey: *embedAPIKey})
		if cerr != nil {
			return fmt.Errorf("embedding client: %w", cerr)
		}
		if hc != nil {
			embClient = hc
		}
	}

	convos, err := readConvos(*convoFile)
	if err != nil {
		return err
	}
	if len(convos) == 0 {
		return fmt.Errorf("no conversations with queries found in %s", *convoFile)
	}

	if *outFile != "" {
		if dir := filepath.Dir(*outFile); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
	}
	out, err := os.Create(*outFile)
	if err != nil {
		return err
	}
	defer out.Close()
	w := bufio.NewWriter(out)
	defer w.Flush()

	total, covered := 0, 0
	for ci, conv := range convos {
		ctx := context.Background()
		st, es, vectors, err := buildStore(ctx, conv, extractCall, embClient, *storeDir, ci)
		if err != nil {
			return fmt.Errorf("conversation %s: %w", conv.ConversationID, err)
		}
		queryDate := lastSessionDate(conv)
		for qi, q := range conv.Queries {
			line, err := buildCandidateLine(ctx, es, vectors, conv, q, qi, queryDate, *topK, *buildVersion)
			if err != nil {
				st.Close()
				return fmt.Errorf("conversation %s query %s: %w", conv.ConversationID, q.QuestionID, err)
			}
			if err := json.NewEncoder(w).Encode(line); err != nil {
				st.Close()
				return err
			}
			total++
			if line.GoldCoverage.CandidateCovered {
				covered++
			}
		}
		if err := st.Close(); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "planner-build: wrote %d candidate lines (gold covered %d) to %s; conversations=%d\n",
		total, covered, *outFile, len(convos))
	return nil
}
