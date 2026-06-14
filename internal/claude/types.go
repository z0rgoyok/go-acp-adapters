package claude

import (
	"encoding/json"
	"time"
)

type Options struct {
	ClaudePath     string
	WorkingDir     string
	ConfigDir      string
	PermissionMode string
	Model          string
	Timeout        time.Duration
	TmuxName       string
	ExtraArgs      []string
}

type Response struct {
	SessionID      string
	TurnID         string
	Text           string
	Messages       []AssistantMessage
	Duration       time.Duration
	TranscriptPath string
	Diagnostics    TurnDiagnostics
}

type TurnDiagnostics struct {
	SessionID       string
	ClaudeSessionID string
	TurnID          string
	TmuxName        string
	TranscriptPath  string
	StartOffset     int64
	OwnershipMarker string
}

type AssistantMessage struct {
	ByteOffset int64
	MessageID  string
	Text       string
	StopReason string
	Timestamp  time.Time
}

type TranscriptEvent interface {
	transcriptEvent()
}

type AssistantTextEvent struct {
	ByteOffset int64
	Timestamp  time.Time
	MessageID  string
	Text       string
	StopReason string
}

type AssistantToolUseEvent struct {
	ByteOffset int64
	Timestamp  time.Time
	MessageID  string
	ToolUseID  string
	Name       string
	Input      json.RawMessage
}

type ToolResultEvent struct {
	ByteOffset int64
	Timestamp  time.Time
	ToolUseID  string
	Content    json.RawMessage
	IsError    bool
}

type SessionTitleEvent struct {
	ByteOffset int64
	Timestamp  time.Time
	Title      string
}

type UsageEvent struct {
	ByteOffset int64
	Timestamp  time.Time
	Usage      json.RawMessage
}

type UnknownTranscriptEvent struct {
	ByteOffset int64
	EventType  string
	Keys       []string
}

type TranscriptDiagnosticEvent struct {
	ByteOffset int64
	EventType  string
	Message    string
}

func (AssistantTextEvent) transcriptEvent()        {}
func (AssistantToolUseEvent) transcriptEvent()     {}
func (ToolResultEvent) transcriptEvent()           {}
func (SessionTitleEvent) transcriptEvent()         {}
func (UsageEvent) transcriptEvent()                {}
func (UnknownTranscriptEvent) transcriptEvent()    {}
func (TranscriptDiagnosticEvent) transcriptEvent() {}

type TurnStream struct {
	Events <-chan TranscriptEvent
	Done   <-chan TurnResult
}

type TurnResult struct {
	Response Response
	Err      error
}

type stopPayload struct {
	TurnID         string `json:"turn_id"`
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
}
