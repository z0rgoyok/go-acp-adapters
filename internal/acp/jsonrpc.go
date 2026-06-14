package acp

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

type ID struct {
	Raw     json.RawMessage
	Present bool
}

func (id ID) IsNotification() bool {
	return !id.Present
}

func (id ID) MarshalJSON() ([]byte, error) {
	if len(id.Raw) == 0 {
		return []byte("null"), nil
	}
	return id.Raw, nil
}

func (id *ID) UnmarshalJSON(data []byte) error {
	if err := validateID(data); err != nil {
		return err
	}
	id.Raw = append(json.RawMessage(nil), data...)
	id.Present = true
	return nil
}

type Request struct {
	ID     ID
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

func (r *Request) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	method, ok := raw["method"]
	if !ok {
		return fmt.Errorf("missing method")
	}
	if err := json.Unmarshal(method, &r.Method); err != nil || r.Method == "" {
		return fmt.Errorf("invalid method")
	}
	if id, ok := raw["id"]; ok {
		if err := validateID(id); err != nil {
			return err
		}
		r.ID = ID{Raw: append(json.RawMessage(nil), id...), Present: true}
	}
	r.Params = raw["params"]
	return nil
}

type Response struct {
	ID     ID        `json:"id"`
	Result any       `json:"result,omitempty"`
	Error  *RPCError `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func ErrorResponse(id ID, code int, message string) Response {
	return Response{ID: id, Error: &RPCError{Code: code, Message: message}}
}

func SuccessResponse(id ID, result any) Response {
	return Response{ID: id, Result: result}
}

func ParseErrorResponse(message string) Response {
	return ErrorResponse(ID{Raw: json.RawMessage("null"), Present: true}, CodeParseError, message)
}

func validateID(data json.RawMessage) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	var s string
	if json.Unmarshal(data, &s) == nil {
		return nil
	}
	var n json.Number
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&n); err != nil {
		return fmt.Errorf("invalid id")
	}
	if _, err := n.Int64(); err != nil {
		return fmt.Errorf("invalid id")
	}
	return nil
}
