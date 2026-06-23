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
//	{"op": "get_email", "args": {"id": "19df4136f28569d2", "format": "metadata"}}
//	  → {"output": {"id": "...", "snippet": "...", "payload": {"headers": [...]}, ...}}
//	  with "format": "full" the output also carries a decoded "body" and
//	  "body_mime_type"
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
//	{"op": "get_draft", "args": {"id": "r-12345"}}
//	  → {"output": {"id": "...", "message": {"id": "...", "snippet": "...",
//	      "payload": {"headers": [...]}, ...}}}
//
//	{"op": "list_drafts",
//	 "args": {"query": "subject:invoice", "max_results": 25, "page_token": "..."}}
//	  → {"output": {"drafts": [{"id": "...", "message": {"id": "...", "threadId": "..."}}],
//	      "nextPageToken": "...", "resultSizeEstimate": 42}}
//
//	{"op": "create_calendar_event",
//	 "args": {"title": "...", "start_time": "2026-05-04T15:00:00-07:00",
//	          "end_time": "2026-05-04T16:00:00-07:00",
//	          "attendees": ["alice@example.com"]}}
//	  → {"output": {"id": "...", "htmlLink": "..."}}
//
//	{"op": "search_contacts",
//	 "args": {"query": "alice", "read_mask": "names,emailAddresses,phoneNumbers"}}
//	  → {"output": {"results": [{"person": {"resourceName": "people/c123",
//	      "names": [...], "emailAddresses": [...], ...}}]}}
//
//	{"op": "get_contact",
//	 "args": {"resource_name": "people/c123", "person_fields": "names,birthdays"}}
//	  → {"output": {"resourceName": "people/c123", "names": [...], ...}}
//
//	{"op": "list_contacts",
//	 "args": {"max_results": 100, "page_token": "...",
//	          "sort_order": "LAST_MODIFIED_DESCENDING"}}
//	  → {"output": {"connections": [{"resourceName": "...", ...}],
//	      "nextPageToken": "...", "totalPeople": 42}}
//
//	{"op": "search_files",
//	 "args": {"query": "name contains 'budget' and trashed=false",
//	          "max_results": 25, "order_by": "modifiedTime desc"}}
//	  → {"output": {"files": [{"id": "...", "name": "...",
//	      "mimeType": "...", "modifiedTime": "...", "parents": [...]}],
//	      "nextPageToken": "..."}}
//
//	{"op": "get_file_content",
//	 "args": {"file_id": "...", "export_mime_type": "text/plain"}}
//	  → {"output": {"name": "...", "mimeType": "application/vnd.google-apps.document",
//	      "exportedAs": "text/plain", "content": "..."}}
//
//	{"op": "get_doc_structure", "args": {"document_id": "..."}}
//	  → {"output": {"documentId": "...", "title": "...",
//	      "body": {"content": [...]}, ...}}
//
//	{"op": "create_doc", "args": {"title": "Untitled", "body": "Hello"}}
//	  → {"output": {"documentId": "...", "title": "...", ...}}
//
//	{"op": "upload_file",
//	 "args": {"name": "notes.md", "content": "# Hello\n",
//	          "mime_type": "text/markdown", "parents": "<folderId>"}}
//	  → {"output": {"id": "...", "name": "...", "mimeType": "...", ...}}
//
//	{"op": "update_doc",
//	 "args": {"document_id": "...",
//	          "requests": [{"insertText": {"text": "...",
//	              "location": {"index": 1}}}]}}
//	  → {"output": {"documentId": "...", "replies": [...], ...}}
//
//	{"op": "rename_file",
//	 "args": {"file_id": "...", "new_name": "..."}}
//	  → {"output": {"id": "...", "name": "...", ...}}
//
//	{"op": "move_file",
//	 "args": {"file_id": "...", "add_parents": "<newFolder>",
//	          "remove_parents": "<oldFolder>"}}
//	  → {"output": {"id": "...", "parents": [...], ...}}
//
//	{"op": "get_file_metadata", "args": {"file_id": "..."}}
//	  → {"output": {"id": "...", "name": "...", "mimeType": "...",
//	      "parents": [...], "modifiedTime": "...", "owners": [...],
//	      "webViewLink": "..."}}
//
//	{"error": {"class": "...", "message": "..."}}  on failure
//
// All outbound HTTP is routed through `aileron_host.http_request` with
// `credential: "oauth2"` so the runtime injects the bound bearer token
// host-side. The connector never holds the OAuth token.
//
// Idempotency: the read ops (list_*, get_email, get_draft, list_drafts,
// search_contacts, get_contact, list_contacts, search_files,
// get_file_content, get_doc_structure) are idempotent by their HTTP
// shape (GET). The Drive housekeeping ops rename_file and move_file are
// natural-idempotent in their effect (renaming/moving to a target
// already-equal state is a no-op against Drive). The other write ops
// (draft_email, send_email, send_draft, create_calendar_event,
// create_doc, upload_file, update_doc) are NOT idempotent — repeating
// them creates duplicate drafts/messages/events/docs/files, or applies
// the same edit twice. Action manifests using non-idempotent ops MUST
// set [[execute]].idempotent = false so the gateway's retry layer
// (ADR-0010) does not double-write. send_email, send_draft, and
// move_file additionally require per-call user approval at the action
// layer (see the respective action.md files) — the mail ops because
// dispatched mail is not recoverable, and move_file because moving a
// file changes folder-inherited permissions, which is third-party
// observable to collaborators.
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
	"strings"
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
	case "get_draft":
		getDraft(in.Args)
	case "list_drafts":
		listDrafts(in.Args)
	case "create_calendar_event":
		createCalendarEvent(in.Args)
	case "search_contacts":
		searchContacts(in.Args)
	case "get_contact":
		getContact(in.Args)
	case "list_contacts":
		listContacts(in.Args)
	case "search_files":
		searchFiles(in.Args)
	case "get_file_content":
		getFileContent(in.Args)
	case "get_doc_structure":
		getDocStructure(in.Args)
	case "create_doc":
		createDoc(in.Args)
	case "upload_file":
		uploadFile(in.Args)
	case "update_doc":
		updateDoc(in.Args)
	case "rename_file":
		renameFile(in.Args)
	case "move_file":
		moveFile(in.Args)
	case "get_file_metadata":
		getFileMetadata(in.Args)
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
// message id. The `format` arg gates how much the call fetches:
//
//	GET https://gmail.googleapis.com/gmail/v1/users/me/messages/{id}?format=metadata
//	GET https://gmail.googleapis.com/gmail/v1/users/me/messages/{id}?format=full
//
// format=metadata (the default) returns headers (Subject, From, To, Date,
// etc.), labelIds, snippet (~200-char body preview), and internalDate
// without fetching the full MIME body. That is the right call cost /
// utility trade-off for "summarize my recent emails" and "what does this
// email say at a glance" agent flows: ten metadata get_email calls cost
// about as much Gmail quota as one list_recent_emails.
//
// format=full additionally fetches the complete MIME payload and the
// connector walks it to decode the message body — preferring the
// text/plain part, falling back to text/html — exposing it as a top-level
// `body` field (and `body_mime_type`) on the output. This is opt-in so the
// cheap default path is unchanged. Attachment bytes are out of scope.
//
// Args:
//
//	id      (string, required) — Gmail message id, as returned by
//	        `list_recent_emails` in `messages[].id`.
//	format  (string, optional) — "metadata" (default) or "full".
//	        Unrecognized values fall back to "metadata".
//
// Output: the parsed users.messages.get response. The agent typically
// reads `payload.headers[]` and `snippet`; with format=full it also reads
// the decoded `body`.
func getEmail(args map[string]any) {
	id, _ := args["id"].(string)
	if id == "" {
		writeError("connector_runtime_error", "get_email: id is required")
		return
	}
	format := normalizeEmailFormat(args["format"])
	q := url.Values{}
	q.Set("format", format)
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
	if format == "full" {
		if payload, ok := parsed["payload"].(map[string]any); ok {
			if decoded, mimeType := extractEmailBody(payload); mimeType != "" {
				parsed["body"] = decoded
				parsed["body_mime_type"] = mimeType
			}
		}
	}
	writeOutput(parsed)
}

