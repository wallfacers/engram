package main

import "github.com/wallfacers/engram/memory"

func pcicOracleSelect(candidates []memory.Result, budget int, chunkTurns map[string][]string, goldTurns []string) []memory.Result {
	if budget > 12 {
		budget = 12
	}
	if budget > len(candidates) {
		budget = len(candidates)
	}
	if budget <= 0 {
		return nil
	}
	gold := make(map[string]struct{}, len(goldTurns))
	for _, turnID := range goldTurns {
		gold[turnID] = struct{}{}
	}
	uncovered := make(map[string]struct{}, len(gold))
	for turnID := range gold {
		uncovered[turnID] = struct{}{}
	}
	selected := make([]memory.Result, 0, budget)
	used := make(map[int]struct{}, budget)
	for len(selected) < budget && len(uncovered) > 0 {
		bestIndex, bestGain := -1, 0
		for i, candidate := range candidates {
			if _, exists := used[i]; exists {
				continue
			}
			gain := 0
			seen := make(map[string]struct{})
			for _, turnID := range chunkTurns[candidate.Name] {
				if _, duplicate := seen[turnID]; duplicate {
					continue
				}
				seen[turnID] = struct{}{}
				if _, ok := uncovered[turnID]; ok {
					gain++
				}
			}
			if gain > bestGain {
				bestIndex, bestGain = i, gain
			}
		}
		if bestIndex < 0 {
			break
		}
		selected = append(selected, candidates[bestIndex])
		used[bestIndex] = struct{}{}
		for _, turnID := range chunkTurns[candidates[bestIndex].Name] {
			delete(uncovered, turnID)
		}
	}
	return selected
}
