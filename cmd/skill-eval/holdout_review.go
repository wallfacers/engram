package main

// T028/T033 review half — closed-schema validation of the serialized review
// envelope. The reviewer-visible envelope is a recursively closed allowlist:
// unknown keys, duplicates, aliases, nested extensions, and any field from
// which the authoring lane or its proposed labels can be derived are schema
// failures. Nothing is accepted then stripped (contracts/dataset-protocol.md §5).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// jsonNode is one node of the recursive closed allowlist.
type jsonNode struct {
	kind     byte // 'o' object, 'a' array, 'v' scalar
	keys     map[string]*jsonNode
	required []string
	items    *jsonNode
}

func obj(keys map[string]*jsonNode, required ...string) *jsonNode {
	return &jsonNode{kind: 'o', keys: keys, required: required}
}

func arr(items *jsonNode) *jsonNode { return &jsonNode{kind: 'a', items: items} }

func scalar() *jsonNode { return &jsonNode{kind: 'v'} }

// reviewEnvelopeSchema is the frozen recursive allowlist for the serialized
// ReviewEnvelope (and the BlindCandidateV1 / FamilySummaryPayload subtrees).
func reviewEnvelopeSchema() *jsonNode {
	blindTurn := obj(map[string]*jsonNode{
		"session": scalar(), "role": scalar(), "content": scalar(), "setup_only": scalar(),
	}, "session", "role", "content")
	blindSeed := obj(map[string]*jsonNode{
		"name": scalar(), "content": scalar(), "event_date": scalar(),
	}, "name", "content")
	blindFile := obj(map[string]*jsonNode{
		"path": scalar(), "content": scalar(), "sha256": scalar(),
	}, "path", "content", "sha256")
	candidate := obj(map[string]*jsonNode{
		"schema_version":  scalar(),
		"prompt":          scalar(),
		"turns":           arr(blindTurn),
		"seed_memories":   arr(blindSeed),
		"workspace_files": arr(blindFile),
	}, "schema_version", "seed_memories", "workspace_files")
	summaryEntry := obj(map[string]*jsonNode{
		"family_id":               scalar(),
		"language_members":        arr(scalar()),
		"blind_semantic_payloads": arr(scalar()),
		"entry_digest":            scalar(),
	}, "family_id", "language_members", "blind_semantic_payloads", "entry_digest")
	summary := obj(map[string]*jsonNode{
		"schema_version":      scalar(),
		"scope":               scalar(),
		"revision":            scalar(),
		"projection_version":  scalar(),
		"source_state_digest": scalar(),
		"source_family_count": scalar(),
		"entries":             arr(summaryEntry),
		"entries_root_digest": scalar(),
		"payload_digest":      scalar(),
	}, "schema_version", "scope", "revision", "projection_version",
		"source_state_digest", "source_family_count", "entries",
		"entries_root_digest", "payload_digest")
	return obj(map[string]*jsonNode{
		"candidate":                          candidate,
		"blind_candidate_digest":             scalar(),
		"review_prompt_digest":               scalar(),
		"dev_family_summary":                 summary,
		"dev_family_summary_digest":          scalar(),
		"accepted_holdout_family_summary":    summary,
		"accepted_holdout_family_summary_digest": scalar(),
		"accepted_holdout_family_revision":      scalar(),
		"envelope_digest":                        scalar(),
	}, "candidate", "blind_candidate_digest", "review_prompt_digest",
		"dev_family_summary", "dev_family_summary_digest", "envelope_digest")
}

// ValidateReviewEnvelopeJSON fails closed on a serialized review envelope:
// duplicate keys anywhere, unknown/aliased/nested-extra keys, missing
// required fields, and a candidate carrying both prompt and turns (or
// neither) are all schema failures.
func ValidateReviewEnvelopeJSON(raw []byte) error {
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("envelope is not a JSON object: %w", err)
	}
	if err := validateNode(root, reviewEnvelopeSchema(), "$"); err != nil {
		return err
	}
	// The candidate exposes exactly one of prompt|turns.
	var cand struct {
		Prompt *json.RawMessage `json:"prompt"`
		Turns  *json.RawMessage `json:"turns"`
	}
	cb, ok := root["candidate"]
	if !ok {
		return fmt.Errorf("envelope candidate missing")
	}
	if err := json.Unmarshal(cb, &cand); err != nil {
		return fmt.Errorf("candidate malformed: %w", err)
	}
	if (cand.Prompt == nil) == (cand.Turns == nil) {
		return fmt.Errorf("candidate must carry exactly one of prompt|turns")
	}
	return nil
}

