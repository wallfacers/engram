package main

import (
	"context"
	"sort"
	"strings"

	"github.com/wallfacers/engram/memory"
)

type DemandAtom struct {
	Entity    string
	Slot      string
	Satisfied bool
}

type ChunkRole struct {
	Anchor        bool
	Duplicate     bool
	StateConflict bool
	Complement    bool
	Lure          bool
	Unknown       bool
}

type PCICSignals struct {
	DemandAtoms       []DemandAtom
	CandidateEntities map[string][]string
}

type PCICSelectionInput struct {
	Candidates        []memory.Result
	Budget            int
	TokenCeiling      int
	DemandAtoms       []DemandAtom
	CandidateEntities map[string][]string
	ChunkTurns        map[string][]string
	Meta              *PCICMeta
}

func derivePCICSignals(ctx context.Context, entries *memory.EntryStore, query string, candidates []memory.Result) (PCICSignals, error) {
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		names = append(names, candidate.Name)
	}
	entitiesByEntry, err := entries.EntitiesByEntry(ctx, names)
	if err != nil {
		return PCICSignals{}, err
	}
	cues, _, err := entries.EntitySignalsForQuery(ctx, query)
	if err != nil {
		return PCICSignals{}, err
	}

	candidateEntities := make(map[string]struct{})
	for _, entities := range entitiesByEntry {
		for _, entity := range entities {
			if norm := memory.EntityNorm(entity); norm != "" {
				candidateEntities[norm] = struct{}{}
			}
		}
	}
	wanted := make(map[string]struct{})
	for _, token := range memory.EntityQueryTokens(query) {
		norm := memory.EntityNorm(token)
		if _, ok := candidateEntities[norm]; ok {
			wanted[norm] = struct{}{}
		}
	}
	for _, cue := range cues {
		if norm := memory.EntityNorm(cue); norm != "" {
			wanted[norm] = struct{}{}
		}
	}
	entities := make([]string, 0, len(wanted))
	for entity := range wanted {
		entities = append(entities, entity)
	}
	sort.Strings(entities)
	atoms := make([]DemandAtom, 0, len(entities))
	for _, entity := range entities {
		atoms = append(atoms, DemandAtom{Entity: entity})
	}
	return PCICSignals{DemandAtoms: atoms, CandidateEntities: entitiesByEntry}, nil
}

func classifyChunkRole(candidate memory.Result, index int, selected []memory.Result, input PCICSelectionInput, atoms []DemandAtom) ChunkRole {
	role := ChunkRole{Anchor: index < 2}
	claims := claimsForChunk(candidate, input)
	if len(claims) == 0 {
		role.Unknown = true
		return role
	}

	for _, candidateClaim := range claims {
		for _, selectedChunk := range selected {
			for _, selectedClaim := range claimsForChunk(selectedChunk, input) {
				if claimsConflict(candidateClaim, selectedClaim) {
					role.StateConflict = true
				}
				if claimsDuplicate(candidateClaim, selectedClaim) {
					role.Duplicate = true
				}
			}
		}
		for _, atom := range atoms {
			if !atom.Satisfied && claimSatisfiesAtom(candidateClaim, atom) {
				role.Complement = true
			}
		}
	}
	if role.StateConflict {
		role.Duplicate = false
	}
	role.Lure = !role.Complement && highConfidenceLure(claims, atoms)
	return role
}

func claimsForChunk(candidate memory.Result, input PCICSelectionInput) []SpanClaim {
	if input.Meta == nil {
		return nil
	}
	turns := input.ChunkTurns[candidate.Name]
	claims := make([]SpanClaim, 0, len(turns))
	for _, turnID := range turns {
		if claim, ok := input.Meta.Spans[turnID]; ok {
			claims = append(claims, claim)
		}
	}
	return claims
}

func claimsDuplicate(a, b SpanClaim) bool {
	return completeClaimKey(a) && completeClaimKey(b) &&
		memory.EntityNorm(a.Entity) == memory.EntityNorm(b.Entity) &&
		memory.EntityNorm(a.Slot) == memory.EntityNorm(b.Slot) &&
		memory.EntityNorm(a.Value) == memory.EntityNorm(b.Value) &&
		memory.EntityNorm(a.TimeState) == memory.EntityNorm(b.TimeState)
}

