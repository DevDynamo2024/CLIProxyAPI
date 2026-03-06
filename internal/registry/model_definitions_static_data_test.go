package registry

import "testing"

func TestLookupStaticModelInfo_GPT54(t *testing.T) {
	model := LookupStaticModelInfo("gpt-5.4")
	if model == nil {
		t.Fatal("expected gpt-5.4 model definition")
	}
	if model.ID != "gpt-5.4" {
		t.Fatalf("expected model ID gpt-5.4, got %q", model.ID)
	}
	if model.Type != "openai" {
		t.Fatalf("expected openai model type, got %q", model.Type)
	}
	if model.Thinking == nil {
		t.Fatal("expected thinking metadata for gpt-5.4")
	}
}
