package main

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
)

// 048 DevFamilyIndex (data-model.md §3.1): machine-derived metadata over the
// frozen core172 payload — never modifies case payloads; provides the
// cross-split dedup baseline for holdout novelty review.

// v2 (2026-09-01): the v1 exact-string digest equality was too strict — the
// first real run showed 52/57 pairs unanimous on same_family with 0 semantic
// divergences, yet 40 of those 52 refused to join purely on slug wording
// ("go-defer-semantics-execution-order" vs "…-order"). v2 keeps the
// three-lane same_family unanimity as the semantic gate and requires the
// three topic slugs to share at least one dash-token pairwise (a cheap
// deterministic topic-alignment check) instead of byte equality.
const DevFamilyIndexAlgorithm = "dev-family-index-v2"
const DevFamilyIndexReviewPromptID = "dev-family-index-review-v1"

// DevFamily is one connected component of the family graph.
type DevFamily struct {
	FamilyID              string          `json:"family_id"`
	CaseIDs               []string        `json:"case_ids"`
	NormalizedPromptDigest string         `json:"normalized_prompt_digest"`
	LanguageMembers       map[string]bool `json:"language_members"`
	Taxonomy              []string        `json:"taxonomy"`
}

// DevFamilyIndex is the frozen index file.
type DevFamilyIndex struct {
	SchemaVersion     int               `json:"schema_version"`
	Algorithm         string            `json:"algorithm"`
	NormalizerVersion string            `json:"normalizer_version"`
	FamilyIDs         []string          `json:"family_ids"`
	Families          []DevFamily       `json:"families"`
	CaseToFamily      map[string]string `json:"case_to_family"`
	DerivationReceipt DerivationReceipt `json:"derivation_receipt"`
}

// DerivationReceipt binds the full derivation evidence.
type DerivationReceipt struct {
	Algorithm            string   `json:"algorithm"`
	NormalizerVersion    string   `json:"normalizer_version"`
	InputPayloadDigest   string   `json:"input_payload_digest"`
	CoreManifestDigest   string   `json:"core_manifest_digest"`
	ReviewPrompt         AuthoringPromptReceipt `json:"review_prompt"`
	Concurrency          int      `json:"concurrency"`
	ObservedMaxInFlight  int      `json:"observed_max_in_flight"`
	ObservedOverlap      bool     `json:"observed_overlap"`
	MirrorPairCount      int      `json:"mirror_pair_count"`
	PairDecisions        []MirrorPairDecision `json:"pair_decisions"`
	LaneProvenance       []ToolProvenance `json:"lane_provenance"`
	IndexDigest          string   `json:"index_digest"`
}

// MirrorPair is one cross-language mirror candidate.
type MirrorPair struct {
	A             string `json:"a"`
	B             string `json:"b"`
	Module        string `json:"module"`
	Category      string `json:"category"`
	RuleShapeDigest string `json:"rule_shape_digest"`
}

// MirrorPairDecision records the three-lane verdict for one pair.
type MirrorPairDecision struct {
	Pair         MirrorPair `json:"pair"`
	Joined       bool       `json:"joined"`
	SameFamily   []bool     `json:"same_family"`
	Digests      []string   `json:"canonical_family_digests"`
	TranscriptDigests []string `json:"transcript_digests"`
}

// MirrorReviewFunc is the injected three-lane review driver: production wires
// the real CLI lanes; tests inject deterministic lanes.
type MirrorReviewFunc func(pair MirrorPair, lane string) (sameFamily bool, canonicalDigest string, provenance ToolProvenance, transcriptDigest string, err error)