func claimsConflict(a, b SpanClaim) bool {
	if memory.EntityNorm(a.Entity) == "" || memory.EntityNorm(a.Slot) == "" ||
		memory.EntityNorm(b.Entity) == "" || memory.EntityNorm(b.Slot) == "" {
		return false
	}
	if memory.EntityNorm(a.Entity) != memory.EntityNorm(b.Entity) || memory.EntityNorm(a.Slot) != memory.EntityNorm(b.Slot) {
		return false
	}
	aValue, bValue := memory.EntityNorm(a.Value), memory.EntityNorm(b.Value)
	aTime, bTime := memory.EntityNorm(a.TimeState), memory.EntityNorm(b.TimeState)
	if aValue == "" || bValue == "" || aTime == "" || bTime == "" {
		return false
	}
	return aValue != bValue || aTime != bTime
}

func completeClaimKey(claim SpanClaim) bool {
	return memory.EntityNorm(claim.Entity) != "" && memory.EntityNorm(claim.Slot) != "" &&
		memory.EntityNorm(claim.Value) != "" && memory.EntityNorm(claim.TimeState) != ""
}

func claimSatisfiesAtom(claim SpanClaim, atom DemandAtom) bool {
	if memory.EntityNorm(claim.Entity) == "" || memory.EntityNorm(claim.Entity) != memory.EntityNorm(atom.Entity) {
		return false
	}
	return atom.Slot == "" || memory.EntityNorm(claim.Slot) == memory.EntityNorm(atom.Slot)
}

func highConfidenceLure(claims []SpanClaim, atoms []DemandAtom) bool {
	matchedEntity := false
	for _, claim := range claims {
		claimEntity := memory.EntityNorm(claim.Entity)
		claimSlot := memory.EntityNorm(claim.Slot)
		if claimEntity == "" || claimSlot == "" {
			return false
		}
		claimMatchesDemandEntity := false
		for _, atom := range atoms {
			if claimEntity != memory.EntityNorm(atom.Entity) {
				continue
			}
			claimMatchesDemandEntity = true
			matchedEntity = true
			if atom.Slot == "" || claimSlot == memory.EntityNorm(atom.Slot) {
				return false
			}
		}
		if !claimMatchesDemandEntity {
			return false
		}
	}
	return matchedEntity
}

func pcicSelect(input PCICSelectionInput) []memory.Result {
	candidates := input.Candidates
	if len(candidates) > pcicWindowSize {
		candidates = candidates[:pcicWindowSize]
	}
	budget := input.Budget
	if budget > 12 {
		budget = 12
	}
	if budget > len(candidates) {
		budget = len(candidates)
	}
	if budget <= 0 {
		return nil
	}
	ceiling := input.TokenCeiling
	if ceiling <= 0 {
		for _, candidate := range candidates[:budget] {
			ceiling += pcicChunkTokenCost(candidate)
		}
	}

	atoms := append([]DemandAtom(nil), input.DemandAtoms...)
	selected := make([]memory.Result, 0, budget)
	selectedIndexes := make(map[int]struct{}, budget)
	tokenCost := 0
	tryAdd := func(index int) bool {
		if len(selected) >= budget {
			return false
		}
		if _, exists := selectedIndexes[index]; exists {
			return false
		}
		cost := pcicChunkTokenCost(candidates[index])
		if tokenCost+cost > ceiling {
			return false
		}
		selected = append(selected, candidates[index])
		selectedIndexes[index] = struct{}{}
		tokenCost += cost
		markSatisfiedAtoms(atoms, claimsForChunk(candidates[index], input))
		return true
	}

	for i := 0; i < len(candidates) && i < 2; i++ {
		tryAdd(i)
	}
	for i := 2; i < len(candidates) && len(selected) < budget; i++ {
		role := classifyChunkRole(candidates[i], i, selected, input, atoms)
		if role.Duplicate || !role.Complement {
			continue
		}
		tryAdd(i)
	}
	for i := 2; i < len(candidates) && len(selected) < budget; i++ {
		if _, exists := selectedIndexes[i]; exists {
			continue
		}
		role := classifyChunkRole(candidates[i], i, selected, input, atoms)
		if role.Duplicate || role.Lure {
			continue
		}
		tryAdd(i)
	}
	for i := 2; i < len(candidates) && len(selected) < budget; i++ {
		if _, exists := selectedIndexes[i]; exists {
			continue
		}
		role := classifyChunkRole(candidates[i], i, selected, input, atoms)
		if role.Duplicate || !role.Lure {
			continue
		}
		tryAdd(i)
	}
	return selected
}

func markSatisfiedAtoms(atoms []DemandAtom, claims []SpanClaim) {
	for i := range atoms {
		if atoms[i].Satisfied {
			continue
		}
		for _, claim := range claims {
			if claimSatisfiesAtom(claim, atoms[i]) {
				atoms[i].Satisfied = true
				break
			}
		}
	}
}

func pcicChunkTokenCost(candidate memory.Result) int {
	return len(strings.Fields(candidate.Content))
}
