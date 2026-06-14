package app

import "claude-acp-adapter/internal/acp"

type Error struct {
	Code    int
	Message string
}

func (e Error) Error() string { return e.Message }

func (e Error) RPCCode() int { return e.Code }

func invalidParams(message string) Error {
	return Error{Code: acp.CodeInvalidParams, Message: message}
}

func internalError(message string) Error {
	return Error{Code: acp.CodeInternalError, Message: message}
}
