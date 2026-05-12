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
//	{"op": "get_email", "args": {"id": "19df4136f28569d2"}}
//	  → {"output": {"id": "...", "snippet": "...", "payload": {"headers": [...]}, ...}}
//
//	{"op": "list_upcoming_events", "args": {"calendar_id": "primary", "max_results": 10}}
//	  → {"output": {"items": [...]}}
//
//	{"op": "draft_email",
//	 "args": {"to": "alice@example.com", "subject": "...", "body": "..."}}
//	  → {"output": {"id": "...", "message": {...}}}
//
//	{"op": "send_email",
//	 "args": {"to": "alice@example.com", "subject": "...", "body": "..."}}
//	  → {"output": {"id": "...", "threadId": "...", "labelIds": [...]}}
//
//	{"op": "send_draft", "args": {"draft_id": "r-12345"}}
//	  → {"output": {"id": "...", "threadId": "...", "labelIds": [...]}}
//
//	{"op": "create_calendar_event",
//	 "args": {"title": "...", "start_time": "2026-05-04T15:00:00-07:00",
//	          "end_time": "2026-05-04T16:00:00-07:00",
//	          "attendees": ["alice@example.com"]}}
//	  → {"output": {"id": "...", "htmlLink": "..."}}
//
//	{"error": {"class": "...", "message": "..."}}  on failure
//
// All outbound HTTP is routed through `aileron_host.http_request` with
// `credential: "oauth2"` so the runtime injects the bound bearer token
// host-side. The connector never holds the OAuth token.
//
// Idempotency: the read ops (list_*) are idempotent by their HTTP
// shape (GET). The write ops (draft_email, send_email, send_draft,
// create_calendar_event) are NOT idempotent — repeating them creates
// duplicate drafts/messages/events. Action manifests using these ops
// MUST set [[execute]].idempotent = false so the gateway's retry layer
// (ADR-0010) does not double-write. send_email and send_draft
// additionally require per-call user approval at the action layer (see
// actions/send-email/action.md and actions/send-draft/action.md) since
// dispatched mail is not recoverable the way a draft is.
//
//go:build wasip1

package main

