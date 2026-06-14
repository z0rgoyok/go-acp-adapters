package claude

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type TmuxSession struct {
	Name       string
	WorkingDir string
	ClaudePath string
	Env        map[string]string
}

func (s *TmuxSession) Launch(ctx context.Context, claudeArgs []string) error {
	return run(ctx, "tmux", s.launchArgs(claudeArgs)...)
}

func (s *TmuxSession) launchArgs(claudeArgs []string) []string {
	args := []string{
		"new-session", "-d",
		"-s", s.Name,
		"-c", s.WorkingDir,
		"-x", "300",
		"-y", "100",
	}
	keys := make([]string, 0, len(s.Env))
	for key := range s.Env {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		value := s.Env[key]
		args = append(args, "-e", key+"="+value)
	}
	args = append(args, "--", s.ClaudePath)
	args = append(args, claudeArgs...)
	return args
}

func (s *TmuxSession) WaitReady(ctx context.Context) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		deadline, _ = ctx.Deadline()
	}

	var last string
	stable := 0
	for time.Now().Before(deadline) {
		out, err := output(ctx, "tmux", "capture-pane", "-p", "-t", s.Name)
		if err == nil {
			ready := strings.Contains(out, "❯") &&
				!strings.Contains(out, "Thinking") &&
				!strings.Contains(out, "Running") &&
				!strings.Contains(out, "⠋") &&
				!strings.Contains(out, "⠙") &&
				!strings.Contains(out, "⠹")
			if ready && out == last {
				stable++
				if stable >= 3 {
					return nil
				}
			} else {
				stable = 0
			}
			last = out
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("claude prompt did not become ready: %q", trimForError(last))
}

func (s *TmuxSession) PastePrompt(ctx context.Context, sessionID string, prompt string) error {
	file, err := os.CreateTemp("", "claude-acp-adapter-paste-*.txt")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)

	if _, err := file.WriteString(prompt); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	buffer := "claude-acp-adapter-" + sessionID
	if err := run(ctx, "tmux", "load-buffer", "-b", buffer, name); err != nil {
		return err
	}
	defer run(context.Background(), "tmux", "delete-buffer", "-b", buffer)

	if err := run(ctx, "tmux", "paste-buffer", "-p", "-b", buffer, "-t", s.Name); err != nil {
		return err
	}
	return run(ctx, "tmux", "send-keys", "-t", s.Name, "Enter")
}

func (s *TmuxSession) IsAlive(ctx context.Context) bool {
	return run(ctx, "tmux", "has-session", "-t", s.Name) == nil
}

func (s *TmuxSession) Interrupt(ctx context.Context) error {
	return run(ctx, "tmux", "send-keys", "-t", s.Name, "C-c")
}

func (s *TmuxSession) Kill(_ context.Context) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	var lastErr error
	if err := s.Interrupt(cleanupCtx); err != nil {
		lastErr = err
	}
	sleepOrDone(cleanupCtx, 300*time.Millisecond)
	if err := run(cleanupCtx, "tmux", "send-keys", "-t", s.Name, "/exit", "Enter"); err != nil {
		lastErr = err
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !s.IsAlive(cleanupCtx) {
			return lastErr
		}
		if !sleepOrDone(cleanupCtx, 200*time.Millisecond) {
			return lastErr
		}
	}
	if s.IsAlive(cleanupCtx) {
		if err := run(cleanupCtx, "tmux", "kill-session", "-t", s.Name); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s %s failed: %w\n%s", name, strings.Join(args, " "), err, out)
}

func output(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s failed: %w\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

func requireExecutable(name string) error {
	if name == "" {
		return errors.New("empty executable name")
	}
	if strings.ContainsRune(name, filepath.Separator) {
		_, err := os.Stat(name)
		return err
	}
	_, err := exec.LookPath(name)
	return err
}

func trimForError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 240 {
		return value
	}
	return value[:240]
}

func sleepOrDone(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
