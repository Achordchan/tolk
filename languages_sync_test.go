package main

import (
	"os"
	"regexp"
	"testing"

	"tolk/internal/engine"
	"tolk/internal/inputassist"
)

// The Swift helper keeps its own copy of the language list; this keeps it
// from drifting away from the Go table the pickers are built from.
func TestHelperLanguageTableMatchesGo(t *testing.T) {
	src, err := os.ReadFile("macos/helper/Sources/TolkHelperCore/Languages.swift")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range regexp.MustCompile(`TolkLanguage\(label: "([^"]+)", code: "([^"]+)"\)`).FindAllStringSubmatch(string(src), -1) {
		got = append(got, m[1])
		// Go sends the helper codes as well as labels (apple.status).
		if code, ok := inputassist.CodeOf(m[1]); !ok || code != m[2] {
			t.Errorf("%s: helper code %q, Go %q", m[1], m[2], code)
		}
	}
	want := append([]string{"自动检测"}, engine.Languages...)
	if len(got) != len(want) {
		t.Fatalf("helper has %d languages, Go has %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("language %d: helper %q, Go %q", i, got[i], want[i])
		}
	}
}
