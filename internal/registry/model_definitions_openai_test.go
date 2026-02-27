package registry

import "testing"

func TestGetOpenAIModels_IncludesCodex53Aliases(t *testing.T) {
	models := GetOpenAIModels()
	if len(models) == 0 {
		t.Fatalf("GetOpenAIModels() returned no models")
	}

	want := []string{
		"gpt-5.3-codex",
		"gpt-5.3-codex-spark",
	}
	for _, id := range want {
		if !containsModelID(models, id) {
			t.Fatalf("missing model %q in GetOpenAIModels()", id)
		}
	}
}

func containsModelID(models []*ModelInfo, want string) bool {
	for _, m := range models {
		if m != nil && m.ID == want {
			return true
		}
	}
	return false
}