import (
	"encoding/base64"
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
	case "get_email":
		getEmail(in.Args)
	case "list_upcoming_events":
		listUpcomingEvents(in.Args)
	case "draft_email":
		draftEmail(in.Args)
	case "send_email":
		sendEmail(in.Args)
	case "send_draft":
		sendDraft(in.Args)
	case "create_calendar_event":
		createCalendarEvent(in.Args)
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
// actions / agent reasoning at v0.0.1 to keep the call cost bounded.
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

// getEmail calls Gmail's users.messages.get endpoint for a single
// message id with format=metadata.
//
//	GET https://gmail.googleapis.com/gmail/v1/users/me/messages/{id}?format=metadata
//
// `format=metadata` returns headers (Subject, From, To, Date, etc.),
// labelIds, snippet (~200-char body preview), and internalDate without
// fetching the full MIME body. That is the right call cost / utility
// trade-off for "summarize my recent emails" and "what does this email
// say at a glance" agent flows. For full-body access, callers can add
// a future `format` arg or a separate `get_email_body` op.
//
// Args:
//
//	id  (string, required) — Gmail message id, as returned by
//	    `list_recent_emails` in `messages[].id`.
//
// Output: the parsed users.messages.get response. The agent typically
// reads `payload.headers[]` and `snippet` directly.
func getEmail(args map[string]any) {
	id, _ := args["id"].(string)
	if id == "" {
		writeError("connector_runtime_error", "get_email: id is required")
		return
	}
	q := url.Values{}
	q.Set("format", "metadata")
	target := "https://gmail.googleapis.com/gmail/v1/users/me/messages/" +
		url.PathEscape(id) + "?" + q.Encode()

	body, status, err := doAuthenticatedGet(target)
	if err != nil {
		writeError("connector_runtime_error", "get_email: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Gmail API returned %d: %s", status, string(body)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeError("connector_runtime_error", "get_email: parse: "+err.Error())
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

// draftEmail creates a Gmail draft via users.drafts.create.
//
//	POST https://gmail.googleapis.com/gmail/v1/users/me/drafts
//	Body: {"message": {"raw": "<base64url(RFC 2822)>"}}
//
// The op only creates drafts — it never sends. Sending is a separate
// op (deliberately split so action authors can reason about
// reversibility). The user reviews and sends the draft from Gmail's
// drafts folder; agents cannot bypass that review step at v0.x.
//
// Args:
//
//	to       (string, required) — comma-separated recipient addresses.
//	subject  (string, required) — the Subject header.
//	body     (string, required) — message body (text/plain).
//	cc       (string, optional) — comma-separated Cc addresses.
//	bcc      (string, optional) — comma-separated Bcc addresses.
//
// Output: the created draft's API representation (id, message id, etc.).
//
// NOTE: this op is NOT idempotent. The action manifest MUST declare
// [[execute]].idempotent = false so the gateway retry layer does not
// produce duplicate drafts on transient failures.
func draftEmail(args map[string]any) {
	to, _ := args["to"].(string)
	subject, _ := args["subject"].(string)
	body, _ := args["body"].(string)
	if to == "" || subject == "" || body == "" {
		writeError("connector_runtime_error", "draft_email: to, subject, and body are required")
		return
	}
	cc, _ := args["cc"].(string)
	bcc, _ := args["bcc"].(string)

	rfc2822 := buildRFC2822(to, cc, bcc, subject, body)
	encoded := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(rfc2822))

	reqBody, err := json.Marshal(map[string]any{
		"message": map[string]any{"raw": encoded},
	})
	if err != nil {
		writeError("connector_runtime_error", "draft_email: encode request: "+err.Error())
		return
	}

	target := "https://gmail.googleapis.com/gmail/v1/users/me/drafts"
	respBody, status, err := doAuthenticatedJSON("POST", target, reqBody)
	if err != nil {
		writeError("connector_runtime_error", "draft_email: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Gmail API returned %d: %s", status, string(respBody)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		writeError("connector_runtime_error", "draft_email: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// sendEmail dispatches an email via users.messages.send.
//
//	POST https://gmail.googleapis.com/gmail/v1/users/me/messages/send
//	Body: {"raw": "<base64url(RFC 2822)>"}
//
// Unlike draft_email, the message leaves the user's outbox immediately
// — there is no "review in Gmail Drafts" step. The action manifest
// gates this op behind Aileron's per-call approval mechanism so the
// runtime asks the user before dispatch; on denial the connector is
// never invoked and no quota is burned.
//
// Args:
//
//	to       (string, required) — comma-separated recipient addresses.
//	subject  (string, required) — the Subject header.
//	body     (string, required) — message body (text/plain).
//	cc       (string, optional) — comma-separated Cc addresses.
//	bcc      (string, optional) — comma-separated Bcc addresses.
//
// Output: the sent message's API representation (id, threadId, labelIds).
//
// NOTE: NOT idempotent. The action manifest MUST declare
// [[execute]].idempotent = false AND [approval].required = true so the
// runtime gates dispatch behind user approval and the gateway retry
// layer does not produce duplicate sends.
func sendEmail(args map[string]any) {
	to, _ := args["to"].(string)
	subject, _ := args["subject"].(string)
	body, _ := args["body"].(string)
	if to == "" || subject == "" || body == "" {
		writeError("connector_runtime_error", "send_email: to, subject, and body are required")
		return
	}
	cc, _ := args["cc"].(string)
	bcc, _ := args["bcc"].(string)

	rfc2822 := buildRFC2822(to, cc, bcc, subject, body)
	encoded := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(rfc2822))

	reqBody, err := json.Marshal(map[string]any{"raw": encoded})
	if err != nil {
		writeError("connector_runtime_error", "send_email: encode request: "+err.Error())
		return
	}

	target := "https://gmail.googleapis.com/gmail/v1/users/me/messages/send"
	respBody, status, err := doAuthenticatedJSON("POST", target, reqBody)
	if err != nil {
		writeError("connector_runtime_error", "send_email: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Gmail API returned %d: %s", status, string(respBody)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		writeError("connector_runtime_error", "send_email: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// sendDraft dispatches an existing Gmail draft via users.drafts.send.
//
//	POST https://gmail.googleapis.com/gmail/v1/users/me/drafts/send
//	Body: {"id": "<draftId>"}
//
// Unlike send_email this op takes no body — the draft already carries
// its recipients, subject, and body in Gmail. Useful for the
// draft-then-send flow where an agent (or the user) drafted earlier
// and now wants to dispatch without reconstructing the RFC 2822
// payload. Uses the same gmail.compose scope as draft_email; no scope
// expansion.
//
// Args:
//
//	draft_id  (string, required) — Gmail draft id, as returned by
//	          draft_email in the response (`id` field on the draft).
//
// Output: the sent message's API representation (id, threadId, labelIds).
//
// NOTE: NOT idempotent. The action manifest MUST declare
// [[execute]].idempotent = false AND [approval].required = true — same
// reasoning as send_email: dispatched mail is not recoverable, and the
// runtime retry layer must not double-send on transient failure.
func sendDraft(args map[string]any) {
	draftID, _ := args["draft_id"].(string)
	if draftID == "" {
		writeError("connector_runtime_error", "send_draft: draft_id is required")
		return
	}

	reqBody, err := json.Marshal(map[string]any{"id": draftID})
	if err != nil {
		writeError("connector_runtime_error", "send_draft: encode request: "+err.Error())
		return
	}

	target := "https://gmail.googleapis.com/gmail/v1/users/me/drafts/send"
	respBody, status, err := doAuthenticatedJSON("POST", target, reqBody)
	if err != nil {
		writeError("connector_runtime_error", "send_draft: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Gmail API returned %d: %s", status, string(respBody)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		writeError("connector_runtime_error", "send_draft: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// createCalendarEvent inserts a Calendar event via events.insert.
//
//	POST https://www.googleapis.com/calendar/v3/calendars/{calendarId}/events
//	Body: {"summary": "...", "start": {...}, "end": {...}, ...}
//
// Args:
//
//	title       (string, required)  — event title (Calendar's "summary" field).
//	start_time  (string, required)  — RFC3339 timestamp, e.g. "2026-05-04T15:00:00-07:00".
//	end_time    (string, required)  — RFC3339 timestamp.
//	timezone    (string, optional)  — IANA timezone, e.g. "America/New_York".
//	description (string, optional)  — long-form description.
//	location    (string, optional)  — physical or virtual location.
//	attendees   (array of strings or comma-separated string, optional) — email addresses.
//	calendar_id (string, optional)  — defaults to "primary".
//
// Output: the created event's API representation (id, htmlLink, etc.).
//
// NOTE: NOT idempotent. Action manifest MUST set
// [[execute]].idempotent = false (same reasoning as draft_email).
func createCalendarEvent(args map[string]any) {
	title, _ := args["title"].(string)
	startTime, _ := args["start_time"].(string)
	endTime, _ := args["end_time"].(string)
	if title == "" || startTime == "" || endTime == "" {
		writeError("connector_runtime_error", "create_calendar_event: title, start_time, end_time are required")
		return
	}

	calendarID, _ := args["calendar_id"].(string)
	if calendarID == "" {
		calendarID = "primary"
	}
	timezone, _ := args["timezone"].(string)

	startObj := map[string]any{"dateTime": startTime}
	endObj := map[string]any{"dateTime": endTime}
	if timezone != "" {
		startObj["timeZone"] = timezone
		endObj["timeZone"] = timezone
	}

	event := map[string]any{
		"summary": title,
		"start":   startObj,
		"end":     endObj,
	}
	if desc, _ := args["description"].(string); desc != "" {
		event["description"] = desc
	}
	if loc, _ := args["location"].(string); loc != "" {
		event["location"] = loc
	}
	if attendees := normalizeAttendees(args["attendees"]); len(attendees) > 0 {
		list := make([]map[string]any, 0, len(attendees))
		for _, addr := range attendees {
			list = append(list, map[string]any{"email": addr})
		}
		event["attendees"] = list
	}

	reqBody, err := json.Marshal(event)
	if err != nil {
		writeError("connector_runtime_error", "create_calendar_event: encode request: "+err.Error())
		return
	}

	target := "https://www.googleapis.com/calendar/v3/calendars/" + url.PathEscape(calendarID) + "/events"
	respBody, status, err := doAuthenticatedJSON("POST", target, reqBody)
	if err != nil {
		writeError("connector_runtime_error", "create_calendar_event: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Calendar API returned %d: %s", status, string(respBody)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		writeError("connector_runtime_error", "create_calendar_event: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// doAuthenticatedJSON issues an HTTP request with a JSON body via the
// host-import ABI and returns (body, status, err). Used for write ops
// (POST/PUT/DELETE). Read ops use doAuthenticatedGet.
func doAuthenticatedJSON(method, target string, body []byte) ([]byte, int, error) {
	req, err := json.Marshal(map[string]any{
		"method": method,
		"url":    target,
		"headers": map[string]string{
			"Accept":       "application/json",
			"Content-Type": "application/json",
		},
		"body":       string(body),
		"credential": "oauth2",
	})
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request: %w", err)
	}
	rc := hostHTTPRequest(ptr(req), uint32(len(req)))
	if rc != 0 {
		return nil, 0, fmt.Errorf("http_request denied or failed (rc=%d)", rc)
	}
	size := hostHTTPResponseSize()
	if size < 0 {
		return nil, 0, fmt.Errorf("http_response_size returned %d", size)
	}
	respBody := make([]byte, size)
	if size > 0 {
		n := hostHTTPResponseRead(ptr(respBody), uint32(size))
		if n < 0 {
			return nil, 0, fmt.Errorf("http_response_read returned %d", n)
		}
		respBody = respBody[:n]
	}
	return respBody, int(hostHTTPResponseStatus()), nil
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

func writeOutput(out map[string]any) {
	_ = json.NewEncoder(os.Stdout).Encode(output{Output: out})
}

func writeError(class, message string) {
	aileronLog("error", message)
	_ = json.NewEncoder(os.Stdout).Encode(output{Error: &outputError{Class: class, Message: message}})
}
