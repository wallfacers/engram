package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wallfacers/engram/memory/evidencecompiler"
)

// temporal_resolution.go — 027: 查询时时间有效性解析 (确定性, 纯 Go, 零 LLM)。
//
// 在固定候选池内对已命中证据做时间组织: 当前值解析 / 演化链组装 / 时间窗约束。
// 与 013/014/017 的检索侧重排/过滤 (temporalScore/temporalHardFilter) 和 024 的写侧
// supersede 惩罚 (conflictResolution/supersededPenalty) 不同, 027 是查侧 bundle
// 组装阶段对已命中候选的时间组织——不改变候选选择与检索打分 (候选逐字节一致),
// 只改变候选进入 answer bundle 的组织方式。

// ResolutionMode 解析模式。
type ResolutionMode string

const (
	// ResolutionDegraded 不适用: 无时间语义/无显式实体 → 退化基线行为。
	ResolutionDegraded ResolutionMode = "degraded"
	// ResolutionCurrentValue 知识更新/当前状态语义: 选最新 valid 版本。
	ResolutionCurrentValue ResolutionMode = "current_value"
	// ResolutionEvolutionChain 演化/时序语义: 按 OccurredAt 全序组装演化链。
	ResolutionEvolutionChain ResolutionMode = "evolution_chain"
	// ResolutionTemporalWindow 显式时间范围: 仅保留 OccurredAt 覆盖该范围的版本。
	ResolutionTemporalWindow ResolutionMode = "temporal_window"
)

// ResolutionAudit 逐题解析审计 (spec FR-008)。字段与 data-model.md 对齐。
type ResolutionAudit struct {
	Mode               string `json:"mode"`
	GroupCount         int    `json:"group_count"`
	VersionsConsidered int    `json:"versions_considered"`
	SupersededExcluded int    `json:"superseded_excluded"`
	WindowExcluded     int    `json:"window_excluded"`
	UnresolvedTime     int    `json:"unresolved_time"`
}

// classifyQueryMode 确定性分类 query 的解析语义 (resolve H1):
//   - 含显式时间范围 (年份/日期)  → temporal_window
//   - 含演化/变化语义关键词       → evolution_chain
//   - 含当前/最新语义关键词       → current_value
//   - 否则                        → degraded
//
// 关键词表覆盖 LoCoMo / LongMemEval-S 的英文 query 常见措辞。
func classifyQueryMode(query string) ResolutionMode {
	q := strings.ToLower(strings.TrimSpace(query))
	if hasExplicitTimeWindow(q) {
		return ResolutionTemporalWindow
	}
	for _, kw := range evolutionKeywords {
		if strings.Contains(q, kw) {
			return ResolutionEvolutionChain
		}
	}
	for _, kw := range currentKeywords {
		if strings.Contains(q, kw) {
			return ResolutionCurrentValue
		}
	}
	return ResolutionDegraded
}

var evolutionKeywords = []string{
	"before", "previously", "used to", "changed", "change to", "originally",
	"earlier", "initially", "before that", "prior to", "evolve", "switched",
	"updated to", "before moving", "what was", "used to be",
}

var currentKeywords = []string{
	"currently", "current", "now", "latest", "right now", "as of", "present",
	"today", "recently", "updated", "last known", "what is now",
}

var queryQuestionWords = []string{
	"when", "did", "what", "where", "who", "how", "do", "does", "is", "are",
	"was", "were", "has", "have", "can", "could", "would", "should", "which",
	"will", "at", "in", "on", "the", "a", "an", "and", "or", "but", "for",
	"to", "of", "with", "from", "by", "after",
}

var yearRe = regexp.MustCompile(`\b(19|20)[0-9]{2}\b`)

// hasExplicitTimeWindow 检测 query 是否含显式时间范围 (年份/日期)。
func hasExplicitTimeWindow(q string) bool {
	return yearRe.MatchString(q)
}

