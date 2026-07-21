package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

const answerableBaseline = 0.676

type AbstainGate struct {
	MinAdvRecall    float64 `json:"min_adv_recall"`
	MaxFalseAbstain float64 `json:"max_false_abstain"`
	MinNet          int     `json:"min_net"`
}

func defaultAbstainGate() AbstainGate {
	return AbstainGate{MinAdvRecall: 0.40, MaxFalseAbstain: 0.05, MinNet: 100}
}

func gateForOptions(opt options) AbstainGate {
	if opt.abstainGate.MinAdvRecall == 0 && opt.abstainGate.MaxFalseAbstain == 0 && opt.abstainGate.MinNet == 0 {
		return defaultAbstainGate()
	}
	return opt.abstainGate
}

func parseAbstainGate(spec string) (AbstainGate, error) {
	if strings.TrimSpace(spec) == "" {
		return defaultAbstainGate(), nil
	}
	gate := AbstainGate{}
	seen := map[string]bool{}
	for _, part := range strings.Split(spec, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || key == "" || value == "" || seen[key] {
			return AbstainGate{}, fmt.Errorf("invalid --abstain-gate %q", spec)
		}
		seen[key] = true
		switch key {
		case "advrecall":
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || parsed < 0 || parsed > 1 {
				return AbstainGate{}, fmt.Errorf("invalid advrecall in --abstain-gate %q", spec)
			}
			gate.MinAdvRecall = parsed
		case "falseabstain":
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || parsed < 0 || parsed > 1 {
				return AbstainGate{}, fmt.Errorf("invalid falseabstain in --abstain-gate %q", spec)
			}
			gate.MaxFalseAbstain = parsed
		case "net":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 0 {
				return AbstainGate{}, fmt.Errorf("invalid net in --abstain-gate %q", spec)
			}
			gate.MinNet = parsed
		default:
			return AbstainGate{}, fmt.Errorf("invalid --abstain-gate key %q", key)
		}
	}
	if len(seen) != 3 {
		return AbstainGate{}, fmt.Errorf("--abstain-gate needs advrecall, falseabstain, and net")
	}
	return gate, nil
}

func abstainProbeOutputPath(opt options) string {
	if opt.abstainProbeOut != "" {
		return opt.abstainProbeOut
	}
	baseDir := opt.storeDir
	if baseDir == "" {
		baseDir = opt.runDir
	}
	return filepath.Join(baseDir, "abstain-probe.json")
}

type ROCPoint struct {
	Threshold              float64 `json:"threshold"`
	AdversarialRecall      float64 `json:"adversarial_recall"`
	AnswerableFalseAbstain float64 `json:"answerable_false_abstain"`
	NetQuestions           int     `json:"net_questions"`
}

type SignalROC struct {
	Signal    string     `json:"signal"`
	AUC       float64    `json:"auc"`
	MeetsGate bool       `json:"meets_gate"`
	BestPoint ROCPoint   `json:"best_point"`
	Points    []ROCPoint `json:"points"`
}

type abstainPopulation struct {
	Adversarial int `json:"adversarial"`
	Answerable  int `json:"answerable"`
	Total       int `json:"total"`
}

type ProbeReport struct {
	Store            string            `json:"store"`
	Counts           abstainPopulation `json:"counts"`
	Signals          []SignalROC       `json:"signals"`
	Gate             AbstainGate       `json:"gate"`
	Verdict          string            `json:"verdict"`
	WinningSignal    *string           `json:"winning_signal"`
	ConfidenceSource ConfidenceSource  `json:"confidence_source"`
	ZeroLLM          bool              `json:"zero_llm"`
}

type probeScore struct {
	Adversarial bool
	Score       float64
}

// abstainProbeCallers is accepted so tests can prove the probe does not invoke
// answer or judge models. The implementation deliberately never reads it.
type abstainProbeCallers struct {
	Answer modelCaller
	Judge  modelCaller
}

func abstainPopulationCounts(convs []conversation) abstainPopulation {
	counts := abstainPopulation{}
	for _, conv := range convs {
		for _, qa := range conv.QA {
			counts.Total++
			if qa.Category == adversarialCategory {
				counts.Adversarial++
			} else {
				counts.Answerable++
			}
		}
	}
	return counts
}

