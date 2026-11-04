package legacy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAPINameDefaultAndSavedSelection(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"new user", nil, "microsoft"},
		{"empty config", map[string]any{}, "microsoft"},
		{"missing engine", map[string]any{"translation": map[string]any{}}, "microsoft"},
		{"empty engine", map[string]any{"translation": map[string]any{"api": ""}}, "microsoft"},
	}
	for _, api := range []string{"google", "microsoft", "deepl", "openai_compat", "achord_builtin", "apple"} {
		tests = append(tests, struct {
			name string
			cfg  map[string]any
			want string
		}{"saved " + api, map[string]any{"translation": map[string]any{"api": api}}, api})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := APIName(tt.cfg); got != tt.want {
				t.Fatalf("APIName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExistingImplicitEngineAndNewConfig(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.json")
	if err := os.WriteFile(old, []byte(`{"translation":{"target_lang":"英语"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(old)
	if err != nil || APIName(cfg) != "google" {
		t.Fatalf("legacy: %v %v", cfg, err)
	}
	fresh := filepath.Join(dir, "new.json")
	if err = Update(fresh, func(m map[string]any) { Sub(m, "translation")["target_lang"] = "英语" }); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(fresh)
	if err != nil || APIName(cfg) != "microsoft" {
		t.Fatalf("fresh: %v %v", cfg, err)
	}
}
