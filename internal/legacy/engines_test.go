package legacy

import "testing"

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