func computeAbstainProbe(ctx context.Context, opt options, convs []conversation, runtimes []*conversationRuntime, arms []string, _ abstainProbeCallers, _ *slog.Logger) (ProbeReport, error) {
	if len(arms) == 0 {
		return ProbeReport{}, fmt.Errorf("abstain probe needs at least one retrieval arm")
	}
	arm := probeArm(arms)
	claimScores := make([]probeScore, 0)
	confidenceScores := make([]probeScore, 0)
	confidenceTop1Scores := make([]probeScore, 0)
	signals := make([]AbstainSignal, 0)
	confidenceSource := ConfidenceSourceCosine

	for _, conv := range convs {
		if conv.ID < 0 || conv.ID >= len(runtimes) || runtimes[conv.ID] == nil {
			return ProbeReport{}, fmt.Errorf("abstain probe runtime unavailable for conversation %d", conv.ID)
		}
		runtime := runtimes[conv.ID]
		retriever := runtime.retrievers[arm]
		if retriever == nil {
			return ProbeReport{}, fmt.Errorf("abstain probe retriever unavailable for arm %q", arm)
		}
		armOpt := optionsForRun(opt, arm, len(arms) > 1)
		for questionIndex, qa := range conv.QA {
			topK, quota := armOpt.retrievalFor(qa.Category)
			selector, _ := selectorForArm(runtime, conv.ID, arm, armOpt, nil, false)
			hits, _, err := retrieveWithQuotaDiagnostics(ctx, retriever, qa.Question, topK, quota, selector)
			if err != nil {
				return ProbeReport{}, fmt.Errorf("abstain probe retrieve conv=%d question=%d: %w", conv.ID, questionIndex, err)
			}
			questionID := qa.QuestionID
			if questionID == "" {
				questionID = questionIDForProbe(conv.ID, questionIndex)
			}
			signal, err := computeAbstainSignal(ctx, runtime.entries, qa.Question, abstainSignalInput{
				QuestionID:        questionID,
				Category:          qa.Category,
				Candidates:        hits,
				Meta:              opt.pcicMeta,
				ChunkTurns:        runtime.chunkTurns,
				SpanKey:           func(turnID string) string { return pcicSpanKey(conv.ID, turnID) },
				Reranked:          runtime.reranked[arm],
				CosineByCandidate: probeCandidateCosines(ctx, runtime, qa.Question, hits),
			})
			if err != nil {
				return ProbeReport{}, fmt.Errorf("abstain probe signal conv=%d question=%d: %w", conv.ID, questionIndex, err)
			}
			confidenceSource = signal.ConfidenceSource
			signals = append(signals, signal)
			confidenceScores = append(confidenceScores, probeScore{Adversarial: signal.Adversarial, Score: 1 - signal.Confidence})
			confidenceTop1Scores = append(confidenceTop1Scores, probeScore{Adversarial: signal.Adversarial, Score: 1 - signal.ConfidenceTop1})
			if signal.ClaimSignalPresent {
				claimScores = append(claimScores, probeScore{Adversarial: signal.Adversarial, Score: claimAbstentionScore(signal.ClaimMatch)})
			}
		}
	}

	gate := gateForOptions(opt)
	report := ProbeReport{
		Store:            opt.storeDir,
		Counts:           abstainPopulationCounts(convs),
		Gate:             gate,
		ConfidenceSource: confidenceSource,
		ZeroLLM:          true,
	}
	if len(claimScores) > 0 {
		report.Signals = append(report.Signals, sweepSignalROC("claim", claimScores, gate))
	}
	report.Signals = append(report.Signals, sweepSignalROC("confidence", confidenceScores, gate))
	report.Signals = append(report.Signals, sweepSignalROC("confidence-top1", confidenceTop1Scores, gate))
	if len(claimScores) == len(signals) && len(signals) > 0 {
		report.Signals = append(report.Signals, sweepCombinedROC(signals, gate))
	}
	report.Verdict, report.WinningSignal = probeVerdict(report.Signals, gate)
	return report, nil
}

func questionIDForProbe(convID, questionIndex int) string {
	return fmt.Sprintf("conv-%d-q-%d", convID, questionIndex)
}

func probeArm(arms []string) string {
	for _, arm := range arms {
		if optionsForArm(options{}, arm).rerank {
			return arm
		}
	}
	for _, arm := range arms {
		if armBackend(arm) == "hybrid" {
			return arm
		}
	}
	return arms[0]
}

