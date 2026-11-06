//go:build windows

package screenshot

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var allowSetForeground = windows.NewLazySystemDLL("user32.dll").NewProc("AllowSetForegroundWindow")

// Warm is the first line a workspace writes: its PowerShell is up, the
// controls are loaded and it is waiting for Start.
type Warm struct {
	Warm bool `json:"warm"`
	// Types is where the controls came from: "cache" (the compiled DLL),
	// "compiled" (compiled now and kept) or "memory" (compiled, not kept).
	Types   string `json:"types"`
	TypesMs int    `json:"typesMs"`
}

// Start is what the workspace needs when the user asks for a screenshot.
// It is sent then, not at launch, because the process may have been waiting
// a long time and the settings may have changed meanwhile.
type Start struct {
	Target    string   `json:"target"`
	Theme     string   `json:"theme"`
	Languages []string `json:"languages"`
}

// Process is one workspace: launched ahead of time, it captures the screen
// only once Begin is called, then serves one selection (and the pins made
// from it) until it exits.
type Process struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	out     *bufio.Reader // the only reader of stdout, so nothing it buffered is lost
	stderr  syncBuilder
	script  string
	job     windows.Handle
	handle  windows.Handle
	started time.Time

	warm     chan struct{} // closed once the warm line arrived
	failed   chan struct{} // closed if the process ended or spoke before being warm
	info     Warm
	warmErr  error
	waitOnce sync.Once
	waitErr  error
}

// Launch starts a workspace process. dll is where the compiled controls
// are kept ("" compiles them in memory each time).
func Launch(dll string) (*Process, error) {
	file, err := os.CreateTemp("", "tolk-screenshot-*.ps1")
	if err != nil {
		return nil, err
	}
	p := &Process{script: file.Name(), warm: make(chan struct{}), failed: make(chan struct{})}
	fail := func(err error) (*Process, error) {
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
			_ = p.cmd.Wait()
		}
		p.closeHandles()
		return nil, err
	}
	_, err = file.WriteString(Script())
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fail(err)
	}
	p.cmd = exec.Command("powershell.exe", "-NoProfile", "-STA", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", p.script)
	// Hide the console, not the WinForms selection surface.
	p.cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	p.cmd.Stderr = &p.stderr
	p.cmd.Env = append(os.Environ(), "TOLK_SHOT_DLL="+dll)
	output, err := p.cmd.StdoutPipe()
	if err != nil {
		return fail(err)
	}
	p.out = bufio.NewReaderSize(output, 64<<10)
	if p.in, err = p.cmd.StdinPipe(); err != nil {
		return fail(err)
	}
	// Closing the job (including on a main-process crash) terminates the workspace.
	if p.job, err = windows.CreateJobObject(nil, nil); err != nil {
		return fail(err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(p.job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return fail(err)
	}
	p.started = time.Now()
	if err = p.cmd.Start(); err != nil {
		return fail(err)
	}
	if p.handle, err = windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(p.cmd.Process.Pid)); err != nil {
		return fail(err)
	}
	if err = windows.AssignProcessToJobObject(p.job, p.handle); err != nil {
		return fail(err)
	}
	go p.readWarm()
	return p, nil
}

func (p *Process) readWarm() {
	line, err := p.out.ReadBytes('\n')
	if err == nil {
		if jerr := json.Unmarshal(line, &p.info); jerr != nil || !p.info.Warm {
			err = fmt.Errorf("截图工作区启动时返回了无法识别的内容")
		}
	}
	if err != nil {
		if errors.Is(err, io.EOF) {
			err = errors.New("截图工作区在就绪前退出")
		}
		p.warmErr = err
		close(p.failed)
		return
	}
	close(p.warm)
}

// WaitWarm waits until the process is ready to start, it fails, or timeout
// passes. Several goroutines may wait at once.
func (p *Process) WaitWarm(timeout time.Duration) (Warm, error) {
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-p.warm:
		return p.info, nil
	case <-p.failed:
		return Warm{}, p.warmErr
	case <-t.C:
		return Warm{}, fmt.Errorf("截图工作区 %d 秒内没有就绪", int(timeout.Seconds()))
	}
}

// Begin captures the screen and opens the selection. Call it once, after
// WaitWarm succeeded; then hand Output and Input to Serve.
func (p *Process) Begin(s Start) error {
	if s.Languages == nil {
		s.Languages = []string{}
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	// The process may have been started before the user put Tolk in front;
	// let the selection window take the foreground when Tolk is allowed to.
	allowSetForeground.Call(uintptr(p.cmd.Process.Pid))
	_, err = p.in.Write(append(b, '\n'))
	return err
}

func (p *Process) Output() io.Reader  { return p.out }
func (p *Process) Input() io.Writer   { return p.in }
func (p *Process) Started() time.Time { return p.started }
func (p *Process) Stderr() string     { return p.stderr.String() }
func (p *Process) Pid() int           { return p.cmd.Process.Pid }
func (p *Process) Kill()              { _ = p.cmd.Process.Kill() } // safe after Wait, unlike a raw handle
func (p *Process) closeHandles() {
	if p.handle != 0 {
		windows.CloseHandle(p.handle)
		p.handle = 0
	}
	if p.job != 0 {
		windows.CloseHandle(p.job)
		p.job = 0
	}
	if p.script != "" {
		_ = os.Remove(p.script)
	}
}

// Alive reports whether the process is still running: a spare that was
// killed while it waited must not be handed a screenshot.
func (p *Process) Alive() bool {
	s, err := windows.WaitForSingleObject(p.handle, 0)
	return err == nil && s == uint32(windows.WAIT_TIMEOUT)
}

// Wait waits for the process to exit and releases everything it held. It
// may be called more than once; the first call does the work. Only call it
// once nothing reads Output any more.
func (p *Process) Wait() error {
	p.waitOnce.Do(func() {
		p.waitErr = p.cmd.Wait()
		p.closeHandles()
	})
	return p.waitErr
}

// syncBuilder collects stderr, which exec writes from its own goroutine
// while a failing warm-up may already be reading it.
type syncBuilder struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuilder) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(b)
}
func (s *syncBuilder) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
