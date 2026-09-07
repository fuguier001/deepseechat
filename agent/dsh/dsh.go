// Package dsh implements a native DeepSeek Harness agent adapter for DSC.
//
// Each turn spawns `dsh --profile headless` with the accumulated conversation
// transcript (kept in a local per-session file), then emits the answer as a
// single result event. This makes the WeChat bot a real DeepSeek Harness
// citizen: same profiles, same skills, same brain as the interactive web app.
package dsh

import (
	"context"
	"fmt"
	"os"
	"bytes"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fuguier001/deepseechat/core"
)

const transcriptLimit = 24 // messages kept for context

func init() {
	core.RegisterAgent("dsh", New)
}

type Agent struct {
	mu      sync.Mutex
	workDir string
	cliBin  string
	profile string
	home    string
}

func New(opts map[string]any) (core.Agent, error) {
	workDir, _ := opts["work_dir"].(string)
	if workDir == "" {
		if wd, err := os.Getwd(); err == nil {
			workDir = wd
		} else {
			workDir = "."
		}
	}
	cliBin, _ := opts["cli_path"].(string)
	if cliBin == "" {
		cliBin = "dsh"
	}
	cliBin = resolveDsh(cliBin)
	profile, _ := opts["profile"].(string)
	if profile == "" {
		profile = "headless"
	}
	return &Agent{workDir: workDir, cliBin: cliBin, profile: profile}, nil
}

// resolveDsh finds the dsh launcher when a bare "dsh" is not on PATH:
// npm global prefix first, then the newest npx cache entry. Absolute
// cli_path values are returned untouched.
func resolveDsh(bin string) string {
	if bin != "dsh" {
		return bin
	}
	if _, err := exec.LookPath(bin); err == nil {
		return bin
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return bin
	}
	candidates := []string{
		filepath.Join(home, ".npm-global", "bin", "dsh"),
		filepath.Join(home, ".local", "bin", "dsh"),
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".npm", "_npx", "*", "node_modules", ".bin", "dsh"))
	candidates = append(candidates, matches...)
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			slog.Info("dsh: resolved launcher", "path", c)
			return c
		}
	}
	return bin
}

func (a *Agent) Name() string { return "dsh" }

func (a *Agent) StartSession(ctx context.Context, sessionID string) (core.AgentSession, error) {
	dataDir := ".dsc"
	if home, err := os.UserHomeDir(); err == nil {
		dataDir = filepath.Join(home, dataDir)
	}
	dir := filepath.Join(dataDir, "dsh-sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("dsh: create session dir: %w", err)
	}
	s := &dshSession{
		agent:   a,
		ctx:     ctx,
		events:  make(chan core.Event, 16),
		sid:     sessionID,
		transFn: filepath.Join(dir, sanitize(sessionID)+".md"),
	}
	go func() { <-ctx.Done(); s.alive = false }()
	s.alive = true
	return s, nil
}

func (a *Agent) ListSessions(ctx context.Context) ([]core.AgentSessionInfo, error) {
	return nil, nil
}

func (a *Agent) Stop() error { return nil }

type dshSession struct {
	agent  *Agent
	ctx    context.Context
	events chan core.Event
	sid    string
	transFn string
	mu     sync.Mutex
	alive  bool
}

func (s *dshSession) Send(prompt string, _ []core.ImageAttachment, _ []core.FileAttachment) error {
	go s.turn(prompt)
	return nil
}

func (s *dshSession) RespondPermission(string, core.PermissionResult) error { return nil }

func (s *dshSession) Events() <-chan core.Event   { return s.events }
func (s *dshSession) CurrentSessionID() string    { return s.sid }
func (s *dshSession) Alive() bool                 { s.mu.Lock(); defer s.mu.Unlock(); return s.alive }
func (s *dshSession) Close() error                { s.mu.Lock(); s.alive = false; s.mu.Unlock(); return nil }

func (s *dshSession) turn(userMsg string) {
	defer func() { recover() }()
	full := s.buildPrompt(userMsg)
	cmd := exec.CommandContext(s.ctx, s.agent.cliBin, "--profile", s.agent.profile, full)
	cmd.Dir = s.agent.workDir
	cmd.Env = append(os.Environ(), "HOME="+filepath.Join(homeOrEmpty(), ".dsc", "dsh-home"), "DSH_HOME="+filepath.Join(homeOrEmpty(), ".dsc", "dsh-home", ".dsh"))
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out // final assistant message only; reasoning streams to stderr
	cmd.Stderr = &errBuf
	err := cmd.Run()
	answer := cleanOutput(out.String())
	s.record("用户", userMsg)
	if err != nil && answer == "" {
		s.emit(core.Event{Type: core.EventError, Error: fmt.Errorf("dsh: %v: %s", err, trunc(errBuf.Bytes(), 200))})
		return
	}
	s.record("DSC", answer)
	s.emit(core.Event{Type: core.EventResult, SessionID: s.sid, Content: answer, Done: true})
}

func (s *dshSession) buildPrompt(userMsg string) string {
	var sb strings.Builder
	if b, err := os.ReadFile(s.transFn); err == nil && len(b) > 0 {
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n---\n")
		if len(lines) > transcriptLimit {
			lines = lines[len(lines)-transcriptLimit:]
		}
		sb.WriteString("以下是之前的对话记录（供上下文参考）：\n")
		sb.WriteString(strings.Join(lines, "\n---\n"))
		sb.WriteString("\n\n")
	}
	sb.WriteString("用户最新消息：")
	sb.WriteString(userMsg)
	sb.WriteString("\n\n请遵守微信回复规范：全中文、简洁、能一句话说清绝不分段、任何情况不超过1000字、不提及任何内部实现。")
	return sb.String()
}

func (s *dshSession) record(role, text string) {
	f, err := os.OpenFile(s.transFn, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "**%s**：%s\n---\n", role, strings.ReplaceAll(text, "\n", " "))
}

func (s *dshSession) emit(e core.Event) {
	select {
	case s.events <- e:
	case <-s.ctx.Done():
	}
}

// cleanOutput strips the headless launcher's reasoning preamble lines.
func cleanOutput(out string) string {
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		t := strings.TrimSpace(l)
		if t == "dsh: reasoning:" || strings.HasPrefix(t, "dsh: ") {
			continue
		}
		keep = append(keep, l)
	}
	s := strings.TrimSpace(strings.Join(keep, "\n"))
	// drop a single leading reasoning sentence-block heuristically kept by launcher
	return s
}

func sanitize(id string) string {
	r := strings.Map(func(c rune) rune {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			return c
		default:
			return '-'
		}
	}, id)
	if len(r) > 80 {
		r = r[len(r)-80:]
	}
	if r == "" {
		return "session"
	}
	return r
}

func trunc(b []byte, n int) string {
	s := string(b)
	if len(s) > n {
		s = s[:n]
	}
	return s
}

func homeOrEmpty() string { h, _ := os.UserHomeDir(); return h }
