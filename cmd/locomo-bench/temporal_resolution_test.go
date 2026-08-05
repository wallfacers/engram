package main

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

// temporal_resolution_test.go — 027 查询时时间有效性解析器测试 (T005/T006/T011/T012/T013)。
//
// 解析器纯 Go 确定性、零 LLM (宪法 V), 消费 compileFormalSources 同一 flat source list
// (候选逐字节一致, 契约 Rule 5)。本文件覆盖:
//   - T005 确定性: 同一 query + 同一 source list 重复运行, bundle/trace/audit 逐字节一致
//   - T006 退化路径: 无时间语义/无显式实体时输出与 exact-token 基线臂逐字节一致
//   - T011 当前值解析: 知识更新/当前状态语义选最新 valid 版本, superseded 排除并计数
//   - T012 演化链组装: 演化语义按 OccurredAt 全序组装完整链
//   - T013 时间窗约束: 显式时间范围过滤 + 越窗/无时间计数

func trTime(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

// trSource 构造一条 formalExpandedSource fixture。candidateID 非空以参与
// buildCompileCandidates 的候选去重; Result.Content 即候选文本 (实体匹配 + exact-token
// 打分的输入); Evidence.OccurredAt 即时间解析的时间戳。
func trSource(candidateID string, rank int, score float64, content string, sourceIDs []string, occurredAt *time.Time) formalExpandedSource {
	return formalExpandedSource{
		Candidate: evalRenderedCandidate{
			CandidateID: candidateID,
			Kind:        "atomic_fact",
			Rank:        rank,
			Score:       score,
			SourceIDs:   sourceIDs,
		},
		Evidence: memory.Evidence{OccurredAt: occurredAt},
		Result:   memory.Result{Content: content},
	}
}

func trAnchors(sources ...formalExpandedSource) []formalExpandedAnchor {
	return []formalExpandedAnchor{{Sources: sources}}
}

// TestTemporalResolutionDeterministic (T005) — 同一 query + 同一 source list 重复运行,
// bundle/trace digest/audit 逐字节一致。
func TestTemporalResolutionDeterministic(t *testing.T) {
	expanded := trAnchors(
		trSource("c1", 1, 0.6, "Emma's email address is emma@old.com", []string{"ev1"}, trTime(2023, time.January, 1)),
		trSource("c2", 2, 0.5, "Emma's email address is emma@new.com", []string{"ev2"}, trTime(2025, time.January, 1)),
	)
	const query = "What was Emma's email before it changed?"

	first, firstTrace, firstAudit, err := compileTemporalResolutionArm(query, expanded, 10)
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	for i := 0; i < 5; i++ {
		bundle, trace, audit, err := compileTemporalResolutionArm(query, expanded, 10)
		if err != nil {
			t.Fatalf("run %d failed: %v", i, err)
		}
		if !reflect.DeepEqual(bundle, first) {
			t.Fatalf("run %d: bundle not deterministic", i)
		}
		if trace.CandidateDigest != firstTrace.CandidateDigest {
			t.Fatalf("run %d: trace digest not deterministic", i)
		}
		if !reflect.DeepEqual(audit, firstAudit) {
			t.Fatalf("run %d: audit not deterministic", i)
		}
	}
}

// TestTemporalResolutionDegradedMatchesExactToken (T006) — query 无时间语义且无显式实体时
// 退化为 exact-token 基线行为: bundle 与 exact-token 臂逐字节一致, 不新增 token、不调用 LLM。
func TestTemporalResolutionDegradedMatchesExactToken(t *testing.T) {
	expanded := trAnchors(
		trSource("c1", 1, 0.6, "The meeting was about the launch plan", []string{"ev1"}, trTime(2024, time.March, 1)),
		trSource("c2", 2, 0.5, "We discussed the budget for next year", []string{"ev2"}, nil),
	)
	const query = "what happened in the meeting" // 无年份、无演化/当前关键词、无大写实体

	bundle, trace, audit, err := compileTemporalResolutionArm(query, expanded, 10)
	if err != nil {
		t.Fatalf("temporal arm failed: %v", err)
	}
	if audit.Mode != string(ResolutionDegraded) {
		t.Fatalf("expected degraded mode, got %q", audit.Mode)
	}
	if audit.GroupCount != 0 {
		t.Fatalf("expected no grouping in degraded mode, got group_count=%d", audit.GroupCount)
	}

	// 与 exact-token 基线臂逐字节一致 (退化 = 原样行为)。
	candidates := buildCompileCandidates(formalCompileSourceList(expanded))
	baseline, baselineTrace, err := compileExactTokenArm(query, candidates, 10)
	if err != nil {
		t.Fatalf("exact-token baseline failed: %v", err)
	}
	if !reflect.DeepEqual(bundle.Items, baseline.Items) {
		t.Fatalf("degraded bundle items diverge from baseline:\n got=%v\nwant=%v", bundle.Items, baseline.Items)
	}
	if !reflect.DeepEqual(bundle.SourceIDs, baseline.SourceIDs) {
		t.Fatalf("degraded source IDs diverge from baseline")
	}
	if !reflect.DeepEqual(trace.CandidateIDs, baselineTrace.CandidateIDs) {
		t.Fatalf("degraded candidate IDs diverge from baseline")
	}
}

// TestTemporalResolutionCurrentValue (T011) — 当前值语义 query 选最新 valid 版本,
// superseded 版本排除并记录到 audit。
func TestTemporalResolutionCurrentValue(t *testing.T) {
	expanded := trAnchors(
		trSource("c1", 1, 0.6, "Emma's email address is emma@old.com", []string{"ev1"}, trTime(2023, time.January, 1)),
		trSource("c2", 2, 0.5, "Emma's email address is emma@new.com", []string{"ev2"}, trTime(2025, time.January, 1)),
	)
	const query = "What is Emma's current email address?"

	bundle, trace, audit, err := compileTemporalResolutionArm(query, expanded, 10)
	if err != nil {
		t.Fatalf("temporal arm failed: %v", err)
	}
	if audit.Mode != string(ResolutionCurrentValue) {
		t.Fatalf("expected current_value mode, got %q", audit.Mode)
	}
	if len(bundle.Items) != 1 {
		t.Fatalf("expected exactly 1 current item, got %d items: %v", len(bundle.Items), bundle.Items)
	}
	if got := bundle.Items[0].CandidateIDs[0]; got != "c2" {
		t.Fatalf("expected latest version c2 to survive, got %q", got)
	}
	if len(trace.CandidateIDs) != 1 || trace.CandidateIDs[0] != "c2" {
		t.Fatalf("trace candidate IDs wrong: %v", trace.CandidateIDs)
	}
	if audit.GroupCount != 1 {
		t.Fatalf("expected 1 group, got %d", audit.GroupCount)
	}
	if audit.VersionsConsidered != 2 {
		t.Fatalf("expected 2 versions considered, got %d", audit.VersionsConsidered)
	}
	if audit.SupersededExcluded != 1 {
		t.Fatalf("expected 1 superseded excluded, got %d", audit.SupersededExcluded)
	}
}

// TestTemporalResolutionEvolutionChain (T012) — 演化语义 query 按 OccurredAt 全序组装
// 完整 superseded→current 链, 逐项绑 SourceID。
func TestTemporalResolutionEvolutionChain(t *testing.T) {
	expanded := trAnchors(
		trSource("c1", 1, 0.6, "Emma's email address is emma@old.com", []string{"ev1"}, trTime(2023, time.January, 1)),
		trSource("c2", 2, 0.5, "Emma's email address is emma@new.com", []string{"ev2"}, trTime(2025, time.January, 1)),
	)
	const query = "What was Emma's email before it changed?"

	bundle, _, audit, err := compileTemporalResolutionArm(query, expanded, 10)
	if err != nil {
		t.Fatalf("temporal arm failed: %v", err)
	}
	if audit.Mode != string(ResolutionEvolutionChain) {
		t.Fatalf("expected evolution_chain mode, got %q", audit.Mode)
	}
	if len(bundle.Items) != 2 {
		t.Fatalf("expected full 2-version chain, got %d items: %v", len(bundle.Items), bundle.Items)
	}
	if got := bundle.Items[0].CandidateIDs[0]; got != "c1" {
		t.Fatalf("expected oldest c1 first, got %q", got)
	}
	if got := bundle.Items[1].CandidateIDs[0]; got != "c2" {
		t.Fatalf("expected newest c2 second, got %q", got)
	}
	// 逐项绑 SourceID (契约: 演化链完整保留)。
	if len(bundle.Items[0].Sources) != 1 || bundle.Items[0].Sources[0].SourceID != "ev1" {
		t.Fatalf("item c1 source binding wrong: %v", bundle.Items[0].Sources)
	}
	if len(bundle.Items[1].Sources) != 1 || bundle.Items[1].Sources[0].SourceID != "ev2" {
		t.Fatalf("item c2 source binding wrong: %v", bundle.Items[1].Sources)
	}
	if audit.SupersededExcluded != 0 {
		t.Fatalf("expected no superseded exclusion in evolution chain, got %d", audit.SupersededExcluded)
	}
	if audit.VersionsConsidered != 2 {
		t.Fatalf("expected 2 versions considered, got %d", audit.VersionsConsidered)
	}
}

// TestTemporalResolutionWindow (T013) — query 含显式时间范围时仅保留 OccurredAt 覆盖
// 该范围的版本; 越窗与无时间候选排除并记录。
func TestTemporalResolutionWindow(t *testing.T) {
	expanded := trAnchors(
		trSource("c1", 1, 0.6, "The office address was 1 Main Street", []string{"ev1"}, trTime(2023, time.May, 1)),
		trSource("c2", 2, 0.5, "The office address is 99 New Avenue", []string{"ev2"}, trTime(2025, time.January, 15)),
		trSource("c3", 3, 0.4, "No timestamped detail here", []string{"ev3"}, nil),
	)
	const query = "What was the office address in 2023?"

	bundle, _, audit, err := compileTemporalResolutionArm(query, expanded, 10)
	if err != nil {
		t.Fatalf("temporal arm failed: %v", err)
	}
	if audit.Mode != string(ResolutionTemporalWindow) {
		t.Fatalf("expected temporal_window mode, got %q", audit.Mode)
	}
	if len(bundle.Items) != 1 {
		t.Fatalf("expected exactly 1 in-window item, got %d items: %v", len(bundle.Items), bundle.Items)
	}
	if got := bundle.Items[0].CandidateIDs[0]; got != "c1" {
		t.Fatalf("expected 2023-05 candidate c1 to survive window, got %q", got)
	}
	if audit.WindowExcluded != 1 {
		t.Fatalf("expected 1 out-of-window excluded, got %d", audit.WindowExcluded)
	}
	if audit.UnresolvedTime != 1 {
		t.Fatalf("expected 1 unresolved-time excluded, got %d", audit.UnresolvedTime)
	}
}

// TestTemporalResolutionMechanismFlagBinding (T004) — formal B1 下开启
// --temporal-resolution 产生 mechanism_flags{temporal_resolution:true} 且 protocol
// hash 与关闭态不同; 关闭态不产生该 key; 与 --compiler-arm 互斥; 非 formal 上下文
// fail-closed。
func TestTemporalResolutionMechanismFlagBinding(t *testing.T) {
	on := options{temporalResolution: true}
	off := options{}

	// (1) additive density flag on/off。
	if got := densityMechanismFlagsForOptions(on); !got["temporal_resolution"] {
		t.Fatalf("temporal_resolution flag missing when enabled: %v", got)
	}
	if got := densityMechanismFlagsForOptions(off); got["temporal_resolution"] {
		t.Fatalf("temporal_resolution flag present when disabled: %v", got)
	}

	// (2) buildFormalExperiment 冻结进 b1 control manifest。
	expOn, err := buildFormalExperiment(on, "")
	if err != nil {
		t.Fatalf("buildFormalExperiment(on) failed: %v", err)
	}
	if expOn.Stage != "b1" || !expOn.MechanismFlags["temporal_resolution"] {
		t.Fatalf("b1 manifest lacks temporal_resolution when enabled: %+v", expOn)
	}
	if !isFormalControlMechanismFlags(expOn.MechanismFlags) {
		t.Fatalf("b1 manifest with temporal_resolution rejected by control-flag validator")
	}
	expOff, err := buildFormalExperiment(off, "")
	if err != nil {
		t.Fatalf("buildFormalExperiment(off) failed: %v", err)
	}
	if expOff.MechanismFlags["temporal_resolution"] {
		t.Fatalf("b1 manifest carries temporal_resolution when disabled")
	}

	// (3) protocol hash 与关闭态不同 (契约 Rule 4: mechanism_flags 参与 hash)。
	base := testEvalProtocol()
	pOn, pOff := base, base
	pOn.Experiment = expOn
	pOff.Experiment = expOff
	hOn, err := evalProtocolFingerprint(pOn)
	if err != nil {
		t.Fatalf("fingerprint on: %v", err)
	}
	hOff, err := evalProtocolFingerprint(pOff)
	if err != nil {
		t.Fatalf("fingerprint off: %v", err)
	}
	if hOn == hOff {
		t.Fatalf("protocol hash identical with temporal_resolution on/off (%s)", hOn)
	}

	// (4) 与 --compiler-arm 互斥 (packer dispatch 单机制归因)。
	mutual := options{temporalResolution: true, compilerArm: "exact_token"}
	mutual.evalProtocolPath = "protocol.json"
	if err := validateMechanismArms(mutual); err == nil {
		t.Fatal("--temporal-resolution and --compiler-arm not rejected as mutually exclusive")
	}

	// (5) 非 formal 上下文 fail-closed。
	nf := options{temporalResolution: true}
	if err := validateMechanismArms(nf); err == nil {
		t.Fatal("--temporal-resolution silently accepted outside a formal context")
	}
}

// TestTemporalResolutionMaterializeCurrentValue (T010 + T011 harness 集成) — 同一 store、
// 双版本 (superseded/current) 证据下:
//   - control (temporalResolution=false) 走 chunk_900 legacy packer, 全量打包两版本
//     (默认关零行为变化: 不激活解析器);
//   - treated (temporalResolution=true) 走 current_value 解析, 只保留最新版本;
//   - treated 的 per-source 候选与 control 的 verbatim 候选共享同一检索产物
//     (T004 候选逐字节一致 = compileFormalSources 同一 flat source list)。
func TestTemporalResolutionMaterializeCurrentValue(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	entries := memory.NewEntryStore(st.DB())
	ledger := entries.Ledger()
	created, err := ledger.AppendBatch(ctx, []memory.EvidenceInput{
		{
			ExternalSourceID: "D1:1", SourceType: memory.EvidenceMessage,
			SourceSessionID: "conv0-sess1", Speaker: "Alice", Ordinal: 0,
			Content:    "Alice's address was 1 Main Street",
			OccurredAt: trTime(2023, time.May, 1),
			RecordedAt: time.Date(2023, time.May, 2, 0, 0, 0, 0, time.UTC),
		},
		{
			ExternalSourceID: "D1:2", SourceType: memory.EvidenceMessage,
			SourceSessionID: "conv0-sess2", Speaker: "Alice", Ordinal: 0,
			Content:    "Alice's address is 99 New Avenue",
			OccurredAt: trTime(2025, time.January, 15),
			RecordedAt: time.Date(2025, time.January, 16, 0, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// FTS buildPlan AND-joins every ASCII query token, so every query token must
	// appear in the projection content. Query = "needlefact current Alice" keeps
	// three shared tokens (needlefact retrieval marker, current_value semantic,
	// Alice entity). The answer-facing candidate text comes from Evidence spans
	// (which carry the versioned address), not the projection text.
	const query = "needlefact current Alice"
	for i, ev := range created {
		if err := entries.UpsertWithSources(ctx, &memory.Entry{
			Name: "proj-" + strconv.Itoa(i), Trigger: "retrieval-marker",
			Content: "needlefact alice current address anchor-" + strconv.Itoa(i), Category: "fact",
		}, []memory.EvidenceRef{{EvidenceID: ev.ID, SourceOrder: 0, FullSource: true}}); err != nil {
			t.Fatalf("upsert anchor %d: %v", i, err)
		}
	}

	protocol := sourceTestProtocol()
	qa := locomoQA{
		QuestionID: "locomo:0:0",
		Question:   query,
		Category:   4,
		Evidence:   []string{"D1:1", "D1:2"},
	}
	retriever := memory.NewRetriever(entries, memory.NewVectorStore(st.DB()), nil)
	projections := memory.NewProjectionStore(st.DB())
	turnEvidence := map[string]string{"D1:1": created[0].ID, "D1:2": created[1].ID}

	baseOpt := options{
		answerModel:    protocol.Models.Answerer.ID,
		formalCounter:  lengthCounter{fingerprint: protocol.Budget.CounterFingerprint},
		formalEvidence: ledger,
	}

	// control: temporalResolution=false → chunk_900 legacy packer (fold + 全量打包)。
	control := materializeFormalB1Question(ctx, protocol, baseOpt, retriever, projections, nil, qa, nil, turnEvidence)
	if len(control.InvalidReasons) != 0 {
		t.Fatalf("control materialization invalid: %v", control.InvalidReasons)
	}
	if len(control.Bundle.Items) != 2 {
		t.Fatalf("control chunk_900 baseline should pack both versions, got %d items", len(control.Bundle.Items))
	}

	// treated: temporalResolution=true → current_value 解析, 只保留最新版本。
	// 真实 CLI run 里处理臂 protocol 经 buildFormalExperiment 冻结并携带
	// temporal_resolution flag (机制可归因), 这里模拟同一冻结。
	treatedOpt := baseOpt
	treatedOpt.temporalResolution = true
	treatedProtocol := protocol
	treatedProtocol.Experiment.MechanismFlags = map[string]bool{
		"idk_retry": false, "iris": false, "rerank": false, "temporal_resolution": true,
	}
	treated := materializeFormalB1Question(ctx, treatedProtocol, treatedOpt, retriever, projections, nil, qa, nil, turnEvidence)
	if len(treated.InvalidReasons) != 0 {
		t.Fatalf("treated materialization invalid: %v", treated.InvalidReasons)
	}
	if len(treated.Bundle.Items) != 1 {
		t.Fatalf("treated current_value should keep exactly the latest version, got %d items: %v", len(treated.Bundle.Items), treated.Bundle.Items)
	}
	if got := treated.Bundle.Items[0].Text; !strings.Contains(got, "99 New Avenue") {
		t.Fatalf("treated kept wrong version %q (want latest 2025 address)", got)
	}

	// T016 配对有效性: 两臂同 store、候选锚逐字节一致。Anchors 派生自同一检索产物
	// (hits), fold 只改 RenderedCandidates 形态不改候选锚 —— 候选池 byte-identical,
	// 两臂只差机制 flag。
	if !reflect.DeepEqual(treated.Candidate.Anchors, control.Candidate.Anchors) {
		t.Fatalf("treated/control candidate anchors diverge (candidate pool not byte-identical)")
	}

	// 交叉 artifact digest 身份 (prepareFrozenFormalB1Answer 的 answer_input_drift 门)。
	if treated.Trace.CandidateSetDigest != treated.Candidate.CandidateSetDigest ||
		treated.Bundle.CandidateSetDigest != treated.Candidate.CandidateSetDigest ||
		treated.Bundle.TraceDigest != treated.Trace.TraceDigest {
		t.Fatalf("treated cross-artifact digest identity drift")
	}

	// 触发机制 flag 冻结 (契约: 处理臂 manifest 带 temporal_resolution)。
	exp, err := buildFormalExperiment(treatedOpt, "")
	if err != nil {
		t.Fatal(err)
	}
	if !exp.MechanismFlags["temporal_resolution"] {
		t.Fatal("treated options did not freeze temporal_resolution into the manifest")
	}
}

// TestTemporalResolutionCategoryPairedStatistics (T017) — 分类别配对统计: 对
// temporal/knowledge-update/multi-hop/single-hop 类别的 paired outcomes 聚合
// DeltaPP + exact McNemar p, Holm non-regression 门禁标记显著负类别, 显著负 →
// promotion STOP (FR-010/FR-011 默认关)。
func TestTemporalResolutionCategoryPairedStatistics(t *testing.T) {
	// (1) pairedCategoryComparison 聚合正确: 手工构造 discordant 对验证 DeltaPP/McNemar。
	got, err := pairedCategoryComparison("temporal",
		[]bool{true, false, false, true},
		[]bool{true, true, false, false},
	)
	if err != nil {
		t.Fatal(err)
	}
	// delta: q0 0, q1 +1, q2 0, q3 -1 → sum 0 → DeltaPP 0。
	// discordant: controlOKTreatmentWrong=1 (q3), controlWrongTreatmentOK=1 (q1) → p=1。
	if got.DeltaPP != 0 {
		t.Fatalf("temporal DeltaPP = %.2f, want 0 (balanced discordant pairs)", got.DeltaPP)
	}
	if got.PValue != 1 {
		t.Fatalf("temporal McNemar p = %v, want 1 (equal discordant counts)", got.PValue)
	}

	// (2) 分类别 Holm 门禁: multi_hop 显著负 → 标记; temporal 非显著正 → 不标记。
	gate := holmNegativeCategoryGate([]evalCategoryComparison{
		{Category: "temporal", DeltaPP: 1.5, PValue: 0.4},
		{Category: "knowledge_update", DeltaPP: 0.0, PValue: 1.0},
		{Category: "multi_hop", DeltaPP: -2.0, PValue: 0.01},
		{Category: "single_hop", DeltaPP: 0.0, PValue: 1.0},
	}, 0.05)
	if !gate["multi_hop"].HolmSignificantNegative {
		t.Fatalf("multi_hop significant negative not flagged by Holm gate")
	}
	if gate["temporal"].HolmSignificantNegative {
		t.Fatalf("temporal non-significant positive falsely flagged as negative regression")
	}

	// (3) 显著负回归 → promotion STOP (机制默认关, FR-010/FR-011)。
	valid := evalArtifactValidity{Valid: true, Complete: true, CandidateIdentityRate: 1,
		SourceValidationRate: 1, SpanRecoveryRate: 1, CitationCoverageRate: 1, WithinCapRate: 1,
		AnswerCallComplianceRate: 1, UnattributedAddCount: 0}
	verdict := promotionVerdictFor(evalPromotionInput{
		Validity: valid, PrimaryDeltaPP: 2.5, PrimaryMcNemarP: 0.03,
		OtherBenchmarkDeltaPP: 0.0, CandidateCoverageNonRegression: true,
		JudgeAuditComplete: true, JudgeAuditVerdictStable: true, OfflineCompatible: true,
		CategoryResults: gate,
	})
	if verdict != evalVerdictSTOP {
		t.Fatalf("promotion verdict = %s, want STOP when a category regresses significantly", verdict)
	}
}
