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
	if r.SubmissionID == "" {
		r.SubmissionID = newID()
	}
	payload, err := encode(r)
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

// MaxPayload stays under the relay's 200 KiB limit on the request.
const MaxPayload = 180 << 10

// wire is what the relay reads: the issue body already rendered, and only
// the fields it checks or files. The structured parts are not sent again.
type wire struct {
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	Contact      string `json:"contact,omitempty"`
	Fingerprint  string `json:"fingerprint,omitempty"`
	InstallID    string `json:"installId"`
	SubmissionID string `json:"submissionId"`
	Version      string `json:"version"`
	OS           string `json:"os"`
	Body         string `json:"body"`
}

// encode builds the request and, measured after JSON escaping (backslashes
// and control characters can grow several times), cuts the logs until it
// fits: a report that is too large would be refused for good.
func encode(r Report) ([]byte, error) {
	// The relay scrubs the body once more, which would also blank an e-mail
	// the user typed as contact on purpose; the contact travels on its own
	// and the relay puts it back under the description.
	body := r
	body.Contact = ""
	body.Attachments = append([]Attachment(nil), r.Attachments...)
	w := wire{r.Kind, r.Title, r.Contact, r.Fingerprint, r.InstallID, r.SubmissionID, r.Version, r.OS, ""}
	for limit := maxAttachment; ; limit /= 2 {
		w.Body = body.Markdown()
		payload, err := json.Marshal(w)
		if err != nil || len(payload) <= MaxPayload {
			return payload, err
		}
		if limit < 200 {
			body.Attachments = nil
			body.Description = string([]rune(body.Description)[:len([]rune(body.Description))/2])
			continue
		}
		for i := range body.Attachments {
			body.Attachments[i].Body = r.Attachments[i].Cut(limit)
		}
	}
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
	var shape struct {
		OK *bool `json:"ok"`
	}
	if json.Unmarshal(raw, &shape) != nil || shape.OK == nil || resp.StatusCode >= 500 {
		// Not a verdict from the relay (a proxy or block page, a gateway
		// error, an outage): worth trying the other address.
		return Result{}, fmt.Errorf("%s: HTTP %d", endpoint, resp.StatusCode)
	}
	_ = json.Unmarshal(raw, &res)
	if !res.OK && res.Skipped == "" {
		// The relay looked at the report and refused it (bad fields, too
		// large): the other address runs the same relay and would too.
		if res.Error == "" {
			res.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return res, fmt.Errorf("%w: %s", ErrRejected, res.Error)
	}
	return res, nil
}
