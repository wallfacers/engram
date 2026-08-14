package main

// detectAdaptiveTopK 在降序融合分数序列上定位「相关→噪声」拐点，返回 per-query 的自适应 top-k。
//
// 依据 research.md Decision 1：RRF 融合分数是离散的 rank 倒数加和，不满足 TAA-k 论文的连续
// 重尾假设，故只取其几何洞察（排序曲线 steep→flat→steep）+ gap-based 规则。相关候选常被多个
// 信号（keyword/semantic/entity）同时命中，分数明显高于只被单信号命中的噪声候选，两者之间
// 留下一个显著大于平均的相邻 gap。
//
// 返回 fallback=true 时 adaptiveTopK=fixedTopK，行为与固定深度完全一致（FR-004）。
func detectAdaptiveTopK(scores []float64, minK, fixedTopK int) (adaptiveTopK, kneeIndex int, fallback bool) {
	n := len(scores)
	// 序列太短（不足 minK），没有可收缩的空间 → 回退固定深度。
	if n < 2 || n <= minK {
		return fixedTopK, 0, true
	}
	hi, lo := scores[0], scores[n-1]
	// 分数无差异（或未降序），无拐点可辨 → 回退。
	if hi <= lo {
		return fixedTopK, 0, true
	}
	span := hi - lo
	var maxGap, totalGap float64
	kneeIdx := 0
	for i := 0; i < n-1; i++ {
		g := (scores[i] - scores[i+1]) / span // 归一化相邻 gap
		totalGap += g
		if g > maxGap {
			maxGap = g
			kneeIdx = i
		}
	}
	meanGap := totalGap / float64(n-1)
	// 无显著拐点：最大 gap 未显著超过平均 gap（分数平滑下降，无相关→噪声分界）。
	// 阈值 2.0 是启发式，T006 诊断阶段用真实 gold-rank 数据回校。
	if maxGap < 2.0*meanGap {
		return fixedTopK, 0, true
	}
	k := kneeIdx + 1 // 保留前 kneeIdx+1 条（含拐点处）
	if k < minK {
		k = minK // 保守下限（FR-005）
	}
	if k > fixedTopK {
		k = fixedTopK
	}
	return k, kneeIdx, false
}
