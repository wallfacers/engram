package memory

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// EntityEdge is one undirected entity relationship. A and B are normalized and
// stored in lexical order; Kind is "co" or "syn".
type EntityEdge struct {
	A, B   string
	Kind   string
	Weight float64
}

const maxAssociativeDepth = 2

const entityEntryScoreEntityCap = 200

// UpsertEdges normalizes and stores entity relationships. Co-occurrence weights
// accumulate across writes, while a synonym edge records the latest similarity
// weight. Self-edges and blank/unknown kinds are ignored by the write path.
func (s *EntryStore) UpsertEdges(ctx context.Context, pairs []EntityEdge) error {
	if s == nil || len(pairs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memory: begin edge upsert: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	const query = `
		INSERT INTO memory_entity_edges(entity_a, entity_b, kind, weight, updated_at)
		VALUES (?,?,?,?,?)
		ON CONFLICT(entity_a, entity_b, kind) DO UPDATE SET
			weight = CASE
				WHEN memory_entity_edges.kind = 'co'
				THEN memory_entity_edges.weight + excluded.weight
				ELSE excluded.weight
			END,
			updated_at = excluded.updated_at`
	now := time.Now().UTC().UnixMicro()
	for _, pair := range pairs {
		a, b := EntityNorm(pair.A), EntityNorm(pair.B)
		kind := strings.ToLower(strings.TrimSpace(pair.Kind))
		if a == "" || b == "" || a == b {
			continue
		}
		if a > b {
			a, b = b, a
		}
		if kind != "co" && kind != "syn" {
			continue
		}
		weight := pair.Weight
		if weight == 0 {
			weight = 1
		}
		if _, err := tx.ExecContext(ctx, query, a, b, kind, weight, now); err != nil {
			return fmt.Errorf("memory: upsert edge %q/%q/%q: %w", a, b, kind, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memory: commit edge upsert: %w", err)
	}
	return nil
}

// NeighborsOf returns canonical edges touching any requested entity and, when
// supplied, matching any requested kind. The result is deterministic.
func (s *EntryStore) NeighborsOf(ctx context.Context, entities []string, kinds []string) ([]EntityEdge, error) {
	if s == nil {
		return nil, nil
	}
	entitySet := make(map[string]struct{}, len(entities))
	for _, raw := range entities {
		if norm := EntityNorm(raw); norm != "" {
			entitySet[norm] = struct{}{}
		}
	}
	if len(entitySet) == 0 {
		return nil, nil
	}
	entityArgs := make([]string, 0, len(entitySet))
	for entity := range entitySet {
		entityArgs = append(entityArgs, entity)
	}
	sort.Strings(entityArgs)
	placeholders := func(n int) string {
		return strings.TrimSuffix(strings.Repeat("?,", n), ",")
	}
	args := make([]any, 0, len(entityArgs)+len(kinds))
	for _, entity := range entityArgs {
		args = append(args, entity)
	}
	where := "(entity_a IN (" + placeholders(len(entityArgs)) + ") OR entity_b IN (" + placeholders(len(entityArgs)) + "))"
	// The second IN list has the same values as the first.
	for _, entity := range entityArgs {
		args = append(args, entity)
	}

	kindSet := make(map[string]struct{}, len(kinds))
	for _, raw := range kinds {
		if kind := strings.ToLower(strings.TrimSpace(raw)); kind == "co" || kind == "syn" {
			kindSet[kind] = struct{}{}
		}
	}
	if len(kindSet) > 0 {
		orderedKinds := make([]string, 0, len(kindSet))
		for kind := range kindSet {
			orderedKinds = append(orderedKinds, kind)
		}
		sort.Strings(orderedKinds)
		where += " AND kind IN (" + placeholders(len(orderedKinds)) + ")"
		for _, kind := range orderedKinds {
			args = append(args, kind)
		}
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT entity_a, entity_b, kind, weight FROM memory_entity_edges WHERE `+where+
			` ORDER BY entity_a, entity_b, kind`, args...)
	if err != nil {
		return nil, fmt.Errorf("memory: neighbors: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []EntityEdge
	for rows.Next() {
		var edge EntityEdge
		if err := rows.Scan(&edge.A, &edge.B, &edge.Kind, &edge.Weight); err != nil {
			return nil, fmt.Errorf("memory: scan neighbor: %w", err)
		}
		out = append(out, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: read neighbors: %w", err)
	}
	return out, nil
}

// EntityClusterEntries returns entries attached to the seed entities or to
// their one-hop co-occurrence/synonym neighbors. The result is deterministic;
// callers decide how to score and cap the returned candidate set.
func (s *EntryStore) EntityClusterEntries(ctx context.Context, seeds []string) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	entitySet := make(map[string]struct{}, len(seeds))
	for _, raw := range seeds {
		if entity := EntityNorm(raw); entity != "" {
			entitySet[entity] = struct{}{}
		}
	}
	if len(entitySet) == 0 {
		return nil, nil
	}
	normalized := make([]string, 0, len(entitySet))
	for entity := range entitySet {
		normalized = append(normalized, entity)
	}
	edges, err := s.NeighborsOf(ctx, normalized, []string{"co", "syn"})
	if err != nil {
		return nil, fmt.Errorf("memory: cluster neighbors: %w", err)
	}
	for _, edge := range edges {
		entitySet[edge.A] = struct{}{}
		entitySet[edge.B] = struct{}{}
	}
	entities := make([]string, 0, len(entitySet))
	for entity := range entitySet {
		entities = append(entities, entity)
	}
	scores := make(map[string]float64, len(entities))
	for _, entity := range entities {
		scores[entity] = 1
	}
	entries, err := s.EntityEntryScores(ctx, scores)
	if err != nil {
		return nil, fmt.Errorf("memory: cluster entries: %w", err)
	}
	result := make([]string, 0, len(entries))
	for name := range entries {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

// EntityDocFreq returns the number of distinct entries containing each entity.
func (s *EntryStore) EntityDocFreq(ctx context.Context) (map[string]int, error) {
	return s.entityDocFreq(ctx, nil)
}

// EntityDocFreqFor returns document frequencies only for the requested entity
// keys. Walkers use this bounded form so unrelated entities do not require a
// full-table GROUP BY on every query.
func (s *EntryStore) EntityDocFreqFor(ctx context.Context, entities []string) (map[string]int, error) {
	return s.entityDocFreq(ctx, entities)
}

func (s *EntryStore) entityDocFreq(ctx context.Context, entities []string) (map[string]int, error) {
	if s == nil {
		return nil, nil
	}
	query := `SELECT entity_norm, COUNT(DISTINCT entry_name) FROM memory_entities`
	args := make([]any, 0, len(entities))
	if len(entities) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(entities)), ",")
		query += ` WHERE entity_norm IN (` + placeholders + `)`
		for _, entity := range entities {
			args = append(args, EntityNorm(entity))
		}
	}
	query += ` GROUP BY entity_norm`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("memory: entity doc frequency: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	out := make(map[string]int)
	for rows.Next() {
		var entity string
		var count int
		if err := rows.Scan(&entity, &count); err != nil {
			return nil, fmt.Errorf("memory: scan entity doc frequency: %w", err)
		}
		out[entity] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: read entity doc frequency: %w", err)
	}
	return out, nil
}

// WalkEntityGraph expands cue entities through the undirected edge graph. Seed
// scores use inverse document frequency (1 / entry frequency), edge weights are
// accumulated, and the walk is bounded to two hops by design.
func (s *EntryStore) WalkEntityGraph(ctx context.Context, seeds []string, depth int) (map[string]float64, error) {
	if s == nil || depth <= 0 {
		return nil, nil
	}
	if depth > maxAssociativeDepth {
		depth = maxAssociativeDepth
	}
	normalizedSeeds := make([]string, 0, len(seeds))
	seenSeeds := make(map[string]struct{}, len(seeds))
	for _, raw := range seeds {
		entity := EntityNorm(raw)
		if entity == "" {
			continue
		}
		if _, seen := seenSeeds[entity]; seen {
			continue
		}
		seenSeeds[entity] = struct{}{}
		normalizedSeeds = append(normalizedSeeds, entity)
	}
	freq, err := s.EntityDocFreqFor(ctx, normalizedSeeds)
	if err != nil {
		return nil, err
	}
	frontier := make(map[string]float64)
	for _, entity := range normalizedSeeds {
		if count := freq[entity]; count > 0 {
			frontier[entity] += 1 / float64(count)
		}
	}
	if len(frontier) == 0 {
		return nil, nil
	}
	visited := make(map[string]struct{}, len(frontier))
	for entity := range frontier {
		visited[entity] = struct{}{}
	}
	scores := make(map[string]float64)
	for hop := 0; hop < depth && len(frontier) > 0; hop++ {
		entities := make([]string, 0, len(frontier))
		for entity := range frontier {
			entities = append(entities, entity)
		}
		sort.Strings(entities)
		edges, err := s.NeighborsOf(ctx, entities, []string{"co", "syn"})
		if err != nil {
			return nil, err
		}
		next := make(map[string]float64)
		for _, edge := range edges {
			for _, source := range []string{edge.A, edge.B} {
				sourceScore, ok := frontier[source]
				if !ok {
					continue
				}
				target := edge.B
				if target == source {
					target = edge.A
				}
				if target == "" || edge.Weight <= 0 {
					continue
				}
				if _, seen := visited[target]; seen {
					continue
				}
				next[target] += sourceScore * edge.Weight
			}
		}
		for entity, score := range next {
			scores[entity] += score
			visited[entity] = struct{}{}
		}
		frontier = next
	}
	return scores, nil
}

// EntityCues returns indexed entities explicitly present in a query. In addition
// to exact normalized-token matches, it matches entity_raw as a substring of the
// whole query so names such as "Alice Smith" survive natural-language wording.
func (s *EntryStore) EntityCues(ctx context.Context, query string) ([]string, error) {
	cues, _, err := s.EntitySignalsForQuery(ctx, query)
	return cues, err
}

// EntityEntryScores maps graph entity scores to the entries carrying those
// entities. A single entry can receive contributions from several entities.
func (s *EntryStore) EntityEntryScores(ctx context.Context, entityScores map[string]float64) (map[string]float64, error) {
	if s == nil || len(entityScores) == 0 {
		return nil, nil
	}
	entities := make([]string, 0, len(entityScores))
	for entity := range entityScores {
		entities = append(entities, entity)
	}
	sort.Slice(entities, func(i, j int) bool {
		if entityScores[entities[i]] != entityScores[entities[j]] {
			return entityScores[entities[i]] > entityScores[entities[j]]
		}
		return entities[i] < entities[j]
	})
	if len(entities) > entityEntryScoreEntityCap {
		slog.Warn("memory: entity entry score entities capped", "cap", entityEntryScoreEntityCap, "dropped", len(entities)-entityEntryScoreEntityCap)
		entities = entities[:entityEntryScoreEntityCap]
	}
	sort.Strings(entities)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(entities)), ",")
	args := make([]any, len(entities))
	for i, entity := range entities {
		args[i] = entity
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT entry_name, entity_norm FROM memory_entities WHERE entity_norm IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("memory: entity entry scores: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	out := make(map[string]float64)
	for rows.Next() {
		var name, entity string
		if err := rows.Scan(&name, &entity); err != nil {
			return nil, fmt.Errorf("memory: scan entity entry score: %w", err)
		}
		out[name] += entityScores[entity]
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: read entity entry scores: %w", err)
	}
	return out, nil
}
