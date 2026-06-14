package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

type Backend interface {
	Initialize(context.Context, InitializeRequest) (InitializeResponse, error)
	NewSession(context.Context, NewSessionRequest) (NewSessionResponse, error)
	SetSessionModel(context.Context, SetSessionModelRequest) (SetSessionModelResponse, error)
	SetSessionConfigOption(context.Context, SetSessionConfigOptionRequest) (SetSessionConfigOptionResponse, error)
	Prompt(context.Context, PromptRequest, Notifier) (PromptResponse, error)
	CancelSession(context.Context, CancelSessionRequest) error
	CloseSession(context.Context, CloseSessionRequest) error
	Shutdown(context.Context) error
}

type PromptTurn interface {
	Run(context.Context, Notifier) (PromptResponse, error)
}

type PromptStarter interface {
	StartPrompt(context.Context, PromptRequest) (PromptTurn, error)
}

type Notifier interface {
	SessionUpdate(SessionUpdateParams) error
}

type Server struct {
	backend Backend
	in      io.Reader
	out     io.Writer
	err     io.Writer
	mu      sync.Mutex
}

type scanResult struct {
	line []byte
	err  error
}

func NewServer(backend Backend, in io.Reader, out io.Writer, err io.Writer) *Server {
	return &Server{backend: backend, in: in, out: out, err: err}
}

func (s *Server) Serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() { _ = s.backend.Shutdown(context.Background()) })
	}
	defer shutdown()

	lines := make(chan scanResult, 1)
	go s.scan(ctx, lines)

	var handlers sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			closeInput(s.in)
			shutdown()
			return ctx.Err()
		case result, ok := <-lines:
			if !ok {
				cancel()
				shutdown()
				handlers.Wait()
				return nil
			}
			if result.err != nil {
				return result.err
			}
			s.handleLine(ctx, result.line, &handlers)
		}
	}
}

func (s *Server) scan(ctx context.Context, lines chan<- scanResult) {
	defer close(lines)
	scanner := bufio.NewScanner(s.in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		select {
		case lines <- scanResult{line: line}:
		case <-ctx.Done():
			return
		}
	}
	if err := scanner.Err(); err != nil {
		select {
		case lines <- scanResult{err: err}:
		case <-ctx.Done():
		}
	}
}

func (s *Server) handleLine(ctx context.Context, line []byte, handlers *sync.WaitGroup) {
	var shape any
	if err := json.Unmarshal(line, &shape); err != nil {
		s.write(ParseErrorResponse("parse error"))
		return
	}
	var request Request
	if err := json.Unmarshal(line, &request); err != nil {
		s.write(ErrorResponse(ID{Raw: json.RawMessage("null"), Present: true}, CodeInvalidRequest, "invalid request"))
		return
	}
	if request.Method == "session/prompt" {
		s.startPrompt(ctx, request, handlers)
		return
	}
	s.dispatchAndWrite(ctx, request)
}

func (s *Server) startPrompt(ctx context.Context, request Request, handlers *sync.WaitGroup) {
	starter, ok := s.backend.(PromptStarter)
	if !ok {
		handlers.Add(1)
		go func() {
			defer handlers.Done()
			s.dispatchAndWrite(ctx, request)
		}()
		return
	}
	var params PromptRequest
	if err := decodeParams(request.Params, &params); err != nil {
		if !request.ID.IsNotification() {
			s.write(ErrorResponse(request.ID, CodeInvalidParams, err.Error()))
		}
		return
	}
	turn, err := starter.StartPrompt(ctx, params)
	if err != nil {
		if !request.ID.IsNotification() {
			s.write(responseFromResult(request.ID, nil, err))
		}
		return
	}
	handlers.Add(1)
	go func() {
		defer handlers.Done()
		result, err := turn.Run(ctx, serverNotifier{s: s})
		if !request.ID.IsNotification() {
			s.write(responseFromResult(request.ID, result, err))
		}
	}()
}

