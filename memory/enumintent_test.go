package memory

import "testing"

func TestEnumerationIntentRecognizesEnglishEnumerationAndComparison(t *testing.T) {
	tests := []string{
		"What things did Alice do during the trip?",
		"What activities did Alice do during the trip?",
		"Which activities did Bob mention?",
		"How many times did she visit the museum?",
		"How often did they meet?",
		"List all the places she visited.",
		"List all places she visited.",
		"Can you list your hobbies?",
		"list them",
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
		"多少国家？",
		"多少本书？",
		"多少次？",
		"你收集了多少？",
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

func TestEnumerationIntentDoesNotMistakePricesForEnumeration(t *testing.T) {
	for _, query := range []string{
		"What is the list price of the camera?",
		"多少钱？",
		"多少价格？",
		"多少费用？",
		"多少金额？",
	} {
		if got := ParseEnumerationIntent(query); got.IsEnumeration {
			t.Errorf("ParseEnumerationIntent(%q) = %+v, want non-enumeration", query, got)
		}
	}
}