// probeCandidateCosines uses public adapter-visible APIs only: it embeds the
// query, reads stored vectors, and scores the already retrieved candidates.
func probeCandidateCosines(ctx context.Context, runtime *conversationRuntime, question string, candidates []memory.Result) map[string]float64 {
	if runtime == nil || runtime.embedClient == nil || runtime.vectors == nil || len(candidates) == 0 {
		return nil
	}
	queryVectors, err := runtime.embedClient.Embed(ctx, []string{question})
	if err != nil || len(queryVectors) != 1 || len(queryVectors[0]) == 0 {
		return nil
	}
	stored, err := runtime.vectors.LoadAllForModel(ctx, runtime.embedClient.Model())
	if err != nil {
		return nil
	}
	out := make(map[string]float64, len(candidates))
	for _, candidate := range candidates {
		if vector, ok := stored[candidate.Name]; ok {
			out[candidate.Name] = embedding.Cosine(queryVectors[0], vector)
		}
	}
	return out
}

func sweepSignalROC(signal string, scores []probeScore, gate AbstainGate) SignalROC {
	points := make([]ROCPoint, 0)
	for _, threshold := range probeThresholds(scores) {
		points = append(points, rocPoint(scores, threshold, func(score probeScore) bool {
			return score.Score >= threshold
		}))
	}
	return finishSignalROC(signal, points, gate)
}

func sweepCombinedROC(signals []AbstainSignal, gate AbstainGate) SignalROC {
	scores := make([]probeScore, 0, len(signals))
	for _, signal := range signals {
		scores = append(scores, probeScore{Adversarial: signal.Adversarial, Score: 1 - signal.Confidence})
	}
	points := make([]ROCPoint, 0)
	for _, threshold := range probeThresholds(scores) {
		points = append(points, rocPointForSignals(signals, threshold))
	}
	return finishSignalROC("combined", points, gate)
}

func probeThresholds(scores []probeScore) []float64 {
	if len(scores) == 0 {
		return []float64{1}
	}
	seen := map[float64]struct{}{}
	maxScore := scores[0].Score
	for _, score := range scores {
		seen[score.Score] = struct{}{}
		if score.Score > maxScore {
			maxScore = score.Score
		}
	}
	thresholds := make([]float64, 0, len(seen)+1)
	thresholds = append(thresholds, math.Nextafter(maxScore, math.Inf(1)))
	for threshold := range seen {
		thresholds = append(thresholds, threshold)
	}
	sort.Slice(thresholds, func(i, j int) bool { return thresholds[i] > thresholds[j] })
	return thresholds
}

func rocPoint(scores []probeScore, threshold float64, flagged func(probeScore) bool) ROCPoint {
	advTotal, answerableTotal, flaggedAdv, flaggedAnswerable := 0, 0, 0, 0
	for _, score := range scores {
		if score.Adversarial {
			advTotal++
		} else {
			answerableTotal++
		}
		if !flagged(score) {
			continue
		}
		if score.Adversarial {
			flaggedAdv++
		} else {
			flaggedAnswerable++
		}
	}
	return ROCPoint{
		Threshold:              threshold,
		AdversarialRecall:      ratio(flaggedAdv, advTotal),
		AnswerableFalseAbstain: ratio(flaggedAnswerable, answerableTotal),
		NetQuestions:           int(math.Round(float64(flaggedAdv) - float64(flaggedAnswerable)*answerableBaseline)),
	}
}

func rocPointForSignals(signals []AbstainSignal, threshold float64) ROCPoint {
	scores := make([]probeScore, 0, len(signals))
	for _, signal := range signals {
		scores = append(scores, probeScore{Adversarial: signal.Adversarial, Score: 1 - signal.Confidence})
	}
	index := 0
	return rocPoint(scores, threshold, func(probeScore) bool {
		signal := signals[index]
		index++
		return signal.ClaimMatch == ClaimNoMatch || 1-signal.Confidence >= threshold
	})
}

