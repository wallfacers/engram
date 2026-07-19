package memory

import "testing"

func TestEnumerationIntentRecognizesEnglishEnumerationAndComparison(t *testing.T) {
	tests := []string{
		"What things did Alice do during the trip?",
		"Which activities did Bob mention?",
		"How many times did she visit the museum?",
		"How often did they meet?",
		"List all the places she visited.",
		"Compare the two travel plans.",
		"Which option happened more often?",
	}
	for _, query := range tests {
		if got := ParseEnumerationIntent(query); !got.IsEnumeration {
			t.Errorf("ParseEnumerationIntent(%q) = %+v, want enumeration", query, got)
		}
	}
}

func TestEnumerationIntentRecognizesChineseEnumeration(t *testing.T) {
	for _, query := range []string{
		"她去了哪些地方？",
		"他参加了几次活动？",
		"他们提到了多少次旅行？",
		"每次会议发生了什么？",
	} {
		if got := ParseEnumerationIntent(query); !got.IsEnumeration {
			t.Errorf("ParseEnumerationIntent(%q) = %+v, want enumeration", query, got)
		}
	}
}

func TestEnumerationIntentDoesNotMisclassifySingleFactQuestions(t *testing.T) {
	for _, query := range []string{
		"What was Alice's favorite color?",
		"When did Bob move to Oslo?",
		"Where did she go yesterday?",
		"Which city does he live in?",
	} {
		if got := ParseEnumerationIntent(query); got.IsEnumeration {
			t.Errorf("ParseEnumerationIntent(%q) = %+v, want non-enumeration", query, got)
		}
	}
}