// parseQueryWindow 解析 query 的显式时间窗 (V1: 提取年份, 窗口 = 该年整年)。
// 返回 ok=false 表示 query 无显式时间。
func parseQueryWindow(query string) (start, end time.Time, ok bool) {
	m := yearRe.FindString(query)
	if m == "" {
		return time.Time{}, time.Time{}, false
	}
	year, err := time.Parse("2006", m)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	start = year.UTC()
	end = year.AddDate(1, 0, 0)
	return start, end, true
}

var entityRe = regexp.MustCompile(`\b[A-Z][a-z]+(?:\s+[A-Z][a-z]+)*\b`)

// extractQueryEntities 提取 query 中的大写专有名词作为主题键 (确定性正则),
// 过滤句首疑问词/虚词。空则 query 无显式实体。
func extractQueryEntities(query string) []string {
	seen := make(map[string]bool)
	var entities []string
	for _, m := range entityRe.FindAllString(query, -1) {
		lower := strings.ToLower(m)
		skip := false
		for _, qw := range queryQuestionWords {
			if lower == qw {
				skip = true
				break
			}
		}
		if skip || seen[lower] {
			continue
		}
		seen[lower] = true
		entities = append(entities, m)
	}
	return entities
}

// candidateTemporalInfo 候选的时间信息: latest = 该候选所有 source OccurredAt 的最大值。
type candidateTemporalInfo struct {
	latest  *time.Time
	hasTime bool
}

// compileTemporalResolutionArm 027 独立编译臂。
//
// 与 exact-token 臂共享 formalCompileSourceList(expanded) 同一 flat source list
// (T114 候选逐字节一致), 但在 bundle 构建时按 query 语义对候选做确定性时间组织。
// 返回 engine 形状一致的 Bundle/Trace (下游 buildCompileBundle 等共享构建器不变)
// 外加 ResolutionAudit 供归因 (spec FR-008)。
func compileTemporalResolutionArm(query string, expanded []formalExpandedAnchor, limit int) (evidencecompiler.Bundle, evidencecompiler.Trace, ResolutionAudit, error) {
	sources := formalCompileSourceList(expanded)
	if len(sources) == 0 {
		return evidencecompiler.Bundle{}, evidencecompiler.Trace{}, ResolutionAudit{}, fmt.Errorf("no expanded sources for temporal resolution")
	}
	candidates := buildCompileCandidates(sources)

	// 1. 收集每候选的时间信息 (latest = 所有 source OccurredAt 的最大值)。
	latestByCandidate := make(map[string]candidateTemporalInfo, len(candidates))
	for _, source := range sources {
		info := latestByCandidate[source.Candidate.CandidateID]
		if source.Evidence.OccurredAt != nil {
			info.hasTime = true
			if info.latest == nil || source.Evidence.OccurredAt.After(*info.latest) {
				t := *source.Evidence.OccurredAt
				info.latest = &t
			}
		}
		latestByCandidate[source.Candidate.CandidateID] = info
	}

	mode := classifyQueryMode(query)
	audit := ResolutionAudit{Mode: string(mode)}

	var kept []evidencecompiler.Candidate
	switch mode {
	case ResolutionDegraded:
		// 退化: 与 exact-token 臂相同的 relevance 选择 (原样行为)。
		selections := selectExactTokenCandidates(query, candidates, limit)
		kept = keepBySelections(candidates, selections)
	case ResolutionTemporalWindow:
		kept = resolveTemporalWindow(candidates, latestByCandidate, query, &audit)
	default: // current_value / evolution_chain
		kept = resolveVersionOrdering(candidates, latestByCandidate, query, mode, &audit)
	}
	// 027 fail-open: 时间组织把候选过滤/选择为空时, 回退到 relevance 选择 (与
	// exact-token 基线一致), 绝不产出空 bundle — 空 bundle 会触发
	// no_evidence_fits_token_cap 使整题 invalid (paired run: 209/1540)。
	// 时间解析是排序/过滤/选值, 不做 need 剪枝 (026 负结果教训)。
	if len(kept) == 0 {
		selections := selectExactTokenCandidates(query, candidates, limit)
		kept = keepBySelections(candidates, selections)
	}

	// 2. 构建 engine 形状一致的 Bundle (复用 exact-token 的 item 构建模式)。
	items := make([]evidencecompiler.BundleItem, 0, len(kept))
	candidateIDs := make([]string, 0, len(kept))
	var sourceIDs []string
	seenSources := make(map[string]bool)
	for _, candidate := range kept {
		sources := make([]evidencecompiler.SourceSpan, 0, len(candidate.SourceIDs))
		for _, sourceID := range candidate.SourceIDs {
			sources = append(sources, evidencecompiler.SourceSpan{SourceID: sourceID})
			if !seenSources[sourceID] {
				seenSources[sourceID] = true
				sourceIDs = append(sourceIDs, sourceID)
			}
		}
		items = append(items, evidencecompiler.BundleItem{
			Kind:         evidencecompiler.ActionExtract,
			Text:         candidate.Text,
			Sources:      sources,
			CandidateIDs: []string{candidate.ID},
		})
		candidateIDs = append(candidateIDs, candidate.ID)
	}

	fallback := ""
	if len(items) == 0 {
		fallback = exactTokenArmFallback
	}
	trace := evidencecompiler.Trace{
		CandidateDigest:    evalJSONDigest(candidateIDs),
		CandidateIDs:       candidateIDs,
		CandidateSourceIDs: sourceIDs,
		FallbackReason:     fallback,
		Valid:              true,
	}
	bundle := evidencecompiler.Bundle{
		Items:     items,
		SourceIDs: sourceIDs,
	}
	return bundle, trace, audit, nil
}