func finishSignalROC(signal string, points []ROCPoint, gate AbstainGate) SignalROC {
	report := SignalROC{Signal: signal, Points: points}
	if len(points) == 0 {
		return report
	}
	report.AUC = rocAUC(points)
	report.BestPoint = points[0]
	for _, point := range points {
		if pointMeetsGate(point, gate) {
			if !report.MeetsGate || point.NetQuestions > report.BestPoint.NetQuestions {
				report.BestPoint = point
			}
			report.MeetsGate = true
			continue
		}
		if !report.MeetsGate && pointDistanceToGate(point, gate) < pointDistanceToGate(report.BestPoint, gate) {
			report.BestPoint = point
		}
	}
	return report
}

func rocAUC(points []ROCPoint) float64 {
	var auc float64
	for i := 1; i < len(points); i++ {
		x0, x1 := points[i-1].AnswerableFalseAbstain, points[i].AnswerableFalseAbstain
		y0, y1 := points[i-1].AdversarialRecall, points[i].AdversarialRecall
		auc += (x1 - x0) * (y0 + y1) / 2
	}
	return clamp01(auc)
}

func pointMeetsGate(point ROCPoint, gate AbstainGate) bool {
	return point.AdversarialRecall >= gate.MinAdvRecall &&
		point.AnswerableFalseAbstain <= gate.MaxFalseAbstain &&
		point.NetQuestions >= gate.MinNet
}

func pointDistanceToGate(point ROCPoint, gate AbstainGate) float64 {
	denominator := float64(gate.MinNet)
	if denominator <= 0 {
		denominator = 1
	}
	return math.Max(0, gate.MinAdvRecall-point.AdversarialRecall) +
		math.Max(0, point.AnswerableFalseAbstain-gate.MaxFalseAbstain) +
		math.Max(0, float64(gate.MinNet-point.NetQuestions)/denominator)
}

func evaluateProbeVerdict(signals []SignalROC, gate AbstainGate) (string, string) {
	verdict, winner := probeVerdict(signals, gate)
	if winner == nil {
		return verdict, ""
	}
	return verdict, *winner
}

func probeVerdict(signals []SignalROC, gate AbstainGate) (string, *string) {
	for _, signal := range signals {
		for _, point := range signal.Points {
			if pointMeetsGate(point, gate) {
				winner := signal.Signal
				return "GO", &winner
			}
		}
	}
	return "NO-GO", nil
}

func writeAbstainProbe(path string, report ProbeReport) error {
	if err := validateAbstainProbe(report); err != nil {
		return err
	}
	return writeJSON(path, report)
}

func validateAbstainProbe(report ProbeReport) error {
	if report.Counts.Total != report.Counts.Adversarial+report.Counts.Answerable {
		return fmt.Errorf("abstain probe counts total %d does not equal adversarial + answerable", report.Counts.Total)
	}
	if !report.ZeroLLM {
		return fmt.Errorf("abstain probe artifact must attest zero_llm=true")
	}
	anyMeetsGate := false
	for _, signal := range report.Signals {
		if !pointInROC(signal.BestPoint, signal.Points) {
			return fmt.Errorf("abstain probe %s best_point is not drawn from points", signal.Signal)
		}
		if signal.MeetsGate != pointMeetsGate(signal.BestPoint, report.Gate) {
			return fmt.Errorf("abstain probe %s meets_gate disagrees with best_point", signal.Signal)
		}
		anyMeetsGate = anyMeetsGate || signal.MeetsGate
	}
	if (report.Verdict == "GO") != anyMeetsGate {
		return fmt.Errorf("abstain probe verdict %q disagrees with signal gate results", report.Verdict)
	}
	if report.Verdict != "GO" && report.Verdict != "NO-GO" {
		return fmt.Errorf("abstain probe verdict %q is invalid", report.Verdict)
	}
	if report.Verdict == "GO" && (report.WinningSignal == nil || *report.WinningSignal == "") {
		return fmt.Errorf("abstain probe GO verdict is missing winning_signal")
	}
	if report.Verdict == "NO-GO" && report.WinningSignal != nil {
		return fmt.Errorf("abstain probe NO-GO verdict must not have winning_signal")
	}
	return nil
}

func pointInROC(want ROCPoint, points []ROCPoint) bool {
	for _, point := range points {
		if point == want {
			return true
		}
	}
	return false
}

