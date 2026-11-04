package screenshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestBlockTranslation(t *testing.T) {
	var out bytes.Buffer
	err := Serve(strings.NewReader(`{"id":7,"source":"自动检测","target":"英语","blocks":["一","二"]}`+"\n"), &out, func(_ context.Context, text, source, target string) (string, error) {
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
	err := Serve(strings.NewReader(`{"id":8,"blocks":["one","two"]}`+"\n"), &out, func(context.Context, string, string, string) (string, error) {
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
	err := Serve(strings.NewReader("{\"closed\":true}\n"), &out, func(context.Context, string, string, string) (string, error) {
		t.Fatal("close translated")
		return "", nil
	}, func() { closed = true })
	if err != nil || !closed || out.Len() != 0 {
		t.Fatalf("close=%v err=%v out=%s", closed, err, out.String())
	}
}

func TestReadyDoesNotCloseOrTranslate(t *testing.T) {
	var out bytes.Buffer
	ready := false
	err := Serve(strings.NewReader("{\"ready\":true}\n"), &out, func(context.Context, string, string, string) (string, error) {
		t.Fatal("ready translated")
		return "", nil
	}, func() { t.Fatal("ready closed") }, func() { ready = true })
	if err != nil || !ready || out.Len() != 0 {
		t.Fatalf("ready=%v error=%v", ready, err)
	}
}

func TestCloseCancelsActiveTranslation(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	started := make(chan struct{})
	closed := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Serve(reader, io.Discard, func(ctx context.Context, _, _, _ string) (string, error) {
			close(started)
			<-ctx.Done()
			return "", ctx.Err()
		}, func() { close(closed) })
	}()
	fmt.Fprintln(writer, `{"id":1,"blocks":["text"]}`)
	<-started
	fmt.Fprintln(writer, `{"closed":true}`)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close blocked behind translation")
	}
	writer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("translation not cancelled")
	}
}
