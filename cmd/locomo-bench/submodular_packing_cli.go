package main

// 045 US1: --aic-gate offline packing-fidelity gate. "Offline" means zero
// LLM/answerer/judge calls; the ONLY model-side dependency is the hybrid
// retriever's query-embedding sidecar (same one the anchor recipe uses —
// plan F4). The gate is probed fail-closed: if the embedding sidecar is not
// reachable, the gate errors out instead of silently scoring a BM25+entity
// pool that the anchor recipe never used.
//
// Gate rule (contracts/cli-flags.md v1, frozen):
//   packed.aic >= 0.95 * top150_full.aic  AND  packed.tokens_mean <= anchor
// where top150_full = top-k 150 with the anchor chunk-quota semantics,
// rendered by the CURRENT assembly (no packing), and the per-question
// packing budget is the current-k30 arm's rendered token estimate.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	aicGateTopK       = 30
	aicGateQuota      = 12
	aicGateTop150K    = 150
	aicGateThreshold  = 0.95
	aicGatePoolFactor = 6
	aicGatePoolMin    = 300
)

type aicGateManifest struct {
	Feature            string    `json:"feature"`
	GeneratedAt        string    `json:"generated_at"`
	Slice              string    `json:"slice"`
	QuestionCount      int       `json:"question_count"`
	Recipe             aicRecipe `json:"recipe"`
	CurrentK30         aicArm    `json:"current_k30"`
	Packed             aicArm    `json:"packed"`
	Top150Full         aicArm    `json:"top150_full"`
	SingletonFallbacks int       `json:"singleton_fallbacks"`
	UnmatchableInPool  int       `json:"unmatchable_in_pool"`
	Gate               aicGate   `json:"gate"`
	Digest             aicDigest `json:"digest"`
}

type aicRecipe struct {
	TopK          int     `json:"top_k"`
	ChunkQuota    int     `json:"chunk_quota"`
	PoolSize      int     `json:"pool"`
	Weights       string  `json:"weights"`
	Scaffold      bool    `json:"scaffold"`
	Normalization string  `json:"normalization"`
}

type aicGate struct {
	Rule    string `json:"rule"`
	Verdict string `json:"verdict"` // GO | NO-GO
}

type aicDigest struct {
	GateFileSHA256  string `json:"packing_gate_sha256"`
	AuditFileSHA256 string `json:"packing_audit_sha256"`
}

type aicAuditRow struct {
	QuestionID     string   `json:"question_id"`
	Gold           []string `json:"gold"`
	Budget         int      `json:"budget"`
	PackedUsed     int      `json:"packed_used"`
	Singleton      bool     `json:"singleton"`
	SelectedCount  int      `json:"selected_count"`
	PoolSize       int      `json:"pool_size"`
	AICCurrent     bool     `json:"aic_current_k30"`
	AICPacked      bool     `json:"aic_packed"`
	AICTop150      bool     `json:"aic_top150_full"`
	Unmatchable    bool     `json:"unmatchable_in_pool,omitempty"`
	Error          string   `json:"error,omitempty"`
}

// parseAicGateSlice parses "0,1" into conversation IDs (empty = all).
func parseAicGateSlice(spec string) (map[int]bool, bool, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, false, nil
	}
	out := make(map[int]bool)
	for _, p := range strings.Split(spec, ",") {
		id, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || id < 0 {
			return nil, false, fmt.Errorf("--aic-gate-slice wants comma-separated conversation ids, got %q", spec)
		}
		out[id] = true
	}
	return out, true, nil
}

