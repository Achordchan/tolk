package main

import (
	"os"
	"regexp"
	"testing"
)

func TestVersionMatchesConfig(t *testing.T) {
	b, err := os.ReadFile("mygo.config.ts")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`const version = "([^"]+)"`).FindSubmatch(b)
	if m == nil || string(m[1]) != Version {
		t.Fatalf("mygo.config.ts version %q != Version %q", m, Version)
	}
}
