package main

import (
	"context"
	"testing"

	"tolk/internal/engine"
)

// fakeEngine records the source of each call and answers like Google does on
// mixed text: on 自动检测 it says "Chinese" and hands the text back.
type fakeEngine struct {
	sources     []string
	unsupported map[string]bool
	detected    string // what auto detection reports; "zh-CN" when empty
	translates  bool   // auto detection translates instead of handing back
}

func (f *fakeEngine) Translate(_ context.Context, text, source, target string) (engine.Result, error) {
	f.sources = append(f.sources, source)
	if f.unsupported[source] {
		return engine.Result{}, &engine.UnsupportedError{}
	}
	if source == engine.Auto {
		d := f.detected
		if d == "" {
			d = "zh-CN"
		}
		if d == "-" {
			d = ""
		}
		if f.translates {
			return engine.Result{Text: "自动译文", Detected: d}, nil
		}
		return engine.Result{Text: text, Detected: d}, nil
	}
	return engine.Result{Text: "译文"}, nil
}

func TestRunEngineResolvesMixedText(t *testing.T) {
	long := "Thank you for your help. Attached is the service agreement between 徐州超拓弹簧制造有限公司 and our agency, signed and stamped."
	cases := []struct {
		name        string
		text        string
		unsupported map[string]bool
		detected    string
		translates  bool
		target      string
		wantSources []string
		wantText    string
		wantDet     string
	}{
		{"sure guess goes as the source", long, nil, "", false, "", []string{"英语"}, "译文", "en"},
		{"weak guess corrects a Chinese reading", "Meeting with 王总 at 3pm", nil, "", false, "", []string{engine.Auto, "英语"}, "译文", "en"},
		{"weak guess corrects a Chinese label", "Meeting with 王总 at 3pm", nil, "简体中文", true, "", []string{engine.Auto, "英语"}, "译文", "en"},
		{"weak guess corrects text handed back with no language", "Meeting with 王总 at 3pm", nil, "-", false, "", []string{engine.Auto, "英语"}, "译文", "en"},
		{"weak guess leaves a good translation alone", "Meeting with 王总 at 3pm", nil, "en", true, "", []string{engine.Auto}, "自动译文", "en"},
		{"chinese mixed text is left to the engine when it translates", "这个 PR 修复了 React 的 cleanup 问题", nil, "en", true, "英语", []string{engine.Auto}, "自动译文", "en"},
		{"chinese mixed text handed back is retried as chinese", "这个 PR 修复了 React 的 cleanup 问题", nil, "en", false, "英语", []string{engine.Auto, "简体中文"}, "译文", "zh-CN"},
		{"japanese already in the target is kept", "東京大学 ABC", nil, "ja", false, "日语", []string{engine.Auto}, "東京大学 ABC", "ja"},
		{"unsupported source falls back to detection", long, map[string]bool{"英语": true}, "", false, "", []string{"英语", engine.Auto}, long, "zh-CN"},
		{"plain text is left to the engine", "你好世界", nil, "", false, "", []string{engine.Auto}, "你好世界", "zh-CN"},
	}
	for _, c := range cases {
		f := &fakeEngine{unsupported: c.unsupported, detected: c.detected, translates: c.translates}
		target := c.target
		if target == "" {
			target = "简体中文"
		}
		r, err := runEngineContext(context.Background(), f, "apple", c.text, engine.Auto, target)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(f.sources) != len(c.wantSources) {
			t.Fatalf("%s: sources %v, want %v", c.name, f.sources, c.wantSources)
		}
		for i := range f.sources {
			if f.sources[i] != c.wantSources[i] {
				t.Fatalf("%s: sources %v, want %v", c.name, f.sources, c.wantSources)
			}
		}
		if r.Text != c.wantText || r.Detected != c.wantDet {
			t.Errorf("%s: got %q/%q, want %q/%q", c.name, r.Text, r.Detected, c.wantText, c.wantDet)
		}
	}
}

func TestRunEngineKeepsExplicitSource(t *testing.T) {
	f := &fakeEngine{}
	if _, err := runEngineContext(context.Background(), f, "apple", "Meeting with 王总", "法语", "简体中文"); err != nil {
		t.Fatal(err)
	}
	if len(f.sources) != 1 || f.sources[0] != "法语" {
		t.Fatalf("sources %v, want [法语]", f.sources)
	}
}

func TestRunEngineSkipsGuessEqualToTarget(t *testing.T) {
	f := &fakeEngine{}
	if _, err := runEngineContext(context.Background(), f, "apple", "我用 iPhone 拍的照片", engine.Auto, "简体中文"); err != nil {
		t.Fatal(err)
	}
	if len(f.sources) != 1 || f.sources[0] != engine.Auto {
		t.Fatalf("sources %v, want [自动检测]", f.sources)
	}
}