// runAicGateCLI executes the US1 gate and exits (flag-triggered mode).
func runAicGateCLI(ctx context.Context, opt options, convs []conversation, arms []string, logger *slog.Logger) error {
	if opt.storeDir == "" {
		return fmt.Errorf("--aic-gate requires --store-dir with persisted conversation stores (032-store)")
	}
	if opt.aicGateDir == "" {
		return fmt.Errorf("--aic-gate requires a path value for its output directory")
	}
	sliceFilter, hasSlice, err := parseAicGateSlice(opt.aicGateSlice)
	if err != nil {
		return err
	}
	selected := convs
	if hasSlice {
		selected = nil
		for _, conv := range convs {
			if sliceFilter[conv.ID] {
				selected = append(selected, conv)
			}
		}
		if len(selected) == 0 {
			return fmt.Errorf("--aic-gate-slice %q matched no conversations", opt.aicGateSlice)
		}
	}
	if err := os.MkdirAll(opt.aicGateDir, 0o755); err != nil {
		return fmt.Errorf("create aic gate dir: %w", err)
	}

	arm := arms[0]
	if armBackend(arm) == "hybrid" {
		client := buildBenchEmbeddingClient(logger, nil)
		if client == nil {
			return fmt.Errorf("aic-gate: embedding client unavailable — run where the hybrid sidecar (EMBED_BASE_URL) lives; see plan F4 (fail-closed, no silent BM25-only pool)")
		}
		if _, err := client.Embed(ctx, []string{"aic-gate reachability probe"}); err != nil {
			return fmt.Errorf("aic-gate: embedding sidecar unreachable: %w — run where EMBED_BASE_URL points (plan F4)", err)
		}
	}

	poolSize := aicGateTopK * aicGatePoolFactor
	if poolSize < aicGatePoolMin {
		poolSize = aicGatePoolMin
	}

	var auditRows []aicAuditRow
	var curRows, packedRows, topRows []aicRow
	curTokens := map[string]float64{}
	packedTokens := map[string]float64{}
	topTokens := map[string]float64{}
	singletons := 0
	unmatchable := 0

	for _, conv := range selected {
		runtime, err := openAicGateRuntime(ctx, opt, conv, arm, logger)
		if err != nil {
			return err
		}
		err = func() error {
			defer runtime.Close()
			retriever := runtime.retrievers[arm]
			if retriever == nil {
				return fmt.Errorf("aic-gate: no retriever for arm %q", arm)
			}
			for qi, qa := range conv.QA {
				row := aicAuditRow{
					QuestionID: fmt.Sprintf("c%dq%d", conv.ID, qi),
					Gold:       []string{qa.AnswerText()},
				}
				auditRows = append(auditRows, row)
				cur := &auditRows[len(auditRows)-1]

				aliases := row.Gold
				curRow := aicRow{QuestionID: row.QuestionID, GoldAliases: aliases}
				packedRow := curRow
				topRow := curRow

				// One shared wide pool for current-k30 and packed (the e2e
				// mechanism replaces quota truncation on THIS pool); a wider
				// pool for the top150 reference arm.
				wide, _, err := retriever.SearchWithDiagnostics(ctx, qa.Question, poolSize)
				if err != nil {
					cur.Error = fmt.Sprintf("wide search: %v", err)
					curRows = append(curRows, curRow)
					packedRows = append(packedRows, packedRow)
					topRows = append(topRows, topRow)
					continue
				}
				cur.PoolSize = len(wide)

				current := applyChunkQuota(wide, aicGateTopK, aicGateQuota)
				currentPrompt := buildAnswerContextPrompt(qa.Question, current, qa.QuestionDate, qa.Category, false)
				currentTokensEst := packEstimateTokens(currentPrompt)
				if m, ok := aicMatch(currentPrompt, aliases); ok {
					curRow.MatchedAlias, curRow.InContext = m, true
				}
				cur.AICCurrent = curRow.InContext

				sel := packSelect(wide, qa.Question, currentTokensEst, defaultPackWeights())
				if sel.SingletonFallback {
					singletons++
					cur.Singleton = true
				}
				packedPrompt := buildAnswerContextPrompt(qa.Question, sel.Selected, qa.QuestionDate, qa.Category, false)
				if m, ok := aicMatch(packedPrompt, aliases); ok {
					packedRow.MatchedAlias, packedRow.InContext = m, true
				}
				cur.Budget = currentTokensEst
				cur.PackedUsed = sel.EstTokensUsed
				cur.SelectedCount = len(sel.Selected)
				cur.AICPacked = packedRow.InContext

				wide150, _, err := retriever.SearchWithDiagnostics(ctx, qa.Question, aicGateTop150K*aicGatePoolFactor)
				if err != nil {
					cur.Error = fmt.Sprintf("top150 search: %v", err)
				} else {
					top150 := applyChunkQuota(wide150, aicGateTop150K, aicGateQuota)
					topPrompt := buildAnswerContextPrompt(qa.Question, top150, qa.QuestionDate, qa.Category, false)
					if m, ok := aicMatch(topPrompt, aliases); ok {
						topRow.MatchedAlias, topRow.InContext = m, true
					}
					topTokens[row.QuestionID] = float64(packEstimateTokens(topPrompt))
					cur.AICTop150 = topRow.InContext
					// Unmatchable-in-pool audit over the WIDEST pool we saw.
					found := false
					for _, h := range wide150 {
						if _, ok := aicMatch(h.Content, aliases); ok {
							found = true
							break
						}
					}
					if !found {
						unmatchable++
						curRow.UnmatchableInPool = true
						cur.Unmatchable = true
					}
				}

				curTokens[row.QuestionID] = float64(currentTokensEst)
				packedTokens[row.QuestionID] = float64(sel.EstTokensUsed)
				curRows = append(curRows, curRow)
				packedRows = append(packedRows, packedRow)
				topRows = append(topRows, topRow)
			}
			return nil
		}()
		if err != nil {
			return err
		}
	}

	curArm := aicArmFrom(curRows, func(id string) float64 { return curTokens[id] })
	packedArm := aicArmFrom(packedRows, func(id string) float64 { return packedTokens[id] })
	topArm := aicArmFrom(topRows, func(id string) float64 { return topTokens[id] })

	verdict := "NO-GO"
	pass := packedArm.AIC >= aicGateThreshold*topArm.AIC && packedArm.TokensMean <= curArm.TokensMean
	if pass {
		verdict = "GO"
	}

	manifest := aicGateManifest{
		Feature:       "045-submodular-packing US1 aic-gate",
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Slice:         opt.aicGateSlice,
		QuestionCount: len(auditRows),
		Recipe: aicRecipe{
			TopK: aicGateTopK, ChunkQuota: aicGateQuota, PoolSize: poolSize,
			Weights: "3:1:1:1", Scaffold: false,
			Normalization: "lower+collapse-ws+substring (frozen 2026-08-16)",
		},
		CurrentK30:         curArm,
		Packed:             packedArm,
		Top150Full:         topArm,
		SingletonFallbacks: singletons,
		UnmatchableInPool:  unmatchable,
		Gate: aicGate{
			Rule:    "packed.aic >= 0.95 * top150_full.aic AND packed.tokens_mean <= current_k30.tokens_mean",
			Verdict: verdict,
		},
	}

	// Freeze-before-digest discipline: every manifest field above is final;
// only now do we write the artifacts and compute the digest over them.
	auditPath := filepath.Join(opt.aicGateDir, "packing_audit.jsonl")
	if err := writeAicAudit(auditPath, auditRows); err != nil {
		return err
	}
	gatePath := filepath.Join(opt.aicGateDir, "packing_gate.json")
	gateBlob, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	gateBlob = append(gateBlob, '\n')
	if err := os.WriteFile(gatePath, gateBlob, 0o644); err != nil {
		return fmt.Errorf("write packing_gate.json: %w", err)
	}
	auditSum := sha256File(auditPath)
	body := manifest // Digest still zero — this is the digested body
	bodyBlob, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return err
	}
	gateSum := sha256.Sum256(bodyBlob)
	manifest.Digest = aicDigest{
		GateFileSHA256:  hex.EncodeToString(gateSum[:]),
		AuditFileSHA256: auditSum,
	}
	gateBlob, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	gateBlob = append(gateBlob, '\n')
	if err := os.WriteFile(gatePath, gateBlob, 0o644); err != nil {
		return fmt.Errorf("rewrite packing_gate.json with digest: %w", err)
	}

	logger.Info("aic gate complete",
		"questions", manifest.QuestionCount,
		"current_aic", curArm.AIC, "packed_aic", packedArm.AIC, "top150_aic", topArm.AIC,
		"current_tokens", curArm.TokensMean, "packed_tokens", packedArm.TokensMean,
		"singletons", singletons, "unmatchable", unmatchable,
		"verdict", verdict)
	fmt.Printf("aic gate verdict: %s (packed %.4f vs top150 %.4f; tokens %.0f vs %.0f)\n",
		verdict, packedArm.AIC, topArm.AIC, packedArm.TokensMean, curArm.TokensMean)
	return nil
}

// openAicGateRuntime opens one conversation store + its retriever (same
// construction the abstain probe uses — the runtime opening is shared, the
// scoring is not).
func openAicGateRuntime(ctx context.Context, opt options, conv conversation, arm string, logger *slog.Logger) (*conversationRuntime, error) {
	embClient := buildBenchEmbeddingClient(logger, nil)
	arms := []string{arm}
	return openAbstainProbeRuntime(ctx, opt, conv, embClient, arms)
}

func writeAicAudit(path string, rows []aicAuditRow) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create packing_audit.jsonl: %w", err)
	}
	defer f.Close() //nolint:errcheck
	enc := json.NewEncoder(f)
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.QuestionID
	}
	sort.Strings(ids)
	byID := make(map[string]int, len(rows))
	for i, r := range rows {
		byID[r.QuestionID] = i
	}
	for _, id := range ids {
		if err := enc.Encode(rows[byID[id]]); err != nil {
			return fmt.Errorf("encode audit row: %w", err)
		}
	}
	return nil
}

func sha256File(path string) string {
	h := sha256.New()
	f, err := os.Open(path) //nolint:gosec // run-dir artifact
	if err != nil {
		return ""
	}
	defer f.Close() //nolint:errcheck
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
