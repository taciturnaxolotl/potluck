package registry

import (
	"testing"
)

func TestResolveModel_WithPrefix(t *testing.T) {
	r := New([]ProviderConfig{
		{ID: "pioneer", Type: TypeOpenAICompat, Name: "Pioneer", BaseURL: "https://api.pioneer.ai"},
		{ID: "openrouter", Type: TypeOpenRouter, Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1"},
	})

	tests := []struct {
		model          string
		wantProvider   string
		wantUpstream   string
	}{
		{"openrouter/claude-sonnet-4", "openrouter", "claude-sonnet-4"},
		{"pioneer/gpt-4o", "pioneer", "gpt-4o"},
		{"claude-sonnet-4", "pioneer", "claude-sonnet-4"}, // bare → default pioneer
		{"unknown/model", "pioneer", "unknown/model"},     // unknown prefix → default
	}

	for _, tt := range tests {
		gotProvider, gotUpstream := r.ResolveModel(tt.model)
		if gotProvider != tt.wantProvider {
			t.Errorf("ResolveModel(%q) provider = %q, want %q", tt.model, gotProvider, tt.wantProvider)
		}
		if gotUpstream != tt.wantUpstream {
			t.Errorf("ResolveModel(%q) upstream = %q, want %q", tt.model, gotUpstream, tt.wantUpstream)
		}
	}
}

func TestGet(t *testing.T) {
	r := New([]ProviderConfig{
		{ID: "pioneer", Type: TypeOpenAICompat, Name: "Pioneer", BaseURL: "https://api.pioneer.ai"},
	})

	cfg, ok := r.Get("pioneer")
	if !ok {
		t.Fatal("expected to find pioneer")
	}
	if cfg.Name != "Pioneer" {
		t.Errorf("expected name Pioneer, got %s", cfg.Name)
	}

	_, ok = r.Get("nonexistent")
	if ok {
		t.Error("expected nonexistent to return false")
	}
}

func TestList(t *testing.T) {
	r := New([]ProviderConfig{
		{ID: "pioneer", Type: TypeOpenAICompat, Name: "Pioneer", BaseURL: "https://api.pioneer.ai"},
		{ID: "free", Type: "openai_compat", Name: "Free", BaseURL: "http://localhost:11434"},
	})

	list := r.List()
	if len(list) != 2 {
		t.Errorf("expected 2 providers, got %d", len(list))
	}
}

func TestToFantasy_OpenAICompat(t *testing.T) {
	r := New([]ProviderConfig{
		{ID: "pioneer", Type: TypeOpenAICompat, Name: "Pioneer", BaseURL: "https://api.pioneer.ai"},
	})

	fp, err := r.ToFantasy("pioneer", "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp == nil {
		t.Fatal("expected non-nil provider")
	}
	if fp.Name() != "Pioneer" {
		t.Errorf("expected name Pioneer, got %s", fp.Name())
	}
}

func TestToFantasy_Free(t *testing.T) {
	r := New([]ProviderConfig{
		{ID: "free", Type: "openai_compat", Name: "Free", BaseURL: "http://localhost:11434"},
	})

	fp, err := r.ToFantasy("free", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestToFantasy_Unknown(t *testing.T) {
	r := New(nil)
	_, err := r.ToFantasy("nonexistent", "key")
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}
