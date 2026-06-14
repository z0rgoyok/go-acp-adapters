package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"claude-acp-adapter/internal/acp"
	"claude-acp-adapter/internal/app"
	"claude-acp-adapter/internal/claude"
)

var serviceOptions app.Options

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	var err error
	serviceOptions.ToolCfg, err = loadToolConfig(args)
	if err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "query" {
		return runQuery(args[1:], stdin, stdout)
	}
	return runACP(stdin, stdout, stderr)
}

func runACP(stdin io.Reader, stdout, stderr io.Writer) error {
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	logCleanupErrors(stderr, claude.CleanupStaleOwnedSessions(context.Background(), claude.StaleCleanupOptions{OlderThan: 30 * time.Minute}))
	defer claude.CleanupActiveSessions(context.Background())

	service := app.NewService(serviceOptions)
	server := acp.NewServer(service, stdin, stdout, stderr)
	return server.Serve(ctx)
}

func runQuery(args []string, stdin io.Reader, stdout io.Writer) error {
	var cwd string
	var model string
	var timeout time.Duration
	var prompt string

	flags := flag.NewFlagSet("query", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&cwd, "cwd", "", "working directory for Claude")
	flags.StringVar(&model, "model", "", "Claude model")
	flags.DurationVar(&timeout, "timeout", 90*time.Second, "query timeout")
	flags.StringVar(&prompt, "prompt", "", "prompt text; stdin is used when empty")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(prompt) == "" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		prompt = string(data)
	}
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("prompt is empty")
	}

	client, err := claude.NewClient(claude.Options{WorkingDir: cwd, Model: model, Timeout: timeout})
	if err != nil {
		return err
	}
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	logCleanupErrors(os.Stderr, claude.CleanupStaleOwnedSessions(context.Background(), claude.StaleCleanupOptions{OlderThan: 30 * time.Minute}))
	defer claude.CleanupActiveSessions(context.Background())

	if err := client.Connect(ctx); err != nil {
		return err
	}
	defer client.Disconnect(context.Background())

	response, err := client.Query(ctx, prompt)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, response.Text)
	return nil
}

func logCleanupErrors(stderr io.Writer, errs []error) {
	for _, err := range errs {
		fmt.Fprintf(stderr, "stale cleanup: %v\n", err)
	}
}