// runAbstainProbeCLI opens the already-built stores without entering the normal
// extraction/ingestion build path. That preserves the probe's no-engine-write
// invariant while allowing retrieval against the persisted data.
func runAbstainProbeCLI(ctx context.Context, opt options, convs []conversation, arms []string, logger *slog.Logger) error {
	if opt.storeDir == "" {
		return fmt.Errorf("--abstain-probe requires --store-dir with persisted conversation stores")
	}
	outputPath := abstainProbeOutputPath(opt)
	if outputPath == "" {
		return fmt.Errorf("--abstain-probe needs --abstain-probe-out, --store-dir, or --run-dir")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create abstain probe output directory: %w", err)
	}

	var embClient embedding.Client
	if hasHybridBackend(arms) {
		embClient = buildBenchEmbeddingClient(logger, nil)
	}
	runtimes := make([]*conversationRuntime, len(convs))
	for index, conv := range convs {
		runtime, err := openAbstainProbeRuntime(ctx, opt, conv, embClient, arms)
		if err != nil {
			for _, opened := range runtimes {
				opened.Close()
			}
			return err
		}
		runtimes[index] = runtime
	}
	defer func() {
		for _, runtime := range runtimes {
			runtime.Close()
		}
	}()

	report, err := computeAbstainProbe(ctx, opt, convs, runtimes, arms, abstainProbeCallers{}, logger)
	if err != nil {
		return err
	}
	if err := writeAbstainProbe(outputPath, report); err != nil {
		return fmt.Errorf("write abstain probe: %w", err)
	}
	for _, signal := range report.Signals {
		fmt.Printf("abstain probe %s: auc=%.4f best=(tau=%.4f adv_recall=%.4f false_abstain=%.4f net=%d) gate=%t\n",
			signal.Signal, signal.AUC, signal.BestPoint.Threshold, signal.BestPoint.AdversarialRecall,
			signal.BestPoint.AnswerableFalseAbstain, signal.BestPoint.NetQuestions, signal.MeetsGate)
	}
	fmt.Printf("abstain probe verdict: %s\n", report.Verdict)
	return nil
}

func hasHybridBackend(arms []string) bool {
	for _, arm := range arms {
		if armBackend(arm) == "hybrid" {
			return true
		}
	}
	return false
}

func openAbstainProbeRuntime(ctx context.Context, opt options, conv conversation, embClient embedding.Client, arms []string) (*conversationRuntime, error) {
	path := filepath.Join(opt.storeDir, fmt.Sprintf("conv%d.db", conv.ID))
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("abstain probe needs persisted store %s", path)
		}
		return nil, fmt.Errorf("stat abstain probe store %s: %w", path, err)
	}
	st, err := store.Open(ctx, store.Options{DSN: path})
	if err != nil {
		return nil, fmt.Errorf("open abstain probe store %s: %w", path, err)
	}
	entries := memory.NewEntryStore(st.DB())
	vectors := memory.NewVectorStore(st.DB())
	retrievers := make(map[string]*memory.Retriever, len(arms))
	reranked := make(map[string]bool, len(arms))
	for _, arm := range arms {
		armOpt := optionsForRun(opt, arm, len(arms) > 1)
		retrieverOptions := retrieverOptionsForAt(armOpt, temporalNowForConversation(conv))
		if armBackend(arm) == "hybrid" {
			var reranker embedding.Reranker
			if armOpt.rerank {
				reranker = buildBenchReranker()
			}
			reranked[arm] = reranker != nil
			retrievers[arm] = memory.NewRetrieverWithOptions(entries, vectors, embClient, reranker, retrieverOptions)
			continue
		}
		retrievers[arm] = memory.NewRetrieverWithOptions(entries, vectors, nil, nil, retrieverOptions)
	}
	return &conversationRuntime{
		store:       st,
		entries:     entries,
		vectors:     vectors,
		embedClient: embClient,
		retrievers:  retrievers,
		reranked:    reranked,
		chunkTurns:  probeChunkTurns(conv),
	}, nil
}

func probeChunkTurns(conv conversation) map[string][]string {
	chunkTurns := make(map[string][]string)
	for _, session := range conv.Sessions {
		for index, chunk := range buildSessionChunks(session) {
			if len(chunk.DiaIDs) > 0 {
				chunkTurns[fmt.Sprintf("chunk-c%d-s%d-%03d", conv.ID, session.Index, index)] = chunk.DiaIDs
			}
		}
	}
	return chunkTurns
}
