package screenshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestBlockTranslation(t *testing.T) {
	var out bytes.Buffer
	err := Serve(strings.NewReader(`{"id":7,"source":"自动检测","target":"英语","blocks":["一","二"]}`+"\n"), &out, func(text, source, target string) (string, error) {
		if source != "自动检测" || target != "英语" {
			t.Fatal("language lost")
		}
		return "translated:" + text, nil
	})
	var res Response
	_ = json.Unmarshal(out.Bytes(), &res)
	if err != nil || res.ID != 7 || len(res.Texts) != 2 || res.Texts[1] != "translated:二" {
		t.Fatalf("%+v %v", res, err)
	}
}
func TestFailureDoesNotExportPartialTranslation(t *testing.T) {
	var out bytes.Buffer
	n := 0
	err := Serve(strings.NewReader(`{"id":8,"blocks":["one","two"]}`+"\n"), &out, func(string, string, string) (string, error) {
		n++
		if n == 2 {
			return "", errors.New("offline")
		}
		return "ok", nil
	})
	var res Response
	_ = json.Unmarshal(out.Bytes(), &res)
	if err != nil || res.Error != "offline" || len(res.Texts) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestWorkspaceCloseReleasesCaptureWithoutTranslation(t *testing.T) {
	var out bytes.Buffer
	closed := false
	err := Serve(strings.NewReader("{\"closed\":true}\n"), &out, func(string, string, string) (string, error) { t.Fatal("close translated"); return "", nil }, func() { closed = true })
	if err != nil || !closed || out.Len() != 0 {
		t.Fatalf("close=%v err=%v out=%s", closed, err, out.String())
	}
}

func TestReadyDoesNotCloseOrTranslate(t *testing.T) {
	var out bytes.Buffer
	ready := false
	err := Serve(strings.NewReader("{\"ready\":true}\n"), &out, func(string, string, string) (string, error) { t.Fatal("ready translated"); return "", nil }, func() { t.Fatal("ready closed") }, func() { ready = true })
	if err != nil || !ready || out.Len() != 0 {
		t.Fatalf("ready=%v error=%v", ready, err)
	}
}
