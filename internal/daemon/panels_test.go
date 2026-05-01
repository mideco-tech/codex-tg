package daemon

import (
	"reflect"
	"testing"
)

func TestExtractChoiceOptionsSkipsNilFieldsAndFallsBack(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"options": []any{
			map[string]any{"text": "Use text"},
			map[string]any{"value": "Use value"},
			map[string]any{"label": "Use label"},
			map[string]any{"label": "<nil>", "text": "Use text after nil label"},
			map[string]any{},
			"<nil>",
		},
	}

	got := extractChoiceOptions(payload)
	want := []string{"Use text", "Use value", "Use label", "Use text after nil label"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractChoiceOptions() = %#v, want %#v", got, want)
	}
}

func TestExtractChoiceOptionsUsesQuestionTextOrValueFallback(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"questions": []any{
			map[string]any{
				"options": []any{
					map[string]any{"text": "Question text"},
					map[string]any{"value": "Question value"},
					map[string]any{"label": "Question label"},
					map[string]any{"label": "<nil>", "value": "Question value after nil label"},
					map[string]any{},
				},
			},
		},
	}

	got := extractChoiceOptions(payload)
	want := []string{"Question text", "Question value", "Question label", "Question value after nil label"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractChoiceOptions() = %#v, want %#v", got, want)
	}
}