func (s *Server) dispatchAndWrite(ctx context.Context, request Request) {
	response, ok := s.dispatch(ctx, request)
	if ok {
		s.write(response)
	}
}

func closeInput(reader io.Reader) {
	closer, ok := reader.(io.Closer)
	if ok {
		_ = closer.Close()
	}
}

func (s *Server) dispatch(ctx context.Context, request Request) (Response, bool) {
	switch request.Method {
	case "initialize":
		var params InitializeRequest
		if err := decodeParams(request.Params, &params); err != nil {
			return ErrorResponse(request.ID, CodeInvalidParams, err.Error()), !request.ID.IsNotification()
		}
		result, err := s.backend.Initialize(ctx, params)
		return responseFromResult(request.ID, result, err), !request.ID.IsNotification()
	case "session/new":
		var params NewSessionRequest
		if err := decodeParams(request.Params, &params); err != nil {
			return ErrorResponse(request.ID, CodeInvalidParams, err.Error()), !request.ID.IsNotification()
		}
		result, err := s.backend.NewSession(ctx, params)
		return responseFromResult(request.ID, result, err), !request.ID.IsNotification()
	case "session/set_model":
		var params SetSessionModelRequest
		if err := decodeParams(request.Params, &params); err != nil {
			return ErrorResponse(request.ID, CodeInvalidParams, err.Error()), !request.ID.IsNotification()
		}
		result, err := s.backend.SetSessionModel(ctx, params)
		return responseFromResult(request.ID, result, err), !request.ID.IsNotification()
	case "session/set_config_option":
		var params SetSessionConfigOptionRequest
		if err := decodeParams(request.Params, &params); err != nil {
			return ErrorResponse(request.ID, CodeInvalidParams, err.Error()), !request.ID.IsNotification()
		}
		result, err := s.backend.SetSessionConfigOption(ctx, params)
		return responseFromResult(request.ID, result, err), !request.ID.IsNotification()
	case "session/prompt":
		var params PromptRequest
		if err := decodeParams(request.Params, &params); err != nil {
			return ErrorResponse(request.ID, CodeInvalidParams, err.Error()), !request.ID.IsNotification()
		}
		result, err := s.backend.Prompt(ctx, params, serverNotifier{s: s})
		return responseFromResult(request.ID, result, err), !request.ID.IsNotification()
	case "session/cancel":
		var params CancelSessionRequest
		if err := decodeParams(request.Params, &params); err == nil {
			_ = s.backend.CancelSession(ctx, params)
		}
		return Response{}, false
	case "session/close":
		var params CloseSessionRequest
		if err := decodeParams(request.Params, &params); err != nil {
			return ErrorResponse(request.ID, CodeInvalidParams, err.Error()), !request.ID.IsNotification()
		}
		err := s.backend.CloseSession(ctx, params)
		return responseFromResult(request.ID, map[string]any{}, err), !request.ID.IsNotification()
	default:
		return ErrorResponse(request.ID, CodeMethodNotFound, "method not found"), !request.ID.IsNotification()
	}
}

func (s *Server) write(message any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := json.NewEncoder(s.out).Encode(message); err != nil && s.err != nil {
		fmt.Fprintf(s.err, "write response: %v\n", err)
	}
}

type serverNotifier struct{ s *Server }

func (n serverNotifier) SessionUpdate(params SessionUpdateParams) error {
	n.s.write(map[string]any{"method": "session/update", "params": params})
	return nil
}

func decodeParams(data json.RawMessage, target any) error {
	if len(data) == 0 {
		data = []byte("{}")
	}
	return json.Unmarshal(data, target)
}

func responseFromResult(id ID, result any, err error) Response {
	if err != nil {
		if rpc, ok := err.(interface{ RPCCode() int }); ok {
			return ErrorResponse(id, rpc.RPCCode(), err.Error())
		}
		return ErrorResponse(id, CodeInternalError, err.Error())
	}
	return SuccessResponse(id, result)
}
