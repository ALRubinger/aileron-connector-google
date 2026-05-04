// Package main is the WASM source for the aileron-connector-google
// reference connector. It targets Go's native WASI Preview 1
// (`GOOS=wasip1 GOARCH=wasm`) and calls into Aileron's host-import ABI
// for outbound HTTP and credential mediation.
//
// Build:
//
//	cd connector && GOOS=wasip1 GOARCH=wasm go build -trimpath \
//	  -ldflags="-s -w" -o ../connector.wasm .
//
// Or via Taskfile from the repo root:
//
//	task build
//
// I/O contract (stdin → stdout JSON):
//
//	{"op": "list_recent_emails", "args": {"query": "is:unread", "max_results": 10}}
//	  → {"output": {"messages": [...]}}
//
//	{"op": "list_upcoming_events", "args": {"calendar_id": "primary", "max_results": 10}}
//	  → {"output": {"items": [...]}}
//
//	{"error": {"class": "...", "message": "..."}}  on failure
//
// All outbound HTTP is routed through `aileron_host.http_request` with
// `credential: "oauth2"` so the runtime injects the bound bearer token
// host-side. The connector never holds the OAuth token.
//
//go:build wasip1

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"time"
	"unsafe"
)

//go:wasmimport aileron_host log
//go:noescape
func hostLog(levelPtr unsafe.Pointer, levelLen uint32, msgPtr unsafe.Pointer, msgLen uint32)

//go:wasmimport aileron_host http_request
//go:noescape
func hostHTTPRequest(reqPtr unsafe.Pointer, reqLen uint32) int32

//go:wasmimport aileron_host http_response_size
//go:noescape
func hostHTTPResponseSize() int32

//go:wasmimport aileron_host http_response_status
//go:noescape
func hostHTTPResponseStatus() int32

//go:wasmimport aileron_host http_response_read
//go:noescape
func hostHTTPResponseRead(dstPtr unsafe.Pointer, dstLen uint32) int32

// _emptyPtrSentinel keeps the address of an empty byte slice valid; Go
// can't take the address of an empty slice's first element directly.
var _emptyPtrSentinel = [1]byte{}

func ptr(b []byte) unsafe.Pointer {
	if len(b) == 0 {
		return unsafe.Pointer(&_emptyPtrSentinel[0])
	}
	return unsafe.Pointer(&b[0])
}

func aileronLog(level, message string) {
	lb := []byte(level)
	mb := []byte(message)
	hostLog(ptr(lb), uint32(len(lb)), ptr(mb), uint32(len(mb)))
}

type input struct {
	Op   string         `json:"op"`
	Args map[string]any `json:"args"`
}

type output struct {
	Output map[string]any `json:"output,omitempty"`
	Error  *outputError   `json:"error,omitempty"`
}

type outputError struct {
	Class   string `json:"class"`
	Message string `json:"message"`
}

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		writeError("connector_runtime_error", "read_stdin: "+err.Error())
		os.Exit(1)
	}
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		writeError("connector_runtime_error", "parse_input: "+err.Error())
		os.Exit(1)
	}

	switch in.Op {
	case "list_recent_emails":
		listRecentEmails(in.Args)
	case "list_upcoming_events":
		listUpcomingEvents(in.Args)
	default:
		writeError("connector_runtime_error", "unknown op: "+in.Op)
		os.Exit(1)
	}
}

