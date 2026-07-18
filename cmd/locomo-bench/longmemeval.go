package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type longMemEvalItem struct {
	ID           string
	Question     string
	Answer       string
	QuestionType string
	Category     string
	Adversarial  bool
	Conversation conversation
}

type longMemEvalRecord struct {
	QuestionID       string          `json:"question_id"`
	Question         string          `json:"question"`
	Answer           json.RawMessage `json:"answer"`
	QuestionType     string          `json:"question_type"`
	QuestionDate     string          `json:"question_date"`
	HaystackSessions json.RawMessage `json:"haystack_sessions"`
}

type longMemEvalMessage struct {
	Role    string          `json:"role"`
	Speaker string          `json:"speaker"`
	Content json.RawMessage `json:"content"`
	Text    string          `json:"text"`
	Date    string          `json:"date"`
}

type longMemEvalSessionObject struct {
	Date     string               `json:"date"`
	Messages []longMemEvalMessage `json:"messages"`
	Turns    []longMemEvalMessage `json:"turns"`
}

// loadLongMemEval loads the LongMemEval_S question format and maps each of its
// seven target question types to a stable report bucket. It does not call any
// model; the returned conversation can enter the existing ingest pipeline.
func loadLongMemEval(path string) ([]longMemEvalItem, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-supplied benchmark path
	if err != nil {
		return nil, fmt.Errorf("read LongMemEval dataset: %w", err)
	}
	records, err := decodeLongMemEvalRecords(raw)
	if err != nil {
		return nil, err
	}
	items := make([]longMemEvalItem, 0, len(records))
	for i, record := range records {
		questionType := strings.ToLower(strings.TrimSpace(record.QuestionType))
		if !isLongMemEvalType(questionType) {
			return nil, fmt.Errorf("question %q has unsupported question_type %q", record.QuestionID, record.QuestionType)
		}
		id := strings.TrimSpace(record.QuestionID)
		if id == "" {
			id = fmt.Sprintf("lme-%d", i)
		}
		conversation, err := parseLongMemEvalConversation(record.HaystackSessions, record.QuestionDate, i)
		if err != nil {
			return nil, fmt.Errorf("question %q: %w", id, err)
		}
		items = append(items, longMemEvalItem{
			ID:           id,
			Question:     record.Question,
			Answer:       rawAnswerText(record.Answer),
			QuestionType: questionType,
			Category:     questionType,
			Adversarial:  questionType == "abstention",
			Conversation: conversation,
		})
	}
	return items, nil
}

func decodeLongMemEvalRecords(raw []byte) ([]longMemEvalRecord, error) {
	var records []longMemEvalRecord
	if err := json.Unmarshal(raw, &records); err == nil {
		return records, nil
	}
	var envelope struct {
		Data      []longMemEvalRecord `json:"data"`
		Questions []longMemEvalRecord `json:"questions"`
		Items     []longMemEvalRecord `json:"items"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("parse LongMemEval JSON: %w", err)
	}
	for _, group := range [][]longMemEvalRecord{envelope.Data, envelope.Questions, envelope.Items} {
		if len(group) > 0 {
			return group, nil
		}
	}
	return nil, fmt.Errorf("parse LongMemEval JSON: expected an array or data/questions/items envelope")
}

func isLongMemEvalType(questionType string) bool {
	switch questionType {
	case "single-session-user", "single-session-assistant", "multi-session",
		"temporal-reasoning", "knowledge-update", "abstention", "preference":
		return true
	default:
		return false
	}
}

func parseLongMemEvalConversation(raw json.RawMessage, fallbackDate string, id int) (conversation, error) {
	var groups []json.RawMessage
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &groups); err != nil {
			return conversation{}, fmt.Errorf("haystack_sessions: %w", err)
		}
	}
	date := parseFlexibleDate(fallbackDate)
	con := conversation{ID: id}
	for index, group := range groups {
		messages, groupDate, err := parseLongMemEvalSession(group)
		if err != nil {
			return conversation{}, fmt.Errorf("session %d: %w", index, err)
		}
		if groupDate.IsZero() {
			groupDate = date
		}
		if groupDate.IsZero() {
			for _, message := range messages {
				if parsed := parseFlexibleDate(message.Date); !parsed.IsZero() {
					groupDate = parsed
					break
				}
			}
		}
		s := session{Index: index + 1, Date: groupDate}
		for _, message := range messages {
			content := lmeMessageText(message.Content)
			if strings.TrimSpace(content) == "" {
				content = message.Text
			}
			if strings.TrimSpace(content) == "" {
				continue
			}
			speaker := message.Speaker
			if speaker == "" {
				speaker = message.Role
			}
			s.Turns = append(s.Turns, turn{Speaker: speaker, Text: content})
		}
		if len(s.Turns) > 0 {
			con.Sessions = append(con.Sessions, s)
		}
	}
	return con, nil
}

func parseLongMemEvalSession(raw json.RawMessage) ([]longMemEvalMessage, time.Time, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, time.Time{}, nil
	}
	if trimmed[0] == '[' {
		var messages []longMemEvalMessage
		if err := json.Unmarshal(trimmed, &messages); err != nil {
			return nil, time.Time{}, err
		}
		return messages, time.Time{}, nil
	}
	var object longMemEvalSessionObject
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return nil, time.Time{}, err
	}
	messages := object.Messages
	if len(messages) == 0 {
		messages = object.Turns
	}
	return messages, parseFlexibleDate(object.Date), nil
}

func lmeMessageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, block := range blocks {
			if block.Type == "" || block.Type == "text" {
				parts = append(parts, block.Text)
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

func rawAnswerText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return strings.Trim(string(raw), `"`)
}

func parseFlexibleDate(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func loadBenchmarkDataset(path, format string) ([]conversation, error) {
	if format == "locomo" {
		return loadDataset(path)
	}
	items, err := loadLongMemEval(path)
	if err != nil {
		return nil, err
	}
	convs := make([]conversation, 0, len(items))
	for i, item := range items {
		answer, err := json.Marshal(item.Answer)
		if err != nil {
			return nil, fmt.Errorf("encode answer %q: %w", item.ID, err)
		}
		category := longMemEvalCategoryID(item.Category)
		qa := locomoQA{
			Question:     item.Question,
			Answer:       answer,
			Category:     category,
			QuestionID:   item.ID,
			QuestionType: item.QuestionType,
			CategoryName: item.Category,
			Adversarial:  item.Adversarial,
		}
		item.Conversation.ID = i
		item.Conversation.QA = []locomoQA{qa}
		convs = append(convs, item.Conversation)
	}
	return convs, nil
}

func longMemEvalCategoryID(category string) int {
	switch category {
	case "single-session-user":
		return 6
	case "single-session-assistant":
		return 7
	case "multi-session":
		return 8
	case "temporal-reasoning":
		return 9
	case "knowledge-update":
		return 10
	case "abstention":
		return 11
	case "preference":
		return 12
	default:
		return 0
	}
}

func normalizeCompareArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--compare" && i+2 < len(args) {
			out = append(out, "--compare="+args[i+1]+","+args[i+2])
			i += 2
			continue
		}
		out = append(out, args[i])
	}
	return out
}

func parseCompareSpec(spec string) ([2]string, error) {
	parts := strings.Split(spec, ",")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return [2]string{}, fmt.Errorf("--compare requires two directories: --compare DIR_A DIR_B")
	}
	return [2]string{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])}, nil
}
