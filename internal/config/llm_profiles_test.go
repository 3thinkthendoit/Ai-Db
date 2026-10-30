package config

import "testing"

func TestNormalizeLLMProfilesMigratesLegacy(t *testing.T) {
	cfg := &AppConfig{
		LLM: LLMConfig{
			BaseURL:       "https://example.com/v1",
			APIKey:        "sk-test",
			Model:         "demo-model",
			AllowWrites:   true,
			MaxRows:       100,
			MaxAgentSteps: 5,
		},
	}
	NormalizeLLMProfiles(cfg)
	if len(cfg.LLMProfiles) != 1 {
		t.Fatalf("want 1 profile, got %d", len(cfg.LLMProfiles))
	}
	p := cfg.LLMProfiles[0]
	if p.ID != DefaultLLMProfileID || p.Name != DefaultLLMProfileName {
		t.Fatalf("unexpected default profile: %+v", p)
	}
	if p.BaseURL != "https://example.com/v1" || p.APIKey != "sk-test" || p.Model != "demo-model" {
		t.Fatalf("profile fields not copied: %+v", p)
	}
	if cfg.ActiveLLMID != DefaultLLMProfileID {
		t.Fatalf("active id = %q", cfg.ActiveLLMID)
	}
	if cfg.LLM.AllowWrites != true || cfg.LLM.MaxRows != 100 || cfg.LLM.MaxAgentSteps != 5 {
		t.Fatalf("safety knobs lost: %+v", cfg.LLM)
	}
	if cfg.LLM.APIKey != "sk-test" || cfg.LLM.Model != "demo-model" {
		t.Fatalf("sync failed: %+v", cfg.LLM)
	}
}

func TestSyncActiveLLMSwitchesEndpoint(t *testing.T) {
	cfg := &AppConfig{
		LLMProfiles: []LLMProfile{
			{ID: "a", Name: "A", BaseURL: "https://a/v1", APIKey: "ka", Model: "ma"},
			{ID: "b", Name: "B", BaseURL: "https://b/v1", APIKey: "kb", Model: "mb"},
		},
		ActiveLLMID: "a",
		LLM:         LLMConfig{AllowWrites: true, MaxRows: 50, MaxAgentSteps: 3},
	}
	SyncActiveLLM(cfg)
	if cfg.LLM.BaseURL != "https://a/v1" || cfg.LLM.APIKey != "ka" || cfg.LLM.Model != "ma" {
		t.Fatalf("active a not synced: %+v", cfg.LLM)
	}
	cfg.ActiveLLMID = "b"
	SyncActiveLLM(cfg)
	if cfg.LLM.BaseURL != "https://b/v1" || cfg.LLM.APIKey != "kb" || cfg.LLM.Model != "mb" {
		t.Fatalf("active b not synced: %+v", cfg.LLM)
	}
	if !cfg.LLM.AllowWrites || cfg.LLM.MaxRows != 50 {
		t.Fatalf("safety overwritten: %+v", cfg.LLM)
	}
}

func TestNormalizeRepairsMissingActive(t *testing.T) {
	cfg := &AppConfig{
		LLMProfiles: []LLMProfile{
			{ID: "x", Name: "X", BaseURL: "https://x/v1", Model: "mx"},
		},
		ActiveLLMID: "gone",
		LLM:         DefaultLLM(),
	}
	NormalizeLLMProfiles(cfg)
	if cfg.ActiveLLMID != "x" {
		t.Fatalf("want active x, got %q", cfg.ActiveLLMID)
	}
	if cfg.LLM.Model != "mx" {
		t.Fatalf("model = %q", cfg.LLM.Model)
	}
}
