package memory

import "testing"

func TestCJK_Classification(t *testing.T) {
	tests := []struct {
		name string
		r    rune
		want bool
	}{
		{"CJK basic", 0x4E00, true},
		{"CJK end", 0x9FFF, true},
		{"Ext A start", 0x3400, true},
		{"Ext B char", 0x20000, true},
		{"Ext C char", 0x2A700, true},
		{"Ext D char", 0x2B740, true},
		{"Ext E char", 0x2B820, true},
		{"Ext F char", 0x2CEB0, true},
		{"Compatibility Ideographs", 0xF900, true},
		{"Radicals Supplement", 0x2E80, true},
		{"CJK Symbols and Punctuation", 0x3000, true},
		{"Hiragana", 0x3041, true},
		{"Katakana", 0x30A1, true},
		{"Katakana Phonetic Extensions", 0x31F0, true},
		{"Hangul", 0xAC00, true},
		{"Jamo", 0x1100, true},
		{"Hangul Compatibility Jamo", 0x3130, true},
		{"Halfwidth Katakana", 0xFF65, true},
		{"Halfwidth Katakana end", 0xFF9F, true},
		{"ASCII a", 'a', false},
		{"digit 5", '5', false},
		{"Latin ext", 0x00E9, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCJK(tt.r); got != tt.want {
				t.Errorf("isCJK(%U) = %v, want %v", tt.r, got, tt.want)
			}
		})
	}
}

func TestTokenizer_Basic(t *testing.T) {
	runs := tokenize("hello world")
	if len(runs) != 3 {
		t.Fatalf("expected 3 runs, got %d", len(runs))
	}
	if runs[0].Kind != "ascii" || runs[0].Text != "hello" {
		t.Errorf("run[0]: %+v", runs[0])
	}
	if runs[1].Kind != "ws" {
		t.Errorf("run[1]: %+v", runs[1])
	}
	if runs[2].Kind != "ascii" || runs[2].Text != "world" {
		t.Errorf("run[2]: %+v", runs[2])
	}
}

func TestTrigrams(t *testing.T) {
	got := trigrams("数据库迁移")
	want := []string{"数据库", "据库迁", "库迁移"}
	if len(got) != len(want) {
		t.Fatalf("trigrams: got %v, want %v", got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("trigram[%d]: got %q, want %q", i, g, want[i])
		}
	}
}

func TestBuildPlan_PureASCII(t *testing.T) {
	expr, ok := buildPlan("hello world")
	if !ok {
		t.Fatal("expected ok=true for pure ASCII")
	}
	if expr != "hello AND world" {
		t.Errorf("got %q", expr)
	}
}

func TestBuildPlan_LongCJK(t *testing.T) {
	expr, ok := buildPlan("数据库迁移")
	if !ok {
		t.Fatal("expected ok=true for long CJK")
	}
	if expr == "" {
		t.Error("expected non-empty expression")
	}
}

func TestBuildPlan_ShortCJK_Fallback(t *testing.T) {
	_, ok := buildPlan("迁移")
	if ok {
		t.Error("short CJK should trigger fallback")
	}
}

func TestBuildPlan_MixedASCIIShortCJK_Fallback(t *testing.T) {
	_, ok := buildPlan("sqlite 迁移")
	if ok {
		t.Error("mixed ASCII + short CJK should fallback")
	}
}

func TestBuildPlan_FTS5OperatorInjection(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"AND operator", "hello AND world"},
		{"OR operator", "hello OR world"},
		{"NOT operator", "hello NOT world"},
		{"star wildcard", "hello*"},
		{"NEAR operator", "hello NEAR world"},
		{"parentheses", "hello (world)"},
		{"unbalanced quote", `hello "world`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, ok := buildPlan(tt.query)
			if !ok {
				return
			}
			for _, op := range []string{" OR ", " NOT ", " NEAR "} {
				if containsString(expr, op) {
					t.Errorf("FTS expression contains raw operator %q: %q", op, expr)
				}
			}
		})
	}
}

func containsString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
