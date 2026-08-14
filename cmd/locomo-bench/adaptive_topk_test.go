package main

import "testing"

func TestDetectAdaptiveTopK(t *testing.T) {
	tests := []struct {
		name         string
		scores       []float64
		minK         int
		fixedTopK    int
		wantTopK     int
		wantKnee     int
		wantFallback bool
	}{
		{
			name:         "显著拐点：前 3 个相关、其余噪声，clamp 到 minK",
			scores:       []float64{1.0, 0.9, 0.85, 0.10, 0.09, 0.08, 0.07, 0.06},
			minK:         5,
			fixedTopK:    150,
			wantTopK:     5, // knee=3，clamp 到 minK=5
			wantKnee:     2,
			wantFallback: false,
		},
		{
			name:         "平滑下降无拐点 → 回退固定深度",
			scores:       []float64{1.0, 0.8, 0.6, 0.4, 0.2, 0.0},
			minK:         5,
			fixedTopK:    150,
			wantTopK:     150,
			wantKnee:     0,
			wantFallback: true,
		},
		{
			name:         "minK clamp：拐点低于保守下限，提升到 minK",
			scores:       []float64{1.0, 0.9, 0.85, 0.10, 0.09, 0.08, 0.07, 0.06, 0.05, 0.04, 0.03, 0.02, 0.01, 0.009, 0.008, 0.007, 0.006, 0.005, 0.004, 0.003},
			minK:         10,
			fixedTopK:    150,
			wantTopK:     10, // knee=3，clamp 到 minK=10
			wantKnee:     2,
			wantFallback: false,
		},
		{
			name:         "空序列 → 回退",
			scores:       []float64{},
			minK:         5,
			fixedTopK:    150,
			wantTopK:     150,
			wantKnee:     0,
			wantFallback: true,
		},
		{
			name:         "单元素序列 → 回退",
			scores:       []float64{1.0},
			minK:         5,
			fixedTopK:    150,
			wantTopK:     150,
			wantKnee:     0,
			wantFallback: true,
		},
		{
			name:         "全相同分数 → 回退",
			scores:       []float64{0.5, 0.5, 0.5, 0.5},
			minK:         5,
			fixedTopK:    150,
			wantTopK:     150,
			wantKnee:     0,
			wantFallback: true,
		},
		{
			name:         "信号降级短稀疏序列（n<=minK）→ 回退（FR-007）",
			scores:       []float64{1.0, 0.9, 0.8},
			minK:         5,
			fixedTopK:    150,
			wantTopK:     150,
			wantKnee:     0,
			wantFallback: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTopK, gotKnee, gotFallback := detectAdaptiveTopK(tt.scores, tt.minK, tt.fixedTopK)
			if gotTopK != tt.wantTopK || gotKnee != tt.wantKnee || gotFallback != tt.wantFallback {
				t.Fatalf("detectAdaptiveTopK(%v, %d, %d) = (%d, %d, %v), want (%d, %d, %v)",
					tt.scores, tt.minK, tt.fixedTopK, gotTopK, gotKnee, gotFallback, tt.wantTopK, tt.wantKnee, tt.wantFallback)
			}
		})
	}
}

// TestDetectAdaptiveTopKInvariant 断言自适应 k 始终落在 [minK, fixedTopK] 区间且不产生负截断。
func TestDetectAdaptiveTopKInvariant(t *testing.T) {
	// 模拟 RRF 融合分数：前 4 个候选被 3 信号命中（分数高），其余只被 1 信号命中（分数低）。
	scores := make([]float64, 0, 300)
	for r := 1; r <= 4; r++ {
		scores = append(scores, 3.0/(60.0+float64(r)))
	}
	for r := 5; r <= 300; r++ {
		scores = append(scores, 1.0/(60.0+float64(r)))
	}
	topK, knee, fallback := detectAdaptiveTopK(scores, 30, 150)
	if fallback {
		t.Fatalf("expected a detectable knee in 3-signal vs 1-signal RRF profile, got fallback")
	}
	if knee != 3 { // 拐点在 index 3（第 4 个 vs 第 5 个候选之间）
		t.Fatalf("knee = %d, want 3", knee)
	}
	if topK < 30 || topK > 150 {
		t.Fatalf("adaptive topK = %d out of [30, 150]", topK)
	}
}
