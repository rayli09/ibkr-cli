// Package apierr defines the typed errors of ibkr-cli and their
// mapping to the stable process exit codes documented in
// docs/SPEC.md §2: 0 ok, 2 usage, 3 auth/session, 4 API error,
// 5 question-pending (orders).
package apierr

import (
	"errors"
	"fmt"
)

// Exit codes.
const (
	ExitOK       = 0
	ExitUsage    = 2
	ExitSession  = 3
	ExitAPI      = 4
	ExitQuestion = 5
)

// UsageError is a bad-flag / bad-argument failure (exit 2).
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// SessionError is an authentication or brokerage-session failure
// (exit 3): no live session, expired session, competing session.
type SessionError struct{ Msg string }

func (e *SessionError) Error() string { return e.Msg }

// APIError is an error response or transport failure from the API
// (exit 4). StatusCode is 0 for transport failures.
type APIError struct {
	StatusCode int
	Body       string
	Msg        string
}

func (e *APIError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	if e.Body != "" {
		return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Body)
	}
	return fmt.Sprintf("API error (status %d)", e.StatusCode)
}

// RateLimitError reports a 429 that persisted after retries (a kind
// of APIError for exit-code purposes, but distinguishable).
type RateLimitError struct{ Route string }

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited on %s after retries", e.Route)
}

// Question is one pending order-confirmation question from CPAPI.
type Question struct {
	ID       string   // reply id for POST /iserver/reply/{id}
	Messages []string // the question text, verbatim
}

// QuestionPending means an order placement/reply stopped on one or
// more unanswered questions (exit 5). The questions are carried so
// the cmd layer can print them verbatim; nothing is auto-answered.
type QuestionPending struct {
	Questions []Question
}

func (e *QuestionPending) Error() string {
	return fmt.Sprintf("%d order confirmation question(s) pending", len(e.Questions))
}

// ExitCode maps an error to its process exit code.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var q *QuestionPending
	if errors.As(err, &q) {
		return ExitQuestion
	}
	var s *SessionError
	if errors.As(err, &s) {
		return ExitSession
	}
	var u *UsageError
	if errors.As(err, &u) {
		return ExitUsage
	}
	var a *APIError
	if errors.As(err, &a) {
		return ExitAPI
	}
	var r *RateLimitError
	if errors.As(err, &r) {
		return ExitAPI
	}
	return 1
}
