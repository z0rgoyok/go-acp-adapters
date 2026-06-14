package acp

const ProtocolVersion uint16 = 1

type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeRequest struct {
	ProtocolVersion    uint16             `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities,omitempty"`
	ClientInfo         *Implementation    `json:"clientInfo,omitempty"`
	Meta               map[string]any     `json:"_meta,omitempty"`
}

type InitializeResponse struct {
	ProtocolVersion   uint16            `json:"protocolVersion"`
	AgentInfo         *Implementation   `json:"agentInfo"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
	AuthMethods       []AuthMethod      `json:"authMethods"`
}

type ClientCapabilities struct{}

type AgentCapabilities struct {
	LoadSession         bool                  `json:"loadSession"`
	PromptCapabilities  PromptCapabilities    `json:"promptCapabilities"`
	McpCapabilities     McpCapabilities       `json:"mcpCapabilities"`
	SessionCapabilities SessionCapabilities   `json:"sessionCapabilities"`
	Auth                AgentAuthCapabilities `json:"auth"`
}

type PromptCapabilities struct {
	Text         bool `json:"text"`
	ResourceLink bool `json:"resourceLink"`
}

type McpCapabilities struct {
	Stdio bool `json:"stdio"`
	HTTP  bool `json:"http"`
	SSE   bool `json:"sse"`
}

type SessionCapabilities struct {
	Close *CloseSessionCapability `json:"close,omitempty"`
}

type CloseSessionCapability struct{}

type AgentAuthCapabilities struct{}

type AuthMethod struct {
	Type string `json:"type"`
}

type NewSessionRequest struct {
	Cwd                   string      `json:"cwd"`
	AdditionalDirectories []string    `json:"additionalDirectories,omitempty"`
	McpServers            []McpServer `json:"mcpServers,omitempty"`
}

type NewSessionResponse struct {
	SessionID     string                `json:"sessionId"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
}

type SessionConfigOption struct {
	Type         string                      `json:"type"`
	ID           string                      `json:"id"`
	Name         string                      `json:"name"`
	Category     string                      `json:"category,omitempty"`
	CurrentValue string                      `json:"currentValue"`
	Options      []SessionConfigSelectOption `json:"options"`
}

type SessionConfigSelectOption struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type SessionModeState struct {
	AvailableModes []SessionMode `json:"availableModes"`
	CurrentModeID  string        `json:"currentModeId"`
}

type SessionMode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type SetSessionModelRequest struct {
	SessionID string `json:"sessionId"`
	ModelID   string `json:"modelId"`
}

type SetSessionModelResponse struct{}

type SetSessionConfigOptionRequest struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

type SetSessionConfigOptionResponse struct{}

type McpServer struct {
	Name    string        `json:"name"`
	Type    string        `json:"type"`
	Command string        `json:"command,omitempty"`
	Args    []string      `json:"args,omitempty"`
	Env     []EnvVariable `json:"env,omitempty"`
}

type EnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type PromptRequest struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

type PromptResponse struct {
	StopReason StopReason `json:"stopReason"`
}

type StopReason string

const (
	StopReasonEndTurn         StopReason = "end_turn"
	StopReasonMaxTokens       StopReason = "max_tokens"
	StopReasonMaxTurnRequests StopReason = "max_turn_requests"
	StopReasonRefusal         StopReason = "refusal"
	StopReasonCancelled       StopReason = "cancelled"
)

type CloseSessionRequest struct {
	SessionID string `json:"sessionId"`
}

type CancelSessionRequest struct {
	SessionID string `json:"sessionId"`
}

type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	URI      string `json:"uri,omitempty"`
	Title    string `json:"title,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type SessionUpdateParams struct {
	SessionID string        `json:"sessionId"`
	Update    SessionUpdate `json:"update"`
}

type SessionUpdate struct {
	SessionUpdate string       `json:"sessionUpdate"`
	MessageID     string       `json:"messageId,omitempty"`
	Content       ContentBlock `json:"content"`
}