// familySlugTokensIntersect is the v2 topic-alignment check: each slug must be
// non-empty and every pair of slugs must share at least one dash-separated
// token. Byte equality is not required — lanes word the same topic at
// different granularity, and the v1 first run proved that wording noise, not
// semantic divergence, is what exact matching punishes.
func familySlugTokensIntersect(digests []string) bool {
	if len(digests) < 2 {
		return false
	}
	sets := make([]map[string]struct{}, len(digests))
	for i, d := range digests {
		s := map[string]struct{}{}
		for _, tok := range strings.Split(d, "-") {
			if tok != "" {
				s[tok] = struct{}{}
			}
		}
		if len(s) == 0 {
			return false
		}
		sets[i] = s
	}
	for i := 0; i < len(sets); i++ {
		for j := i + 1; j < len(sets); j++ {
			shared := false
			for tok := range sets[i] {
				if _, ok := sets[j][tok]; ok {
					shared = true
					break
				}
			}
			if !shared {
				return false
			}
		}
	}
	return true
}

// FamilyDerivationOptions carries the frozen concurrency and the review hook.
type FamilyDerivationOptions struct {
	Concurrency int
	Review      MirrorReviewFunc
	Lanes       []string // exactly three, e.g. claude|codex|opencode
	// Progress, when set, receives one line per completed lane review
	// (operationally useful for detached long runs; nil in tests).
	Progress func(done, total int, lane string, joined bool)
}

// MirrorCandidates discovers cross-language mirror candidates: equal
// module + category + machine-rule-shape digest across different languages.
func MirrorCandidates(core *CoreDatasetV2) []MirrorPair {
	type shape struct{ module, category, rule string }
	byShape := map[shape][]string{}
	ids := sortedKeys(core.Cases)
	for _, id := range ids {
		c := core.Cases[id]
		if c.Module == "regression" {
			continue // legacy cases have no mirror structure to review
		}
		shapeDigest, _ := RuleShapeDigest(c)
		byShape[shape{c.Module, c.Category, shapeDigest}] = append(byShape[shape{c.Module, c.Category, shapeDigest}], id)
	}
	var pairs []MirrorPair
	for s, members := range byShape {
		byLang := map[string][]string{}
		for _, id := range members {
			byLang[core.Cases[id].EffectiveLang()] = append(byLang[core.Cases[id].EffectiveLang()], id)
		}
		langs := sortedKeys(byLang)
		for i := 0; i < len(langs); i++ {
			for j := i + 1; j < len(langs); j++ {
				for _, a := range byLang[langs[i]] {
					for _, b := range byLang[langs[j]] {
						pairs = append(pairs, MirrorPair{A: a, B: b, Module: s.module, Category: s.category, RuleShapeDigest: s.rule})
					}
				}
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].A != pairs[j].A {
			return pairs[i].A < pairs[j].A
		}
		return pairs[i].B < pairs[j].B
	})
	return pairs
}

// RuleShapeDigest is the canonical digest of a case's machine-rule shape
// (module machine fields excluding the human-only observable).
func RuleShapeDigest(c *TriggerCaseV2) (string, error) {
	return NormalizedLabelDigest(c.Module, "", "", c.Category, c.Expect)
}

// NormalizeCaseText applies the frozen normalizer: LF, ASCII lowercase,
// Unicode whitespace folding to single spaces.
func NormalizeCaseText(s string) string {
	lower := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		lower[i] = b
	}
	var out bytes.Buffer
	lastSpace := false
	for _, r := range string(lower) {
		if unicode.IsSpace(r) {
			if !lastSpace {
				out.WriteRune(' ')
			}
			lastSpace = true
			continue
		}
		lastSpace = false
		out.WriteRune(r)
	}
	return strings.TrimSuffix(out.String(), " ")
}