// listRecentEmails calls Gmail's users.messages.list endpoint.
//
//	GET https://gmail.googleapis.com/gmail/v1/users/me/messages?q={query}&maxResults={n}
//
// Args:
//
//	query        (string, optional) — Gmail search query, e.g. "is:unread".
//	max_results  (number, optional) — page size cap; default 10.
//
// Output: the raw Gmail messages.list JSON (a list of {id, threadId} pairs
// plus paging metadata). Resolving message bodies is left to subsequent
// actions / agent reasoning at v0.1.0 to keep the call cost bounded.
func listRecentEmails(args map[string]any) {
	query, _ := args["query"].(string)
	maxResults := readMaxResults(args, 10)

	q := url.Values{}
	q.Set("maxResults", strconv.Itoa(maxResults))
	if query != "" {
		q.Set("q", query)
	}
	target := "https://gmail.googleapis.com/gmail/v1/users/me/messages?" + q.Encode()

	body, status, err := doAuthenticatedGet(target)
	if err != nil {
		writeError("connector_runtime_error", "list_recent_emails: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Gmail API returned %d: %s", status, string(body)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeError("connector_runtime_error", "list_recent_emails: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// listUpcomingEvents calls Calendar's events.list endpoint.
//
//	GET https://www.googleapis.com/calendar/v3/calendars/{calendarId}/events
//	    ?maxResults={n}&timeMin={now}&singleEvents=true&orderBy=startTime
//
// Args:
//
//	calendar_id  (string, optional) — calendar id; default "primary".
//	max_results  (number, optional) — page size cap; default 10.
//
// Output: the raw Calendar events.list JSON. timeMin is set to "now" so
// the agent gets upcoming events; singleEvents=true expands recurring
// events; orderBy=startTime returns chronological order.
func listUpcomingEvents(args map[string]any) {
	calendarID, _ := args["calendar_id"].(string)
	if calendarID == "" {
		calendarID = "primary"
	}
	maxResults := readMaxResults(args, 10)

	q := url.Values{}
	q.Set("maxResults", strconv.Itoa(maxResults))
	q.Set("timeMin", time.Now().UTC().Format(time.RFC3339))
	q.Set("singleEvents", "true")
	q.Set("orderBy", "startTime")
	target := "https://www.googleapis.com/calendar/v3/calendars/" + url.PathEscape(calendarID) + "/events?" + q.Encode()

	body, status, err := doAuthenticatedGet(target)
	if err != nil {
		writeError("connector_runtime_error", "list_upcoming_events: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Calendar API returned %d: %s", status, string(body)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeError("connector_runtime_error", "list_upcoming_events: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// doAuthenticatedGet issues an HTTP GET via the host-import ABI and
// returns (body, status, err). The `credential: "oauth2"` field tells
// the host to inject the bound bearer token; the connector never sees
// the token bytes.
func doAuthenticatedGet(target string) ([]byte, int, error) {
	req, err := json.Marshal(map[string]any{
		"method":     "GET",
		"url":        target,
		"credential": "oauth2",
		"headers":    map[string]string{"Accept": "application/json"},
	})
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request: %w", err)
	}
	rc := hostHTTPRequest(ptr(req), uint32(len(req)))
	if rc != 0 {
		// The host has stuck a structured *Error on the per-call state;
		// the runtime surfaces it as an ADR-0010 envelope to the caller.
		// From the connector's point of view we just need to bail —
		// emitting our own error here would double-wrap the host's.
		return nil, 0, fmt.Errorf("http_request denied or failed (rc=%d)", rc)
	}
	size := hostHTTPResponseSize()
	if size < 0 {
		return nil, 0, fmt.Errorf("http_response_size returned %d", size)
	}
	body := make([]byte, size)
	if size > 0 {
		n := hostHTTPResponseRead(ptr(body), uint32(size))
		if n < 0 {
			return nil, 0, fmt.Errorf("http_response_read returned %d", n)
		}
		body = body[:n]
	}
	return body, int(hostHTTPResponseStatus()), nil
}

// readMaxResults extracts max_results from args. The JSON unmarshal
// produces float64 for numbers; this normalises it to int with a sane
// default and a sensible upper bound to keep API quotas under control.
func readMaxResults(args map[string]any, def int) int {
	const cap = 100
	v, ok := args["max_results"]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		i := int(n)
		if i <= 0 {
			return def
		}
		if i > cap {
			return cap
		}
		return i
	case int:
		if n <= 0 {
			return def
		}
		if n > cap {
			return cap
		}
		return n
	default:
		return def
	}
}

func writeOutput(out map[string]any) {
	_ = json.NewEncoder(os.Stdout).Encode(output{Output: out})
}

func writeError(class, message string) {
	aileronLog("error", message)
	_ = json.NewEncoder(os.Stdout).Encode(output{Error: &outputError{Class: class, Message: message}})
}
