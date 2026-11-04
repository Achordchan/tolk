package dlx

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestReleaseValidation(t *testing.T) {
	old := client
	defer func() { client = old }()
	for _, tt := range []struct {
		name, tag, url, digest string
		wantError              bool
	}{
		{"valid", "v1.3.1", "https://github.com/OwO-Network/DLX/releases/download/v1.3.1/" + assetName(), "sha256:" + strings.Repeat("a", 64), false},
		{"path traversal", "../../bad", "https://example.com", "", true},
		{"untrusted download", "v1.3.1", "https://example.com/plugin", "sha256:" + strings.Repeat("a", 64), true},
		{"missing digest", "v1.3.1", "https://github.com/OwO-Network/DLX/releases/download/v1.3.1/" + assetName(), "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"tag_name":"` + tt.tag + `","assets":[{"name":"` + assetName() + `","browser_download_url":"` + tt.url + `","digest":"` + tt.digest + `"}]}`))}, nil
			})}
			_, _, err := fetch(context.Background())
			if (err != nil) != tt.wantError {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
func TestMissingPlugin(t *testing.T) {
	Configure(t.TempDir(), func(State) {})
	if Status().Installed != "" {
		t.Fatal("unexpected installed version")
	}
	if _, err := Translate(context.Background(), "EN", "ZH", "hello"); err == nil {
		t.Fatal("missing plugin must not translate")
	}
}
