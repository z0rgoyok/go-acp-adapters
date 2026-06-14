package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

func createFIFO(path string) error {
	if info, err := os.Stat(path); err == nil {
		if info.Mode()&os.ModeNamedPipe == 0 {
			return fmt.Errorf("%s exists and is not a fifo", path)
		}
		return nil
	}
	return unix.Mkfifo(path, 0o600)
}

type stopReader struct {
	ctx      context.Context
	cancel   context.CancelFunc
	path     string
	file     *os.File
	payloads chan stopPayload
	done     chan struct{}
	once     sync.Once
}

func startStopReader(ctx context.Context, path string) (*stopReader, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	readCtx, cancel := context.WithCancel(ctx)
	reader := &stopReader{
		ctx:      readCtx,
		cancel:   cancel,
		path:     path,
		file:     file,
		payloads: make(chan stopPayload, 8),
		done:     make(chan struct{}),
	}
	go reader.readLoop()
	return reader, nil
}

func (r *stopReader) readLoop() {
	defer close(r.done)
	defer close(r.payloads)

	reader := bufio.NewReader(r.file)
	for {
		select {
		case <-r.ctx.Done():
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if line != "" {
			if payload, ok := parseStopPayload([]byte(line)); ok {
				select {
				case r.payloads <- payload:
				case <-r.ctx.Done():
					return
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func parseStopPayload(line []byte) (stopPayload, bool) {
	var envelope struct {
		TurnID  string          `json:"turn_id"`
		Raw     json.RawMessage `json:"raw"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(line, &envelope); err == nil {
		raw := envelope.Raw
		if len(raw) == 0 {
			raw = envelope.Payload
		}
		if len(raw) > 0 {
			var payload stopPayload
			if err := json.Unmarshal(raw, &payload); err != nil {
				return stopPayload{}, false
			}
			if payload.TurnID == "" {
				payload.TurnID = envelope.TurnID
			}
			return payload, payload != (stopPayload{})
		}
	}
	var payload stopPayload
	if err := json.Unmarshal(line, &payload); err == nil && payload != (stopPayload{}) {
		return payload, true
	}
	return stopPayload{}, false
}

func (r *stopReader) Payloads() <-chan stopPayload {
	return r.payloads
}

func (r *stopReader) Drain() {
	for {
		select {
		case _, ok := <-r.payloads:
			if !ok {
				return
			}
		default:
			return
		}
	}
}

func (r *stopReader) Stop() {
	r.once.Do(func() {
		r.cancel()
		if writer, err := os.OpenFile(r.path, os.O_WRONLY, 0); err == nil {
			_, _ = writer.WriteString("\n")
			_ = writer.Close()
		}
		_ = r.file.Close()
		<-r.done
	})
}
