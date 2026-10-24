// Package audit appends one JSON line per mutating API call to a
// local 0600 audit log (SPEC §5.1.5): timestamp, account, request,
// response, reply chain. Secrets are never included — order bodies
// carry no credentials by construction.
package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Entry is one audit record.
type Entry struct {
	Time     time.Time       `json:"time"`
	Command  string          `json:"command"`
	Account  string          `json:"account,omitempty"`
	Request  json.RawMessage `json:"request,omitempty"`
	Response json.RawMessage `json:"response,omitempty"`
	Note     string          `json:"note,omitempty"`
}

// Log appends an entry to the audit log at path, creating it 0600.
// Logging failures are returned but callers treat them as non-fatal:
// the audit trail must never block a read, and for mutations the
// caller reports the failure alongside the outcome.
func Log(path, command, account string, request, response []byte, note string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	e := Entry{Time: time.Now().UTC(), Command: command, Account: account, Note: note}
	if len(request) > 0 {
		e.Request = json.RawMessage(request)
	}
	if len(response) > 0 {
		e.Response = json.RawMessage(response)
	}
	return json.NewEncoder(f).Encode(e)
}