// caseSemanticDigest is the normalized digest over prompt/user turns, seed
// name/content and machine rules — exact-duplicate detection input.
func caseSemanticDigest(c *TriggerCaseV2) (string, error) {
	proj := struct {
		Prompt      string          `json:"prompt"`
		Turns       []BlindTurn     `json:"turns,omitempty"`
		Seeds       []BlindSeedMemory `json:"seeds,omitempty"`
		MachineRule string          `json:"machine_rule"`
	}{}
	if c.Prompt != nil {
		proj.Prompt = NormalizeCaseText(*c.Prompt)
	}
	for _, t := range c.Turns {
		proj.Turns = append(proj.Turns, BlindTurn{Session: t.Session, Role: t.Role, Content: NormalizeCaseText(t.Content), SetupOnly: t.SetupOnly})
	}
	for _, s := range c.SeedMemories {
		proj.Seeds = append(proj.Seeds, BlindSeedMemory{Name: NormalizeCaseText(s.Name), Content: NormalizeCaseText(s.Content), EventDate: s.EventDate})
	}
	shape, err := RuleShapeDigest(c)
	if err != nil {
		return "", err
	}
	proj.MachineRule = shape
	return CanonicalSHA256(proj)
}

// DeriveDevFamilyIndex runs the frozen derivation over the core dataset:
// exact-digest joins, three-lane unanimous mirror joins (bounded worker pool),
// stable component IDs and a complete derivation receipt.
func DeriveDevFamilyIndex(core *CoreDatasetV2, reviewPrompt AuthoringPromptReceipt, opts FamilyDerivationOptions, inputPayloadDigest, coreManifestDigest string) (*DevFamilyIndex, error) {
	if opts.Concurrency < 1 {
		return nil, fmt.Errorf("family-index derivation requires concurrency >= 1, got %d", opts.Concurrency)
	}
	if len(opts.Lanes) != 3 {
		return nil, fmt.Errorf("family-index mirror review requires exactly three lanes")
	}
	ids := sortedKeys(core.Cases)

	// Phase 1: exact normalized digest joins.
	exactGroups := map[string][]string{}
	semDigests := map[string]string{}
	for _, id := range ids {
		c := core.Cases[id]
		d, err := caseSemanticDigest(c)
		if err != nil {
			return nil, err
		}
		semDigests[id] = d
		exactGroups[d] = append(exactGroups[d], id)
	}

	// Phase 2: mirror candidates across different exact groups.
	mirrorPairs := MirrorCandidates(core)
	decisions := make([]MirrorPairDecision, len(mirrorPairs))
	laneProv := map[string]ToolProvenance{}
	var (
		maxInFlight int32
		current     int32
		overlap     int32
		completed   int
		wg          sync.WaitGroup
		sem         chan struct{}
		mu          sync.Mutex
		reviewErr   error
	)
	sem = make(chan struct{}, opts.Concurrency)
	runOne := func(idx int, pair MirrorPair) {
		defer wg.Done()
		defer atomic.AddInt32(&current, -1)
		now := atomic.AddInt32(&current, 1)
		for {
			prev := atomic.LoadInt32(&maxInFlight)
			if now <= prev || atomic.CompareAndSwapInt32(&maxInFlight, prev, now) {
				break
			}
		}
		if now > 1 {
			atomic.StoreInt32(&overlap, 1)
		}
		dec := MirrorPairDecision{Pair: pair, SameFamily: []bool{}, Digests: []string{}, TranscriptDigests: []string{}}
		allSame := true
		var digests []string
		for _, lane := range opts.Lanes {
			same, cd, prov, tdigest, err := opts.Review(pair, lane)
			if err != nil {
				mu.Lock()
				if reviewErr == nil {
					reviewErr = fmt.Errorf("mirror review %s/%s lane %s: %w", pair.A, pair.B, lane, err)
				}
				mu.Unlock()
				break
			}
			mu.Lock()
			if _, ok := laneProv[lane]; !ok {
				laneProv[lane] = prov
			}
			mu.Unlock()
			dec.SameFamily = append(dec.SameFamily, same)
			dec.Digests = append(dec.Digests, cd)
			dec.TranscriptDigests = append(dec.TranscriptDigests, tdigest)
			if !same {
				allSame = false
			}
			digests = append(digests, cd)
		}
		// Join only when all three lanes say same_family and their topic
		// slugs align on the same subject (pairwise dash-token overlap).
		if allSame && len(dec.Digests) == 3 && familySlugTokensIntersect(dec.Digests) {
			dec.Joined = true
		}
		mu.Lock()
		decisions[idx] = dec
		completed++
		if opts.Progress != nil {
			opts.Progress(completed, len(mirrorPairs), "pair", dec.Joined)
		}
		mu.Unlock()
		<-sem
	}
	for i, p := range mirrorPairs {
		wg.Add(1)
		sem <- struct{}{}
		go runOne(i, p)
	}
	wg.Wait()
	if reviewErr != nil {
		return nil, reviewErr
	}

	// Phase 3: union-find over exact joins + unanimous mirror joins.
	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		if parent[x] == "" {
			parent[x] = x
		}
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b string) { parent[find(a)] = find(b) }
	for _, g := range exactGroups {
		for i := 1; i < len(g); i++ {
			union(g[0], g[i])
		}
	}
	for _, dec := range decisions {
		if dec.Joined {
			union(dec.Pair.A, dec.Pair.B)
		}
	}

	// Phase 4: components → stable family IDs (sorted member IDs digest).
	comps := map[string][]string{}
	for _, id := range ids {
		root := find(id)
		comps[root] = append(comps[root], id)
	}
	index := &DevFamilyIndex{
		SchemaVersion: 1, Algorithm: DevFamilyIndexAlgorithm, NormalizerVersion: "family-normalizer-v1",
		CaseToFamily: map[string]string{},
	}
	for _, root := range sortedKeys(comps) {
		members := comps[root]
		sort.Strings(members)
		famID := "fam-" + sha256Hex([]byte(strings.Join(members, "\x00")))[:24]
		fam := DevFamily{FamilyID: famID, CaseIDs: members}
		if d, ok := semDigests[members[0]]; ok {
			fam.NormalizedPromptDigest = d
		}
		langMembers := map[string]bool{}
		tax := map[string]bool{}
		for _, id := range members {
			c := core.Cases[id]
			langMembers[c.EffectiveLang()] = true
			tax[c.Category] = true
			index.CaseToFamily[id] = famID
		}
		for l := range langMembers {
			fam.LanguageMembers = map[string]bool{}
			_ = l
			break
		}
		fam.LanguageMembers = langMembers
		for t := range tax {
			fam.Taxonomy = append(fam.Taxonomy, t)
		}
		sort.Strings(fam.Taxonomy)
		index.Families = append(index.Families, fam)
		index.FamilyIDs = append(index.FamilyIDs, famID)
	}

	// Receipt.
	receipt := DerivationReceipt{
		Algorithm: DevFamilyIndexAlgorithm, NormalizerVersion: "family-normalizer-v1",
		InputPayloadDigest: inputPayloadDigest, CoreManifestDigest: coreManifestDigest,
		ReviewPrompt: reviewPrompt,
		Concurrency: opts.Concurrency,
		ObservedMaxInFlight: int(atomic.LoadInt32(&maxInFlight)),
		ObservedOverlap: atomic.LoadInt32(&overlap) == 1,
		MirrorPairCount: len(mirrorPairs),
		PairDecisions: decisions,
	}
	for _, lane := range opts.Lanes {
		if p, ok := laneProv[lane]; ok {
			receipt.LaneProvenance = append(receipt.LaneProvenance, p)
		}
	}
	saved := receipt.IndexDigest
	receipt.IndexDigest = ""
	d, err := CanonicalSHA256(receipt)
	if err != nil {
		return nil, err
	}
	receipt.IndexDigest = d
	_ = saved
	index.DerivationReceipt = receipt
	return index, nil
}

// SaveDevFamilyIndex writes the frozen index canonically (immutable output:
// refuses an existing file).
func SaveDevFamilyIndex(path string, idx *DevFamilyIndex) error {
	b, err := CanonicalJSON(idx)
	if err != nil {
		return err
	}
	return WriteFrozenFile(path, b)
}