// keepBySelections 按 exact-token selection 保留候选 (退化路径的确定性原样行为)。
func keepBySelections(candidates []evidencecompiler.Candidate, selections []exactTokenSelection) []evidencecompiler.Candidate {
	byID := make(map[string]bool, len(selections))
	for _, s := range selections {
		byID[s.CandidateID] = true
	}
	kept := make([]evidencecompiler.Candidate, 0, len(selections))
	for _, c := range candidates {
		if byID[c.ID] {
			kept = append(kept, c)
		}
	}
	return kept
}

// resolveTemporalWindow 时间窗约束: 仅保留 OccurredAt 覆盖 query 显式时间窗的候选。
// 无时间信息或越窗的候选排除并计数。V1 窗口 = 提取年份的整年。
func resolveTemporalWindow(candidates []evidencecompiler.Candidate, latestByCandidate map[string]candidateTemporalInfo, query string, audit *ResolutionAudit) []evidencecompiler.Candidate {
	start, end, ok := parseQueryWindow(query)
	if !ok {
		// 理论上 classifyQueryMode 已保证有窗口; 防御性退化。
		return nil
	}
	kept := make([]evidencecompiler.Candidate, 0, len(candidates))
	for _, c := range candidates {
		info := latestByCandidate[c.ID]
		if !info.hasTime {
			audit.UnresolvedTime++
			continue
		}
		if info.latest.Before(start) || !info.latest.Before(end) {
			audit.WindowExcluded++
			continue
		}
		kept = append(kept, c)
	}
	return kept
}

