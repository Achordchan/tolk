package dlx

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

var processMu sync.Mutex
var command *exec.Cmd
var done chan struct{}
var endpoint, token, runningPath string
var localClient = &http.Client{Timeout: 25 * time.Second, Transport: &http.Transport{Proxy: nil}}

type Result struct {
	Code       int    `json:"code"`
	Data       string `json:"data"`
	Message    string `json:"message"`
	SourceLang string `json:"source_lang"`
}

func Stop() { processMu.Lock(); defer processMu.Unlock(); stopLocked() }
func stopLocked() {
	if command != nil {
		_ = command.Process.Kill()
		<-done
		command = nil
	}
	endpoint = ""
}
func startLocked(path string) (string, error) {
	if command != nil && runningPath != path {
		stopLocked()
	}
	if command != nil {
		select {
		case <-done:
			command = nil
		default:
			return endpoint, nil
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return "", err
	}
	token = hex.EncodeToString(secret)
	cmd := exec.Command(path, "-ip", "127.0.0.1", "-port", fmt.Sprint(port), "-token", token)
	prepare(cmd)
	if err = cmd.Start(); err != nil {
		return "", err
	}
	command = cmd
	runningPath = path
	done = make(chan struct{})
	finished := done
	go func() { _ = cmd.Wait(); close(finished) }()
	endpoint = fmt.Sprintf("http://127.0.0.1:%d", port)
	// Bounded readiness probes are local process startup, not remote task polling.
	deadline := time.Now().Add(5 * time.Second)
	probe := &http.Client{Timeout: 200 * time.Millisecond, Transport: &http.Transport{Proxy: nil}}
	for time.Now().Before(deadline) {
		select {
		case <-done:
			command = nil
			return "", fmt.Errorf("插件进程已退出")
		default:
		}
		resp, e := probe.Get(endpoint + "/")
		if e == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return endpoint, nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	stopLocked()
	return "", fmt.Errorf("插件启动超时")
}
func Translate(ctx context.Context, source, target, text string) (Result, error) {
	s := Status()
	if s.Installed == "" || !validVersion(s.Installed) {
		return Result{}, fmt.Errorf("请在设置 → 翻译服务中安装 Achord 翻译插件")
	}
	processMu.Lock()
	url, err := startLocked(executable(s.Installed))
	secret := token
	if err != nil {
		processMu.Unlock()
		return Result{}, err
	}
	// Hold the process lease until the request finishes so installation cannot kill it.
	defer processMu.Unlock()
	b, _ := json.Marshal(map[string]string{"text": text, "source_lang": source, "target_lang": target})
	req, _ := http.NewRequestWithContext(ctx, "POST", url+"/translate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := localClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	var r Result
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return r, err
	}
	if r.Code == 0 {
		r.Code = resp.StatusCode
	}
	return r, nil
}
