package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Endpoints are tried in order. report.achord.cn is reachable from mainland
// China, where workers.dev often is not; workers.dev is the spare for
// everyone else.
var Endpoints = []string{
	"https://report.achord.cn/v1/report",
	"https://tolk-report.achordchan.workers.dev/v1/report",
}

// Repo is where issues go, and what the new-issue link falls back to.
const Repo = "Achordchan/tolk"

// Result is the relay's answer.
type Result struct {
	OK      bool   `json:"ok"`
	Number  int    `json:"number"`
	URL     string `json:"url"`
	Merged  bool   `json:"merged"`  // added to an existing issue for the same error
	Skipped string `json:"skipped"` // the relay chose not to file it (rate limit…)
	Error   string `json:"error"`
}

// ErrRejected means the relay answered and refused the report; trying the
// next endpoint would not help.
var ErrRejected = errors.New("report rejected")

// Send delivers r (call Clean first) to the first relay that answers.
func Send(ctx context.Context, client *http.Client, r Report) (Result, error) {
	payload, err := json.Marshal(struct {
		Report
		Body string `json:"body"`
	}{r, r.Markdown()})
	if err != nil {
		return Result{}, err
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	var last error
	for _, ep := range Endpoints {
		res, err := post(ctx, client, ep, payload)
		if err == nil || errors.Is(err, ErrRejected) {
			return res, err
		}
		last = err
		if ctx.Err() != nil {
			break
		}
	}
	return Result{}, last
}

func post(ctx context.Context, client *http.Client, endpoint string, payload []byte) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tolk-Report", "1")
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var res Result
	jerr := json.Unmarshal(raw, &res)
	switch {
	case jerr != nil:
		// Not the relay talking (a proxy or block page, an outage): worth
		// trying the other address.
		return Result{}, fmt.Errorf("%s: HTTP %d", endpoint, resp.StatusCode)
	case !res.OK && res.Skipped == "":
		if res.Error == "" {
			res.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return res, fmt.Errorf("%w: %s", ErrRejected, res.Error)
	}
	return res, nil
}