// resolveVersionOrdering 当前值/演化链解析:
//   - 提取 query 实体作主题键; 含相同实体的候选视为同一事实的版本组。
//   - 组内按 OccurredAt 排序; current_value 每组保留最新 (其余 superseded_excluded),
//     evolution_chain 保留全部 (时间序)。
//   - query 无实体或候选无实体匹配时, 退化为全局时间排序 (current 降序 / evolution 升序),
//     不做版本选择 (无主题键无法判定 supersede)。
//   - 无时间信息的候选保持在末尾 (不参与排序)。
func resolveVersionOrdering(candidates []evidencecompiler.Candidate, latestByCandidate map[string]candidateTemporalInfo, query string, mode ResolutionMode, audit *ResolutionAudit) []evidencecompiler.Candidate {
	entities := extractQueryEntities(query)
	hasEntities := len(entities) > 0

	// 主题键 → 候选分组。
	groups := make(map[string][]evidencecompiler.Candidate) // 实体 → 候选
	var ungrouped []evidencecompiler.Candidate
	groupCount := 0
	for _, c := range candidates {
		matched := false
		for _, entity := range entities {
			if strings.Contains(strings.ToLower(c.Text), strings.ToLower(entity)) {
				groups[entity] = append(groups[entity], c)
				matched = true
			}
		}
		if !matched {
			ungrouped = append(ungrouped, c)
		}
	}
	groupCount = len(groups)
	audit.GroupCount = groupCount
	for _, group := range groups {
		audit.VersionsConsidered += len(group)
	}

	sortTime := func(cs []evidencecompiler.Candidate) {
		sort.SliceStable(cs, func(i, j int) bool {
			ti, tj := latestByCandidate[cs[i].ID], latestByCandidate[cs[j].ID]
			if ti.hasTime != tj.hasTime {
				return ti.hasTime // 有时间者在前
			}
			if ti.hasTime {
				return ti.latest.Before(*tj.latest)
			}
			return false // 都无时间: 保持原序 (stable)
		})
	}

	// 收集组内排序后的保留集合。
	kept := make([]evidencecompiler.Candidate, 0, len(candidates))
	if hasEntities && groupCount > 0 {
		for _, entity := range entities {
			group := groups[entity]
			if len(group) == 0 {
				continue
			}
			sortTime(group)
			switch mode {
			case ResolutionCurrentValue:
				// 最新 valid 保留; 其余 superseded 排除。
				kept = append(kept, group[len(group)-1])
				audit.SupersededExcluded += len(group) - 1
			case ResolutionEvolutionChain:
				// 完整演化链: 全部保留, 时间序。
				kept = append(kept, group...)
			}
		}
	}
	// 未分组候选 (无实体匹配): 时间排序后附加 (current 降序最新优先 / evolution 升序)。
	if len(ungrouped) > 0 {
		sortTime(ungrouped)
		if mode == ResolutionCurrentValue {
			// 降序: 最新优先。
			reverseCandidates(ungrouped)
		}
		kept = append(kept, ungrouped...)
	}
	return kept
}

// reverseCandidates 原地反转候选切片 (current_value 未分组时最新优先)。
func reverseCandidates(cs []evidencecompiler.Candidate) {
	for i, j := 0, len(cs)-1; i < j; i, j = i+1, j-1 {
		cs[i], cs[j] = cs[j], cs[i]
	}
}

// resolutionAuditSchema identifies one resolution-audit journal line.
const resolutionAuditSchema = "resolution_audit/v1"

// appendResolutionAudit 追加一条 per-question 解析审计到 run-dir 的
// resolution_audit.jsonl (spec FR-008, contract Rule 7)。独立于 frozen 结构
// (不触碰 candidate/trace/bundle digest), 供 US2/US3 归因。resolution_oracle
// (superseded/current 是否在池) 由归因阶段以 fixed-gold oracle 补齐, V1 只记录
// 解析器自身的确定性输出。run-dir 为空 (dry-run) 时静默跳过。
func appendResolutionAudit(runDir, questionID string, audit ResolutionAudit) error {
	if strings.TrimSpace(runDir) == "" || strings.TrimSpace(questionID) == "" {
		return nil
	}
	record := struct {
		Schema             string `json:"schema"`
		QuestionID         string `json:"question_id"`
		Mode               string `json:"mode"`
		GroupCount         int    `json:"group_count"`
		VersionsConsidered int    `json:"versions_considered"`
		SupersededExcluded int    `json:"superseded_excluded"`
		WindowExcluded     int    `json:"window_excluded"`
		UnresolvedTime     int    `json:"unresolved_time"`
	}{
		Schema:             resolutionAuditSchema,
		QuestionID:         questionID,
		Mode:               audit.Mode,
		GroupCount:         audit.GroupCount,
		VersionsConsidered: audit.VersionsConsidered,
		SupersededExcluded: audit.SupersededExcluded,
		WindowExcluded:     audit.WindowExcluded,
		UnresolvedTime:     audit.UnresolvedTime,
	}
	path := filepath.Join(runDir, "resolution_audit.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644) //nolint:gosec // operator-selected run artifact
	if err != nil {
		return fmt.Errorf("open resolution audit: %w", err)
	}
	defer f.Close()
	b, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal resolution audit: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("write resolution audit: %w", err)
	}
	return nil
}
