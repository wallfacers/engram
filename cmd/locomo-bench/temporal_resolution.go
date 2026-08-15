package main

import (
	"regexp"
	"strings"
)

// temporal_resolution.go — 027: 查询时时间有效性解析。044 清理后仅保留
// classifyQueryMode(确定性 query 语义分类),供 miss_attribution(notebook
// 归因)与 eval_runner 复用;作答期时间组织(compileTemporalResolutionArm、
// resolveTemporalWindow/resolveVersionOrdering 等)已随 027 NO-GO 移除。

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

var yearRe = regexp.MustCompile(`\b(19|20)[0-9]{2}\b`)

// hasExplicitTimeWindow 检测 query 是否含显式时间范围 (年份/日期)。
func hasExplicitTimeWindow(q string) bool {
	return yearRe.MatchString(q)
}