func validateNode(m map[string]json.RawMessage, n *jsonNode, path string) error {
	for k := range m {
		child, ok := n.keys[k]
		if !ok {
			return fmt.Errorf("%s: unknown field %q (closed allowlist)", path, k)
		}
		v := m[k]
		switch child.kind {
		case 'o':
			var sub map[string]json.RawMessage
			if err := json.Unmarshal(v, &sub); err != nil {
				return fmt.Errorf("%s.%s: malformed object: %w", path, k, err)
			}
			if sub == nil {
				return fmt.Errorf("%s.%s: null object not allowed", path, k)
			}
			if err := validateNode(sub, child, path+"."+k); err != nil {
				return err
			}
		case 'a':
			var items []json.RawMessage
			if err := json.Unmarshal(v, &items); err != nil {
				return fmt.Errorf("%s.%s: malformed array: %w", path, k, err)
			}
			if items == nil {
				continue // null array treated as absent (omitempty projections)
			}
			if child.items != nil && child.items.kind == 'v' {
				continue // scalar-element arrays carry no closed object schema
			}
			for i, it := range items {
				var sub map[string]json.RawMessage
				if err := json.Unmarshal(it, &sub); err != nil {
					return fmt.Errorf("%s.%s[%d]: malformed element: %w", path, k, i, err)
				}
				if err := validateNode(sub, child.items, fmt.Sprintf("%s.%s[%d]", path, k, i)); err != nil {
					return err
				}
			}
		}
	}
	for _, r := range n.required {
		if _, ok := m[r]; !ok {
			return fmt.Errorf("%s: required field %q missing", path, r)
		}
	}
	return nil
}

// rejectDuplicateJSONKeys walks the token stream and fails on any object with
// a repeated key — encoding/json would silently keep the last one, which would
// let a smuggled field hide behind a canonical one. Object frames alternate
// key/value positions; array frames treat every token as an element.
func rejectDuplicateJSONKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	type frame struct {
		keys      map[string]bool // nil ⇒ array frame
		expectKey bool
	}
	var stack []*frame
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if len(stack) == 0 {
			d, ok := tok.(json.Delim)
			if !ok || d != '{' {
				return fmt.Errorf("envelope root is not a JSON object")
			}
			stack = append(stack, &frame{keys: map[string]bool{}, expectKey: true})
			continue
		}
		top := stack[len(stack)-1]
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{':
				stack = append(stack, &frame{keys: map[string]bool{}, expectKey: true})
			case '[':
				stack = append(stack, &frame{keys: nil})
			case '}', ']':
				stack = stack[:len(stack)-1]
				if len(stack) > 0 && stack[len(stack)-1].keys != nil {
					stack[len(stack)-1].expectKey = true
				}
			}
		case string:
			if top.keys != nil {
				if top.expectKey {
					if top.keys[t] {
						return fmt.Errorf("duplicate JSON key %q", t)
					}
					top.keys[t] = true
					top.expectKey = false
				} else {
					top.expectKey = true
				}
			}
		default:
			if top.keys != nil && !top.expectKey {
				top.expectKey = true
			}
		}
	}
	if len(stack) != 0 {
		return fmt.Errorf("unbalanced JSON")
	}
	return nil
}

// NearestFamilyReferenced enforces that a reviewer's non-empty novelty
// nearest-family reference names an entry of one of the payloads it actually
// received (dev summary or accepted-holdout summary).
func NearestFamilyReferenced(rec ReviewRecord, dev, accepted *FamilySummaryPayload) error {
	if rec.NearestFamilyID == nil || *rec.NearestFamilyID == "" {
		if rec.Novel {
			return nil
		}
		return fmt.Errorf("non-novel verdict without a nearest_family_id")
	}
	id := *rec.NearestFamilyID
	for _, p := range []*FamilySummaryPayload{dev, accepted} {
		if p == nil {
			continue
		}
		for _, e := range p.Entries {
			if e.FamilyID == id {
				return nil
			}
		}
	}
	return fmt.Errorf("nearest_family_id %q absent from both materialized payloads", id)
}

// sortedStringSliceEq is a small helper for deterministic comparisons.
func sortedStringSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := append([]string{}, a...), append([]string{}, b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
