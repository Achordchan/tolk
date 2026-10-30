package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const systemPrompt = "你是专业翻译。只返回翻译结果，不要添加任何解释或额外内容。注意：输入里可能包含特殊标记 [[DZFYQ_NL_*]]，它代表换行。你必须原样保留该标记（不要翻译、不要删除、不要新增），并保持其相对位置不变。"

// OpenAICompat talks to any chat-completions compatible service.
type OpenAICompat struct {
	Client  *http.Client
	BaseURL string
	Model   string
	APIKey  string
}

// NewOpenAICompat validates the settings the same way the Python app did.
func NewOpenAICompat(baseURL, model, apiKey string) (*OpenAICompat, error) {
	baseURL, model, apiKey = strings.TrimSpace(baseURL), strings.TrimSpace(model), strings.TrimSpace(apiKey)
	switch {
	case baseURL == "":
		return nil, fmt.Errorf("未设置模型URL")
	case model == "":
		return nil, fmt.Errorf("未设置模型名称")
	case apiKey == "":
		return nil, fmt.Errorf("未设置API密钥")
	}
	return &OpenAICompat{Client: defaultClient(), BaseURL: strings.TrimRight(baseURL, "/"), Model: model, APIKey: apiKey}, nil
}

// Warm connects to the API host.
func (o *OpenAICompat) Warm(ctx context.Context) error { return warmHost(ctx, o.Client, o.BaseURL) }

func (o *OpenAICompat) Translate(ctx context.Context, text, source, target string) (Result, error) {
	if strings.TrimSpace(text) == "" {
		return Result{}, ErrEmpty
	}
	prompt := fmt.Sprintf("请将下面文本翻译成%s。只返回翻译结果，不要添加解释。\n\n%s", target, text)
	if source != Auto && source != "" {
		prompt = fmt.Sprintf("请将下面%s文本翻译成%s。只返回翻译结果，不要添加解释。\n\n%s", source, target, text)
	}
	body, _ := json.Marshal(map[string]any{
		"model": o.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.2,
		"max_tokens":  2000,
		"top_p":       0.95,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.APIKey)
	resp, err := o.Client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("无法连接到 AI 翻译服务，请检查网络连接、代理或模型 URL: %s", o.scrub(err.Error()))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("AI翻译失败: HTTP %d %s", resp.StatusCode, o.scrub(strings.TrimSpace(string(raw))))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return Result{}, fmt.Errorf("AI未返回翻译结果")
	}
	if out.Choices[0].FinishReason == "length" {
		// Showing half a translation as a success would silently drop the rest.
		return Result{}, fmt.Errorf("译文过长被截断，请分段翻译")
	}
	return Result{Text: strings.TrimSpace(out.Choices[0].Message.Content)}, nil
}

// scrub keeps the API key out of error messages.
func (o *OpenAICompat) scrub(s string) string {
	if o.APIKey != "" {
		s = strings.ReplaceAll(s, o.APIKey, "***")
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