// listUpcomingEvents calls Calendar's events.list endpoint.
//
//	GET https://www.googleapis.com/calendar/v3/calendars/{calendarId}/events
//	    ?maxResults={n}&timeMin={t}&timeMax={t}&singleEvents=true&orderBy=startTime
//
// Args:
//
//	calendar_id  (string, optional) — calendar id; default "primary".
//	max_results  (number, optional) — page size cap; default 10.
//	time_min     (string, optional) — RFC3339 lower bound; overrides the
//	             "now" default, e.g. to ask for a window that starts on a
//	             specific day.
//	time_max     (string, optional) — RFC3339 upper bound; when set, caps
//	             the window so unbounded far-future recurring expansions
//	             (yearly birthdays, etc.) don't crowd out the result page.
//
// Output: the raw Calendar events.list JSON. timeMin defaults to "now"
// (so the agent gets upcoming events) unless time_min is supplied;
// singleEvents=true expands recurring events; orderBy=startTime returns
// chronological order.
func listUpcomingEvents(args map[string]any) {
	calendarID, _ := args["calendar_id"].(string)
	if calendarID == "" {
		calendarID = "primary"
	}
	maxResults := readMaxResults(args, 10)

	timeMin, _ := args["time_min"].(string)
	if timeMin == "" {
		timeMin = time.Now().UTC().Format(time.RFC3339)
	}
	timeMax, _ := args["time_max"].(string)

	target := buildListEventsURL(calendarID, timeMin, timeMax, maxResults)

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

// replyContext carries the data needed to nest a new message inside an
// existing Gmail thread, derived from the original message identified by
// in_reply_to_message_id.
type replyContext struct {
	threadID   string // drafts.create / messages.send threadId
	inReplyTo  string // In-Reply-To header value (original Message-ID)
	references string // References header value (chain + Message-ID)
}

// fetchReplyContext GETs the original message (format=metadata) and
// extracts the threading data needed to thread a reply. It is only
// called when in_reply_to_message_id is supplied; when absent, the
// draft/send paths skip this entirely and behave byte-for-byte as
// before. On any failure it returns a non-nil error string suitable for
// surfacing via writeError under the calling op's name.
//
// format=metadata returns payload.headers[] (carrying Message-ID and
// References) plus the top-level threadId — everything needed to
// position the reply — without paying for the full MIME body.
func fetchReplyContext(op, messageID string) (*replyContext, string) {
	q := url.Values{}
	q.Set("format", "metadata")
	target := "https://gmail.googleapis.com/gmail/v1/users/me/messages/" +
		url.PathEscape(messageID) + "?" + q.Encode()

	body, status, err := doAuthenticatedGet(target)
	if err != nil {
		return nil, op + ": fetch in_reply_to_message_id: " + err.Error()
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Sprintf("%s: fetching in_reply_to_message_id, Gmail API returned %d: %s", op, status, string(body))
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, op + ": parse in_reply_to_message_id: " + err.Error()
	}

	rc := &replyContext{}
	rc.threadID, _ = parsed["threadId"].(string)
	if payload, ok := parsed["payload"].(map[string]any); ok {
		if headers, ok := payload["headers"].([]any); ok {
			rc.inReplyTo, rc.references = replyThreadingHeaders(headers)
		}
	}
	return rc, ""
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
//	in_reply_to_message_id (string, optional) — a Gmail message id (as
//	         returned by list_recent_emails / get_email). When set, the
//	         draft is nested inside that message's thread: the connector
//	         fetches the original to read its Message-ID, References
//	         chain and threadId, then writes In-Reply-To / References
//	         headers, prefixes "Re: " on the subject when not already
//	         present, and sets threadId on the draft. When absent the
//	         request is byte-for-byte unchanged.
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
	inReplyToID, _ := args["in_reply_to_message_id"].(string)
	if to == "" || subject == "" || body == "" {
		writeError("connector_runtime_error", "draft_email: to, subject, and body are required")
		return
	}
	cc, _ := args["cc"].(string)
	bcc, _ := args["bcc"].(string)

	var inReplyTo, references, threadID string
	if inReplyToID != "" {
		rc, errMsg := fetchReplyContext("draft_email", inReplyToID)
		if errMsg != "" {
			writeError("external_api_error", errMsg)
			return
		}
		inReplyTo, references, threadID = rc.inReplyTo, rc.references, rc.threadID
		subject = ensureRePrefix(subject)
	}

	rfc2822 := buildRFC2822Reply(to, cc, bcc, subject, body, inReplyTo, references)
	encoded := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(rfc2822))

	message := map[string]any{"raw": encoded}
	if threadID != "" {
		message["threadId"] = threadID
	}
	reqBody, err := json.Marshal(map[string]any{
		"message": message,
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
//	in_reply_to_message_id (string, optional) — a Gmail message id (as
//	         returned by list_recent_emails / get_email). When set, the
//	         sent message is nested inside that message's thread: the
//	         connector fetches the original to read its Message-ID,
//	         References chain and threadId, then writes In-Reply-To /
//	         References headers, prefixes "Re: " on the subject when not
//	         already present, and sets threadId on the send. When absent
//	         the request is byte-for-byte unchanged.
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
	inReplyToID, _ := args["in_reply_to_message_id"].(string)
	if to == "" || subject == "" || body == "" {
		writeError("connector_runtime_error", "send_email: to, subject, and body are required")
		return
	}
	cc, _ := args["cc"].(string)
	bcc, _ := args["bcc"].(string)

	var inReplyTo, references, threadID string
	if inReplyToID != "" {
		rc, errMsg := fetchReplyContext("send_email", inReplyToID)
		if errMsg != "" {
			writeError("external_api_error", errMsg)
			return
		}
		inReplyTo, references, threadID = rc.inReplyTo, rc.references, rc.threadID
		subject = ensureRePrefix(subject)
	}

	rfc2822 := buildRFC2822Reply(to, cc, bcc, subject, body, inReplyTo, references)
	encoded := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(rfc2822))

	sendReq := map[string]any{"raw": encoded}
	if threadID != "" {
		sendReq["threadId"] = threadID
	}
	reqBody, err := json.Marshal(sendReq)
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

// getDraft calls Gmail's users.drafts.get endpoint for a single draft
// id with format=metadata.
//
//	GET https://gmail.googleapis.com/gmail/v1/users/me/drafts/{id}?format=metadata
//
// The response wraps a message: `{id, message: {id, threadId,
// labelIds, snippet, payload: {headers: [...]}, ...}}`. `format=metadata`
// returns headers (Subject, From, To, Date, etc.) and a ~200-char
// snippet without fetching the full MIME body — the same cost/utility
// trade-off as get_email.
//
// Primary use: feeding the send_draft approval prompt so the user sees
// what they are about to dispatch (To / Subject / snippet) rather than
// an opaque draft id. Idempotent and read-only — safe for the runtime
// to call pre-approval.
//
// Args:
//
//	id  (string, required) — Gmail draft id, as returned by
//	    `draft_email` in the response's `id` field.
//
// Output: the parsed users.drafts.get response. Subject/From/To live
// at `message.payload.headers[]`; snippet at `message.snippet`.
func getDraft(args map[string]any) {
	id, _ := args["id"].(string)
	if id == "" {
		writeError("connector_runtime_error", "get_draft: id is required")
		return
	}
	q := url.Values{}
	q.Set("format", "metadata")
	target := "https://gmail.googleapis.com/gmail/v1/users/me/drafts/" +
		url.PathEscape(id) + "?" + q.Encode()

	body, status, err := doAuthenticatedGet(target)
	if err != nil {
		writeError("connector_runtime_error", "get_draft: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Gmail API returned %d: %s", status, string(body)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeError("connector_runtime_error", "get_draft: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// listDrafts calls Gmail's users.drafts.list endpoint.
//
//	GET https://gmail.googleapis.com/gmail/v1/users/me/drafts?q={query}&maxResults={n}&pageToken={token}
//
// Output mirrors users.drafts.list: a list of `{id, message: {id, threadId}}`
// pairs plus `nextPageToken` and `resultSizeEstimate`. Like list_recent_emails,
// the response carries only ids — pair with get_draft to resolve headers and
// snippets, then send_draft to dispatch. The two-call composition keeps the
// surface predictable; an aggregated convenience action can be added later if
// usage shows it is needed.
//
// Args:
//
//	query        (string, optional) — Gmail search query, e.g. "subject:invoice".
//	                                  Passed through to the API's `q` parameter.
//	max_results  (number, optional) — page size cap; default 10, capped at 100.
//	page_token   (string, optional) — continuation token from a prior call's
//	                                  `nextPageToken`.
//
// Idempotent by HTTP shape (GET); the action manifest sets
// [[execute]].idempotent = true so the gateway's retry layer (ADR-0010) may
// safely re-issue on transient failure. No approval gate — listing drafts is
// strictly read-only and does not mutate Gmail state.
func listDrafts(args map[string]any) {
	query, _ := args["query"].(string)
	maxResults := readMaxResults(args, 10)
	pageToken, _ := args["page_token"].(string)

	target := buildListDraftsURL(query, maxResults, pageToken)

	body, status, err := doAuthenticatedGet(target)
	if err != nil {
		writeError("connector_runtime_error", "list_drafts: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Gmail API returned %d: %s", status, string(body)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeError("connector_runtime_error", "list_drafts: parse: "+err.Error())
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

// searchContacts calls the People API's people:searchContacts endpoint.
//
//	GET https://people.googleapis.com/v1/people:searchContacts
//	    ?query={query}&readMask={fields}&pageSize={n}
//
// Args:
//
//	query        (string, required) — search string matched against
//	             names, emails, phone numbers, organizations, and other
//	             searchable fields on the user's contacts.
//	read_mask    (string, optional) — comma-separated person fields to
//	             return. Default: "names,emailAddresses,phoneNumbers,birthdays".
//	             The API requires at least one field; the connector
//	             substitutes the default before dispatching.
//	max_results  (number, optional) — page size cap. Default 10; the
//	             People API itself caps this op at 30 and the connector
//	             clamps to that ceiling so over-eager callers get
//	             results instead of an HTTP 400.
//
// Output: the raw `people:searchContacts` response. Results live at
// `results[].person`; each person carries the fields named in readMask.
//
// People API quirk: Google recommends warming the search cache by
// issuing an empty-query request first so freshly-modified contacts
// appear. The connector intentionally skips that — the WASM sandbox
// is stateless per invocation so a per-call warm-up would double the
// quota cost of every search, and the dominant use case here is
// reading existing contacts where the cache is already populated. If
// a caller hits stale results, re-running the search after a moment
// is the documented mitigation.
func searchContacts(args map[string]any) {
	query, _ := args["query"].(string)
	if query == "" {
		writeError("connector_runtime_error", "search_contacts: query is required")
		return
	}
	readMask, _ := args["read_mask"].(string)
	if readMask == "" {
		readMask = "names,emailAddresses,phoneNumbers,birthdays"
	}
	pageSize := readMaxResults(args, 10)

	target := buildSearchContactsURL(query, readMask, pageSize)

	body, status, err := doAuthenticatedGet(target)
	if err != nil {
		writeError("connector_runtime_error", "search_contacts: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("People API returned %d: %s", status, string(body)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeError("connector_runtime_error", "search_contacts: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// getContact calls the People API's people.get endpoint for a single
// contact identified by its resource_name (e.g. "people/c123456789").
//
//	GET https://people.googleapis.com/v1/{resourceName}?personFields={fields}
//
// `resource_name` is the full Google-issued identifier as returned by
// search_contacts / list_contacts (the `resourceName` field on each
// person). It is intentionally NOT a bare numeric id — the People API
// uses the full "people/<id>" form in its URLs and we pass it through
// to avoid an asymmetric encode/decode step.
//
// Args:
//
//	resource_name (string, required) — must start with "people/" and
//	              carry the Google-issued id; rejected otherwise. The
//	              id portion is url.PathEscaped before substitution
//	              into the URL so an attacker-controlled value cannot
//	              inject query params or escape the path.
//	person_fields (string, optional) — comma-separated fields to
//	              return. Default: a wider set than search_contacts
//	              ("names,emailAddresses,phoneNumbers,birthdays,addresses,
//	              organizations,biographies,urls") because get_contact
//	              is the "drill into one record" call where the cost of
//	              extra fields is bounded to a single API hit.
//
// Output: the raw People API person response. Fields not requested in
// person_fields are omitted by the API.
func getContact(args map[string]any) {
	resourceName, _ := args["resource_name"].(string)
	const prefix = "people/"
	if resourceName == "" || len(resourceName) <= len(prefix) || resourceName[:len(prefix)] != prefix {
		writeError("connector_runtime_error", "get_contact: resource_name must be of the form \"people/<id>\"")
		return
	}
	id := resourceName[len(prefix):]
	personFields, _ := args["person_fields"].(string)
	if personFields == "" {
		personFields = "names,emailAddresses,phoneNumbers,birthdays,addresses,organizations,biographies,urls"
	}

	q := url.Values{}
	q.Set("personFields", personFields)
	target := "https://people.googleapis.com/v1/people/" + url.PathEscape(id) + "?" + q.Encode()

	body, status, err := doAuthenticatedGet(target)
	if err != nil {
		writeError("connector_runtime_error", "get_contact: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("People API returned %d: %s", status, string(body)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeError("connector_runtime_error", "get_contact: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// listContacts calls the People API's people.connections.list endpoint
// for the authenticated user.
//
//	GET https://people.googleapis.com/v1/people/me/connections
//	    ?personFields={fields}&pageSize={n}&pageToken={t}&sortOrder={s}
//
// Args:
//
//	person_fields (string, optional) — comma-separated fields. Default:
//	              "names,emailAddresses,phoneNumbers". Kept lean here
//	              because list responses can carry hundreds of records;
//	              callers can pair with get_contact to fetch a wider
//	              field set per record.
//	max_results   (number, optional) — page size; default 100. The
//	              People API allows up to 1000 but the connector's
//	              readMaxResults caps at 100 to keep quota usage
//	              predictable; users who want more should paginate.
//	page_token    (string, optional) — continuation token from a prior
//	              call's `nextPageToken`. Pass to fetch the next page.
//	sort_order    (string, optional) — one of LAST_MODIFIED_ASCENDING,
//	              LAST_MODIFIED_DESCENDING, FIRST_NAME_ASCENDING,
//	              LAST_NAME_ASCENDING. Unknown values surface as the
//	              API's HTTP 400 rather than being pre-validated here
//	              — same pass-through posture as Gmail's `q` parameter.
//
// Output: the raw `people/me/connections` response: `{connections: [...],
// nextPageToken, totalPeople, totalItems}`.
func listContacts(args map[string]any) {
	personFields, _ := args["person_fields"].(string)
	if personFields == "" {
		personFields = "names,emailAddresses,phoneNumbers"
	}
	pageSize := readMaxResults(args, 100)
	pageToken, _ := args["page_token"].(string)
	sortOrder, _ := args["sort_order"].(string)

	target := buildListContactsURL(personFields, pageSize, pageToken, sortOrder)

	body, status, err := doAuthenticatedGet(target)
	if err != nil {
		writeError("connector_runtime_error", "list_contacts: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("People API returned %d: %s", status, string(body)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeError("connector_runtime_error", "list_contacts: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// searchFiles calls Drive's files.list endpoint.
//
//	GET https://www.googleapis.com/drive/v3/files
//	    ?q={query}&pageSize={n}&fields={mask}&orderBy={...}&pageToken={t}
//
// `q` follows Drive's query language: e.g. "name contains 'budget'",
// "mimeType='application/vnd.google-apps.document'",
// "modifiedTime > '2026-01-01T00:00:00Z'", "'folderId' in parents",
// "trashed=false". Combine with " and " / " or ". Pass-through to the
// API — invalid queries surface as HTTP 400 rather than being
// pre-validated here, same posture as Gmail's `q` parameter.
//
// Args:
//
//	query        (string, optional) — Drive query expression. Omit for
//	             "list all (non-trashed) files".
//	max_results  (number, optional) — page size; default 25, capped at
//	             100 by readMaxResults to keep responses bounded.
//	order_by     (string, optional) — e.g. "modifiedTime desc",
//	             "name", "createdTime desc". Multiple keys comma-
//	             separated.
//	page_token   (string, optional) — continuation token from a prior
//	             call's `nextPageToken`.
//	fields       (string, optional) — Drive partial-response field
//	             mask. Default is a lean set
//	             (id,name,mimeType,modifiedTime,parents,webViewLink);
//	             override for owners, capabilities, etc.
//
// Output: the raw files.list response: `{files: [...], nextPageToken}`.
func searchFiles(args map[string]any) {
	query, _ := args["query"].(string)
	maxResults := readMaxResults(args, 25)
	orderBy, _ := args["order_by"].(string)
	pageToken, _ := args["page_token"].(string)
	fields, _ := args["fields"].(string)

	target := buildSearchFilesURL(query, fields, orderBy, pageToken, maxResults)

	body, status, err := doAuthenticatedGet(target)
	if err != nil {
		writeError("connector_runtime_error", "search_files: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Drive API returned %d: %s", status, string(body)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeError("connector_runtime_error", "search_files: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// getFileContent fetches a Drive file's content and returns it as text.
//
// Two API hits per call:
//
//	1. GET drive/v3/files/{id}?fields=name,mimeType  — identify the file
//	2a. GET drive/v3/files/{id}/export?mimeType={target}  — for native
//	    Google types (Docs / Sheets / Slides) where there is no byte
//	    stream; export converts to the requested text format.
//	2b. GET drive/v3/files/{id}?alt=media             — for non-native
//	    files with a downloadable byte stream.
//
// v1 returns content as a JSON string and so only supports text-shaped
// mimeTypes. Native Google types are exported to text/plain (Docs,
// Slides) or text/csv (Sheets) by default — callers can override via
// `export_mime_type` for, e.g., text/html. Non-native files must have
// a text/* (or a small allowlist of application/* text-shaped) mime;
// binary files (PDFs, images, archives) are rejected with a clear
// error. Binary support is a follow-up that needs a host-ABI binary-
// body field — see helpers.go's isTextLikeMIME comment for the
// trade-off.
//
// Args:
//
//	file_id          (string, required) — Drive file id, as returned by
//	                 search_files in `files[].id`.
//	export_mime_type (string, optional) — override the default text
//	                 export target for native Google types. Examples:
//	                 "text/html", "application/rtf",
//	                 "application/vnd.openxmlformats-officedocument
//	                 .wordprocessingml.document" (DOCX). Note: non-text
//	                 export targets will succeed against Drive but the
//	                 returned content may be corrupted by the JSON-
//	                 string round-trip — text targets are recommended.
//
// Output: `{name, mimeType, exportedAs, content}`. `exportedAs` is set
// to the target mime when an export happened, or to the source mime
// when alt=media was used. `content` is the file's text bytes as a
// string.
func getFileContent(args map[string]any) {
	fileID, _ := args["file_id"].(string)
	if fileID == "" {
		writeError("connector_runtime_error", "get_file_content: file_id is required")
		return
	}
	exportMIME, _ := args["export_mime_type"].(string)

	// Step 1: identify mime type and name.
	metaQ := url.Values{}
	metaQ.Set("fields", "name,mimeType")
	metaTarget := "https://www.googleapis.com/drive/v3/files/" + url.PathEscape(fileID) + "?" + metaQ.Encode()
	metaBody, metaStatus, err := doAuthenticatedGet(metaTarget)
	if err != nil {
		writeError("connector_runtime_error", "get_file_content: metadata: "+err.Error())
		return
	}
	if metaStatus < 200 || metaStatus >= 300 {
		writeError("external_api_error", fmt.Sprintf("Drive API returned %d on metadata fetch: %s", metaStatus, string(metaBody)))
		return
	}
	var meta struct {
		Name     string `json:"name"`
		MimeType string `json:"mimeType"`
	}
	if err := json.Unmarshal(metaBody, &meta); err != nil {
		writeError("connector_runtime_error", "get_file_content: parse metadata: "+err.Error())
		return
	}

	// Step 2: branch on native vs. binary-on-disk.
	var contentURL, returnedMIME string
	if isGoogleNativeMIME(meta.MimeType) {
		target := exportMIME
		if target == "" {
			target = defaultExportMIME(meta.MimeType)
		}
		if target == "" {
			writeError("connector_runtime_error", fmt.Sprintf("get_file_content: no default text export for %s; pass export_mime_type explicitly if a text-compatible target exists", meta.MimeType))
			return
		}
		exportQ := url.Values{}
		exportQ.Set("mimeType", target)
		contentURL = "https://www.googleapis.com/drive/v3/files/" + url.PathEscape(fileID) + "/export?" + exportQ.Encode()
		returnedMIME = target
	} else {
		if !isTextLikeMIME(meta.MimeType) {
			writeError("connector_runtime_error", fmt.Sprintf("get_file_content: v1 supports text content only; got mimeType=%s. Binary file support is planned (host-ABI binary-body field needed).", meta.MimeType))
			return
		}
		mediaQ := url.Values{}
		mediaQ.Set("alt", "media")
		contentURL = "https://www.googleapis.com/drive/v3/files/" + url.PathEscape(fileID) + "?" + mediaQ.Encode()
		returnedMIME = meta.MimeType
	}

	contentBody, status, err := doAuthenticatedGet(contentURL)
	if err != nil {
		writeError("connector_runtime_error", "get_file_content: download: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Drive API returned %d on content fetch: %s", status, string(contentBody)))
		return
	}

	writeOutput(map[string]any{
		"name":       meta.Name,
		"mimeType":   meta.MimeType,
		"exportedAs": returnedMIME,
		"content":    string(contentBody),
	})
}

// getDocStructure calls the Docs API documents.get endpoint and
// returns the full structured Document JSON — paragraphs, runs,
// styles, ranges, headings — the indexing space that update_doc's
// batchUpdate operations target.
//
//	GET https://docs.googleapis.com/v1/documents/{documentId}
//	    [?suggestionsViewMode=...]
//
// Pair with get_file_content for human-readable text and with
// update_doc for precise edits: text export tells the agent *what* the
// document says, structure tells the agent *where* every character
// lives in Docs' index space so insertText / replaceAllText /
// deleteContentRange can target it accurately.
//
// Args:
//
//	document_id            (string, required) — Docs document id.
//	suggestions_view_mode  (string, optional) — one of
//	                       DEFAULT_FOR_CURRENT_ACCESS,
//	                       SUGGESTIONS_INLINE,
//	                       PREVIEW_SUGGESTIONS_ACCEPTED,
//	                       PREVIEW_WITHOUT_SUGGESTIONS. Defaults to the
//	                       Docs API default. SUGGESTIONS_INLINE is the
//	                       right pick when an update_doc batchUpdate
//	                       needs index correctness over a document that
//	                       carries existing suggestions.
//
// Output: the raw documents.get response. The agent typically reads
// `body.content[]` (a sequence of StructuralElement objects: paragraph,
// table, sectionBreak, tableOfContents) and indexes via each element's
// `startIndex` / `endIndex`.
func getDocStructure(args map[string]any) {
	documentID, _ := args["document_id"].(string)
	if documentID == "" {
		writeError("connector_runtime_error", "get_doc_structure: document_id is required")
		return
	}
	suggestionsViewMode, _ := args["suggestions_view_mode"].(string)

	target := "https://docs.googleapis.com/v1/documents/" + url.PathEscape(documentID)
	if suggestionsViewMode != "" {
		q := url.Values{}
		q.Set("suggestionsViewMode", suggestionsViewMode)
		target += "?" + q.Encode()
	}

	body, status, err := doAuthenticatedGet(target)
	if err != nil {
		writeError("connector_runtime_error", "get_doc_structure: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Docs API returned %d: %s", status, string(body)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeError("connector_runtime_error", "get_doc_structure: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// createDoc creates a new Google Doc via the Docs API documents.create
// endpoint and (optionally) writes initial body content via a
// follow-up batchUpdate insertText.
//
//	POST https://docs.googleapis.com/v1/documents
//	Body: {"title": "..."}
//
// If `body` is provided, a second call inserts the text at index 1
// (immediately after the body's start). The two-call shape is the
// supported path — documents.create itself does not accept body
// content in its request.
//
// Args:
//
//	title  (string, required) — the document title.
//	body   (string, optional) — initial body content (plain text). If
//	       omitted the doc is created empty.
//
// Output: the documents.create response (documentId, title, body, ...).
// On the body-insert variant, the response is updated by a second API
// hit so the returned `body.content` reflects the inserted text.
//
// NOTE: NOT idempotent. The action manifest MUST set
// [[execute]].idempotent = false so the gateway retry layer (ADR-0010)
// does not produce duplicate docs on transient failure. Reversibility
// via Drive trash makes this safe to run without an approval gate.
func createDoc(args map[string]any) {
	title, _ := args["title"].(string)
	if title == "" {
		writeError("connector_runtime_error", "create_doc: title is required")
		return
	}
	initialBody, _ := args["body"].(string)

	createReq, err := json.Marshal(map[string]any{"title": title})
	if err != nil {
		writeError("connector_runtime_error", "create_doc: encode request: "+err.Error())
		return
	}
	createResp, status, err := doAuthenticatedJSON("POST", "https://docs.googleapis.com/v1/documents", createReq)
	if err != nil {
		writeError("connector_runtime_error", "create_doc: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Docs API returned %d on create: %s", status, string(createResp)))
		return
	}
	var doc map[string]any
	if err := json.Unmarshal(createResp, &doc); err != nil {
		writeError("connector_runtime_error", "create_doc: parse: "+err.Error())
		return
	}

	if initialBody == "" {
		writeOutput(doc)
		return
	}

	documentID, _ := doc["documentId"].(string)
	if documentID == "" {
		writeError("connector_runtime_error", "create_doc: response missing documentId; cannot insert body")
		return
	}
	updateReq, err := json.Marshal(map[string]any{
		"requests": []map[string]any{
			{"insertText": map[string]any{
				"text":     initialBody,
				"location": map[string]any{"index": 1},
			}},
		},
	})
	if err != nil {
		writeError("connector_runtime_error", "create_doc: encode update: "+err.Error())
		return
	}
	updateURL := "https://docs.googleapis.com/v1/documents/" + url.PathEscape(documentID) + ":batchUpdate"
	updateResp, status, err := doAuthenticatedJSON("POST", updateURL, updateReq)
	if err != nil {
		writeError("connector_runtime_error", "create_doc: insert body: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Docs API returned %d on body insert: %s", status, string(updateResp)))
		return
	}

	// Re-fetch so the response reflects the inserted body. Cheap and
	// keeps the contract clean: callers always get a populated doc on
	// success.
	getResp, getStatus, err := doAuthenticatedGet("https://docs.googleapis.com/v1/documents/" + url.PathEscape(documentID))
	if err != nil {
		writeError("connector_runtime_error", "create_doc: refetch: "+err.Error())
		return
	}
	if getStatus < 200 || getStatus >= 300 {
		writeError("external_api_error", fmt.Sprintf("Docs API returned %d on refetch: %s", getStatus, string(getResp)))
		return
	}
	var refreshed map[string]any
	if err := json.Unmarshal(getResp, &refreshed); err != nil {
		writeError("connector_runtime_error", "create_doc: parse refetch: "+err.Error())
		return
	}
	writeOutput(refreshed)
}

// uploadFile uploads a new file to Drive via the multipart upload
// endpoint.
//
//	POST https://www.googleapis.com/upload/drive/v3/files?uploadType=multipart
//	Content-Type: multipart/related; boundary=<...>
//	Body:  metadata JSON part  +  content part
//
// v1 supports UTF-8 text content only — see helpers.go's
// buildMultipartUpload comment for the host-ABI binary-body limitation.
// For Google Docs, prefer create_doc (which uses the Docs API
// documents.create endpoint and gives a proper Docs document_id back).
//
// Args:
//
//	name       (string, required) — file name (the Drive display name).
//	content    (string, required) — UTF-8 text content.
//	mime_type  (string, optional) — content mimeType; default text/plain.
//	parents    (string|array, optional) — parent folder id(s). Comma-
//	           separated string or JSON array. Omit to land in the
//	           user's My Drive root.
//
// Output: the created file's Drive metadata (id, name, mimeType, ...).
//
// NOTE: NOT idempotent. Manifest MUST set [[execute]].idempotent =
// false. Reversibility via Drive trash makes this safe without an
// approval gate.
func uploadFile(args map[string]any) {
	name, _ := args["name"].(string)
	content, _ := args["content"].(string)
	if name == "" || content == "" {
		writeError("connector_runtime_error", "upload_file: name and content are required")
		return
	}
	mimeType, _ := args["mime_type"].(string)
	if mimeType == "" {
		mimeType = "text/plain"
	}
	parents := driveParentsList(args["parents"])

	metadata := map[string]any{
		"name":     name,
		"mimeType": mimeType,
	}
	if parents != "" {
		metadata["parents"] = strings.Split(parents, ",")
	}

	body, contentType, err := buildMultipartUpload(metadata, mimeType, []byte(content))
	if err != nil {
		writeError("connector_runtime_error", "upload_file: build multipart: "+err.Error())
		return
	}

	target := "https://www.googleapis.com/upload/drive/v3/files?uploadType=multipart"
	respBody, status, err := doAuthenticatedUpload("POST", target, contentType, body)
	if err != nil {
		writeError("connector_runtime_error", "upload_file: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Drive API returned %d: %s", status, string(respBody)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		writeError("connector_runtime_error", "upload_file: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// updateDoc applies a batch of structured edits to a Google Doc via
// the Docs API documents.batchUpdate endpoint.
//
//	POST https://docs.googleapis.com/v1/documents/{documentId}:batchUpdate
//	Body: {"requests": [...]}
//
// `requests` is passed through directly to the Docs API — the agent
// constructs the individual Request objects (insertText,
// replaceAllText, deleteContentRange, updateTextStyle,
// updateParagraphStyle, insertTable, insertInlineImage, etc.). See
// the Docs API reference for the full Request type union.
//
// Pair with get_doc_structure to know the index space, then issue
// updates against the indices it returned. The Docs API processes
// requests in order and computes index shifts between them; constructing
// requests left-to-right (or descending end-index for safety against
// shift drift) is the conventional pattern.
//
// Edits are NOT gated behind an approval block: Docs revisions make
// every edit reversible from the owner's version history. The action
// manifest still sets idempotent = false because re-running the same
// batch (e.g. an insertText) applies the edit twice.
//
// Args:
//
//	document_id  (string, required) — Docs document id.
//	requests     (array, required)  — Docs API Request objects.
//
// Output: the documents.batchUpdate response — `documentId`,
// `replies` (parallel to requests; carries created object ids for
// insert requests), `writeControl`.
func updateDoc(args map[string]any) {
	documentID, _ := args["document_id"].(string)
	if documentID == "" {
		writeError("connector_runtime_error", "update_doc: document_id is required")
		return
	}
	requests, ok := args["requests"].([]any)
	if !ok || len(requests) == 0 {
		writeError("connector_runtime_error", "update_doc: requests is required and must be a non-empty array")
		return
	}

	reqBody, err := json.Marshal(map[string]any{"requests": requests})
	if err != nil {
		writeError("connector_runtime_error", "update_doc: encode request: "+err.Error())
		return
	}
	target := "https://docs.googleapis.com/v1/documents/" + url.PathEscape(documentID) + ":batchUpdate"
	respBody, status, err := doAuthenticatedJSON("POST", target, reqBody)
	if err != nil {
		writeError("connector_runtime_error", "update_doc: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Docs API returned %d: %s", status, string(respBody)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		writeError("connector_runtime_error", "update_doc: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// renameFile renames a Drive file via files.update (HTTP PATCH).
//
//	PATCH https://www.googleapis.com/drive/v3/files/{id}
//	Body: {"name": "<new>"}
//
// Idempotent in effect: renaming a file to a name it already has is a
// no-op against Drive. Action manifest sets idempotent = true.
//
// Args:
//
//	file_id   (string, required) — Drive file id.
//	new_name  (string, required) — new display name.
func renameFile(args map[string]any) {
	fileID, _ := args["file_id"].(string)
	newName, _ := args["new_name"].(string)
	if fileID == "" || newName == "" {
		writeError("connector_runtime_error", "rename_file: file_id and new_name are required")
		return
	}

	reqBody, err := json.Marshal(map[string]any{"name": newName})
	if err != nil {
		writeError("connector_runtime_error", "rename_file: encode request: "+err.Error())
		return
	}
	target := "https://www.googleapis.com/drive/v3/files/" + url.PathEscape(fileID)
	respBody, status, err := doAuthenticatedJSON("PATCH", target, reqBody)
	if err != nil {
		writeError("connector_runtime_error", "rename_file: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Drive API returned %d: %s", status, string(respBody)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		writeError("connector_runtime_error", "rename_file: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// moveFile changes a Drive file's parent folder(s) via files.update
// with addParents / removeParents query params.
//
//	PATCH https://www.googleapis.com/drive/v3/files/{id}
//	      ?addParents=<id>&removeParents=<id>&fields=id,name,parents
//	Body: {}
//
// Per Drive's documented contract, parent changes go through the
// query params (not the body). The empty JSON body still requires
// Content-Type: application/json — the host helper sets it.
//
// Approval-gated at the action layer (ADR-0009): moving a file
// between folders changes its inherited sharing permissions, which
// is third-party-observable to collaborators in the same way send_email
// is observable. Reversibility via "move back" is not enough — once
// a collaborator has opened the file from a folder they were given
// access to, the act of granting (or revoking) that access has
// already happened on Drive's side.
//
// Idempotent in effect (re-issuing with the same addParents /
// removeParents leaves Drive in the same state).
//
// Args:
//
//	file_id         (string, required)        — Drive file id.
//	add_parents     (string|array, required)  — folder id(s) to add as
//	                parents. Comma-separated string or JSON array.
//	remove_parents  (string|array, optional)  — folder id(s) to remove.
//	                Typically the current parent; omit for "add as a
//	                multi-parent" semantics.
func moveFile(args map[string]any) {
	fileID, _ := args["file_id"].(string)
	if fileID == "" {
		writeError("connector_runtime_error", "move_file: file_id is required")
		return
	}
	addParents := driveParentsList(args["add_parents"])
	if addParents == "" {
		writeError("connector_runtime_error", "move_file: add_parents is required (comma-separated string or JSON array)")
		return
	}
	removeParents := driveParentsList(args["remove_parents"])

	q := url.Values{}
	q.Set("addParents", addParents)
	if removeParents != "" {
		q.Set("removeParents", removeParents)
	}
	q.Set("fields", "id,name,parents")
	target := "https://www.googleapis.com/drive/v3/files/" + url.PathEscape(fileID) + "?" + q.Encode()

	respBody, status, err := doAuthenticatedJSON("PATCH", target, []byte("{}"))
	if err != nil {
		writeError("connector_runtime_error", "move_file: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Drive API returned %d: %s", status, string(respBody)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		writeError("connector_runtime_error", "move_file: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// getFileMetadata calls Drive's files.get for a single file id with a
// default fields mask. Used as a standalone read ("what is file X?")
// and as the authoritative source behind move_file's approval preview
// — mirrors the get_draft / send_draft pairing where the preview op
// fetches data straight from the API rather than trusting agent-
// supplied args (ADR-0009 / ADR-0016).
//
//	GET https://www.googleapis.com/drive/v3/files/{id}
//	    ?fields=id,name,mimeType,parents,modifiedTime,owners,webViewLink
//
// Args:
//
//	file_id  (string, required) — Drive file id.
//	fields   (string, optional) — Drive partial-response field mask.
//	         Default includes id, name, mimeType, parents,
//	         modifiedTime, owners, webViewLink — the set needed for a
//	         meaningful approval card and most "what is this?" reads.
//
// Output: the raw files.get response.
func getFileMetadata(args map[string]any) {
	fileID, _ := args["file_id"].(string)
	if fileID == "" {
		writeError("connector_runtime_error", "get_file_metadata: file_id is required")
		return
	}
	fields, _ := args["fields"].(string)
	if fields == "" {
		fields = "id,name,mimeType,parents,modifiedTime,owners,webViewLink"
	}
	q := url.Values{}
	q.Set("fields", fields)
	target := "https://www.googleapis.com/drive/v3/files/" + url.PathEscape(fileID) + "?" + q.Encode()

	body, status, err := doAuthenticatedGet(target)
	if err != nil {
		writeError("connector_runtime_error", "get_file_metadata: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		writeError("external_api_error", fmt.Sprintf("Drive API returned %d: %s", status, string(body)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeError("connector_runtime_error", "get_file_metadata: parse: "+err.Error())
		return
	}
	writeOutput(parsed)
}

// doAuthenticatedUpload is doAuthenticatedJSON's sibling for non-JSON
// request bodies (multipart/related uploads). Same host-ABI shape and
// credential injection as the JSON variant; the only difference is
// the caller-supplied Content-Type and the relaxed body encoding —
// the multipart body is passed through as a string, which round-trips
// cleanly for UTF-8 text content (the v1 upload_file constraint).
func doAuthenticatedUpload(method, target, contentType string, body []byte) ([]byte, int, error) {
	req, err := json.Marshal(map[string]any{
		"method": method,
		"url":    target,
		"headers": map[string]string{
			"Accept":       "application/json",
			"Content-Type": contentType,
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
