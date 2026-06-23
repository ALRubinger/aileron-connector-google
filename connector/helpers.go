// Pure helpers for the connector — no host-import dependencies, so this
// file builds on every Go target (including the host platform). main.go
// is wasip1-only because it imports the aileron_host module; keeping
// these helpers in a separate, untagged file lets `go test` exercise
// them as ordinary Go unit tests.

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
)

// buildRFC2822 constructs a minimal RFC 2822 / RFC 5322 message that
// Gmail's drafts.create accepts as the `raw` field. Gmail expects the
// body to follow the same wire format an SMTP server would handle:
// CRLF-terminated headers, blank line, body. The Subject header is
// RFC 2047 encoded-word wrapped when it contains non-ASCII (raw 8-bit
// bytes in headers get mis-decoded as Latin-1 by downstream agents,
// producing mojibake on display); ASCII subjects pass through untouched.
// The body carries its own charset via Content-Type and is left as-is.
func buildRFC2822(to, cc, bcc, subject, body string) string {
	return buildRFC2822Reply(to, cc, bcc, subject, body, "", "")
}

// buildRFC2822Reply is buildRFC2822 plus the two threading headers Gmail
// needs to nest a message inside an existing thread: In-Reply-To (the
// original message's Message-ID) and References (the original message's
// References chain, with its Message-ID appended). When both inReplyTo
// and references are "" the output is byte-for-byte identical to the
// pre-threading buildRFC2822 — the headers are simply omitted — so the
// non-reply path is provably unchanged.
//
// Setting threadId on the drafts.create / messages.send request body is
// what actually groups the message in Gmail's UI; the In-Reply-To /
// References headers are what RFC-compliant clients (and Gmail's own
// conversation collapsing) use to position the reply within the thread.
// Both are required for a clean reply, so the caller sets threadId and
// passes these headers here.
func buildRFC2822Reply(to, cc, bcc, subject, body, inReplyTo, references string) string {
	var b strings.Builder
	b.WriteString("To: ")
	b.WriteString(to)
	b.WriteString("\r\n")
	if cc != "" {
		b.WriteString("Cc: ")
		b.WriteString(cc)
		b.WriteString("\r\n")
	}
	if bcc != "" {
		b.WriteString("Bcc: ")
		b.WriteString(bcc)
		b.WriteString("\r\n")
	}
	b.WriteString("Subject: ")
	b.WriteString(mime.QEncoding.Encode("utf-8", subject))
	b.WriteString("\r\n")
	if inReplyTo != "" {
		b.WriteString("In-Reply-To: ")
		b.WriteString(inReplyTo)
		b.WriteString("\r\n")
	}
	if references != "" {
		b.WriteString("References: ")
		b.WriteString(references)
		b.WriteString("\r\n")
	}
	b.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.String()
}

// replyThreadingHeaders derives the threading data needed to nest a
// reply inside an existing Gmail thread, given the original message's
// headers (the payload.headers[] array from users.messages.get with
// format=metadata, where each entry is {"name":..,"value":..}).
//
// Returns:
//
//	inReplyTo  — the original Message-ID, verbatim (incl. its angle
//	             brackets), suitable for the In-Reply-To header. "" if
//	             the original carried no Message-ID.
//	references — the original References chain with the original
//	             Message-ID appended (space-separated, per RFC 5322).
//	             Falls back to just the Message-ID when the original had
//	             no References header (it was the thread root). "" only
//	             when there is no Message-ID at all.
//
// Header name matching is case-insensitive because RFC 5322 header
// field names are case-insensitive and Gmail's metadata format echoes
// whatever casing the original sender used.
func replyThreadingHeaders(headers []any) (inReplyTo, references string) {
	messageID := extractHeader(headers, "Message-ID")
	origRefs := extractHeader(headers, "References")
	if messageID == "" {
		return "", ""
	}
	if origRefs != "" {
		return messageID, origRefs + " " + messageID
	}
	return messageID, messageID
}

// extractHeader returns the value of the named header from a Gmail
// payload.headers[] array (case-insensitive), or "" if absent. Each
// element is expected to be a map with "name" and "value" string keys;
// non-conforming elements are skipped.
func extractHeader(headers []any, name string) string {
	for _, h := range headers {
		m, ok := h.(map[string]any)
		if !ok {
			continue
		}
		n, _ := m["name"].(string)
		if strings.EqualFold(n, name) {
			v, _ := m["value"].(string)
			return v
		}
	}
	return ""
}

// ensureRePrefix prefixes "Re: " onto a subject for a reply, unless the
// subject already opens with a reply marker. Matching is case-insensitive
// and tolerates leading whitespace ("re:", "RE:", "  Re: foo" are all
// treated as already-prefixed) so threaded replies don't accrete
// "Re: Re: Re:". Only the exact "re:" token (three chars) is recognised:
// "Re :" with a space before the colon does NOT match and would get a
// fresh prefix — acceptable since mail clients emit "Re:" without the
// space, so this only affects hand-typed oddities.
func ensureRePrefix(subject string) string {
	trimmed := strings.TrimSpace(subject)
	if len(trimmed) >= 3 && strings.EqualFold(trimmed[:3], "re:") {
		return subject
	}
	return "Re: " + subject
}

// normalizeAttendees accepts either a comma-separated string or a JSON
// array of strings and returns a slice of email addresses (whitespace
// trimmed, empties dropped). Returns nil for unsupported types so the
// caller omits the field rather than dispatching with malformed data.
func normalizeAttendees(v any) []string {
	switch in := v.(type) {
	case string:
		if in == "" {
			return nil
		}
		parts := strings.Split(in, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				out = append(out, t)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(in))
		for _, x := range in {
			if s, ok := x.(string); ok {
				if t := strings.TrimSpace(s); t != "" {
					out = append(out, t)
				}
			}
		}
		return out
	default:
		return nil
	}
}

// buildListDraftsURL constructs the Gmail users.drafts.list endpoint
// URL. Pulled out of listDrafts so the wire shape (param names,
// omission of empty optionals) is exercisable without the host-import
// HTTP path. Mirrors the inline pattern in listRecentEmails; the
// helper exists here rather than there because list_drafts is the
// first op that takes a continuation token, which is enough wire
// surface to warrant a unit test.
func buildListDraftsURL(query string, maxResults int, pageToken string) string {
	q := url.Values{}
	q.Set("maxResults", strconv.Itoa(maxResults))
	if query != "" {
		q.Set("q", query)
	}
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	return "https://gmail.googleapis.com/gmail/v1/users/me/drafts?" + q.Encode()
}

// buildListEventsURL constructs the Calendar events.list endpoint URL.
// Pulled out of listUpcomingEvents so the wire shape (param names,
// the time-window bounds, omission of an empty timeMax) is exercisable
// without the host-import HTTP path.
//
// timeMin is required and always set — the caller passes either the
// supplied RFC3339 time_min or a "now" default, so the query never
// degrades into an all-of-history scan. timeMax is optional: when
// non-empty it bounds the upper edge of the window, which is what
// stops events.list from expanding unbounded far-future recurring
// instances (e.g. yearly birthdays for 2027/2028/2029…). Both values
// are passed through verbatim; RFC3339 validation is the API's job.
func buildListEventsURL(calendarID, timeMin, timeMax string, maxResults int) string {
	q := url.Values{}
	q.Set("maxResults", strconv.Itoa(maxResults))
	q.Set("timeMin", timeMin)
	if timeMax != "" {
		q.Set("timeMax", timeMax)
	}
	q.Set("singleEvents", "true")
	q.Set("orderBy", "startTime")
	return "https://www.googleapis.com/calendar/v3/calendars/" +
		url.PathEscape(calendarID) + "/events?" + q.Encode()
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

// searchContactsPageSizeCap is the People API's stated maximum for
// `people:searchContacts.pageSize` (the API documents `Maximum: 30`
// and returns HTTP 400 above it). The connector's readMaxResults caps
// at 100 — fine for Gmail / Calendar list ops, too loose here — so
// buildSearchContactsURL clamps a second time against this value.
const searchContactsPageSizeCap = 30

// buildSearchContactsURL constructs the People API people:searchContacts
// endpoint URL. Pulled out of searchContacts so the wire shape
// (param names, the People-API-specific pageSize cap) is exercisable
// without the host-import HTTP path. Mirrors buildListDraftsURL.
//
//	GET https://people.googleapis.com/v1/people:searchContacts
//	    ?query=...&readMask=...&pageSize=...
//
// readMask is required by the API — buildSearchContactsURL trusts the
// caller (searchContacts) to have substituted a default before this
// runs. pageSize is clamped to searchContactsPageSizeCap.
func buildSearchContactsURL(query, readMask string, pageSize int) string {
	if pageSize > searchContactsPageSizeCap {
		pageSize = searchContactsPageSizeCap
	}
	q := url.Values{}
	q.Set("query", query)
	q.Set("readMask", readMask)
	q.Set("pageSize", strconv.Itoa(pageSize))
	return "https://people.googleapis.com/v1/people:searchContacts?" + q.Encode()
}

// buildListContactsURL constructs the People API people/me/connections
// endpoint URL. Same extract-for-testability pattern as
// buildListDraftsURL — list_contacts has the widest param surface of
// the contacts ops (personFields, pageSize, pageToken, sortOrder),
// which is enough to warrant a unit-testable helper.
//
//	GET https://people.googleapis.com/v1/people/me/connections
//	    ?personFields=...&pageSize=...&pageToken=...&sortOrder=...
//
// personFields is required by the API; the caller substitutes a
// default before this runs. pageToken and sortOrder are omitted from
// the query string when empty so the URL stays minimal on the
// first-page / unsorted call.
func buildListContactsURL(personFields string, pageSize int, pageToken, sortOrder string) string {
	q := url.Values{}
	q.Set("personFields", personFields)
	q.Set("pageSize", strconv.Itoa(pageSize))
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	if sortOrder != "" {
		q.Set("sortOrder", sortOrder)
	}
	return "https://people.googleapis.com/v1/people/me/connections?" + q.Encode()
}

// driveFilesDefaultFields is the field set returned by search_files when
// the caller does not pass `fields` explicitly. Kept lean — id, name,
// mimeType, modifiedTime, parents, webViewLink — to keep response size
// bounded for large result pages. Callers that need owners, sharing
// info, or capabilities can override.
const driveFilesDefaultFields = "files(id,name,mimeType,modifiedTime,parents,webViewLink),nextPageToken"

// buildSearchFilesURL constructs the Drive v3 files.list endpoint URL.
// Pulled out of searchFiles so the wire shape (param names, omission of
// empty optionals, default field mask) is exercisable without the
// host-import HTTP path. Mirrors the buildListDraftsURL / buildList
// ContactsURL pattern.
//
//	GET https://www.googleapis.com/drive/v3/files
//	    ?q=...&pageSize=...&fields=...&orderBy=...&pageToken=...
//
// `fields` is required for predictable response size — Drive's default
// response omits useful fields like modifiedTime and parents, so we
// pass an explicit mask. Callers may override but cannot omit.
func buildSearchFilesURL(query, fields, orderBy, pageToken string, pageSize int) string {
	if fields == "" {
		fields = driveFilesDefaultFields
	}
	q := url.Values{}
	q.Set("pageSize", strconv.Itoa(pageSize))
	q.Set("fields", fields)
	if query != "" {
		q.Set("q", query)
	}
	if orderBy != "" {
		q.Set("orderBy", orderBy)
	}
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	return "https://www.googleapis.com/drive/v3/files?" + q.Encode()
}

// isGoogleNativeMIME reports whether a Drive mimeType is one of
// Google's editor formats (Docs / Sheets / Slides / Drawings / Forms /
// etc.). Native files have no downloadable byte stream; reading their
// content goes through the export endpoint with a target mimeType,
// while non-native files are streamed via alt=media.
func isGoogleNativeMIME(driveMIME string) bool {
	return strings.HasPrefix(driveMIME, "application/vnd.google-apps.")
}

// defaultExportMIME maps a Google native Drive mimeType to the
// connector's default text export type. Returns "" for native types
// that have no sensible text export (e.g. Drawings, Forms) so the
// caller can error out with a clear message instead of dispatching
// against an export the agent cannot consume.
//
// The defaults favor text over rich formats because the dominant
// consumer is an LLM agent reading content for context — text/plain
// for Docs/Slides and text/csv for Sheets are the cheapest faithful
// representations. Callers that need PDF or DOCX may override via the
// `export_mime_type` arg on get_file_content.
func defaultExportMIME(driveMIME string) string {
	switch driveMIME {
	case "application/vnd.google-apps.document":
		return "text/plain"
	case "application/vnd.google-apps.spreadsheet":
		return "text/csv"
	case "application/vnd.google-apps.presentation":
		return "text/plain"
	default:
		return ""
	}
}

// buildMultipartUpload assembles a multipart/related body for Drive's
// uploadType=multipart endpoint: a JSON metadata part followed by the
// file content part. Returns the body bytes and the full Content-Type
// header value (including the generated boundary) the caller should
// set on the outbound request.
//
// v1 scope: the content is treated as UTF-8 text — JSON encoding of
// the host-ABI request body coerces arbitrary bytes to valid UTF-8 by
// replacing invalid sequences with the Unicode replacement character,
// so binary uploads (PDFs, images) would silently corrupt. Restricting
// upload_file to text content is the explicit v1 trade-off; binary
// support is a follow-up that needs a host-ABI binary-body field.
//
// mime/multipart.Writer is used for boundary generation; the outer
// Content-Type is set to multipart/related (not the package default
// multipart/form-data) because Drive's upload endpoint expects the
// related variant per its documented contract.
func buildMultipartUpload(metadata map[string]any, contentMIME string, content []byte) ([]byte, string, error) {
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, "", fmt.Errorf("marshal metadata: %w", err)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	metaHdr := textproto.MIMEHeader{}
	metaHdr.Set("Content-Type", "application/json; charset=UTF-8")
	metaPart, err := w.CreatePart(metaHdr)
	if err != nil {
		return nil, "", fmt.Errorf("create metadata part: %w", err)
	}
	if _, err := metaPart.Write(metaJSON); err != nil {
		return nil, "", fmt.Errorf("write metadata part: %w", err)
	}

	contHdr := textproto.MIMEHeader{}
	contHdr.Set("Content-Type", contentMIME)
	contPart, err := w.CreatePart(contHdr)
	if err != nil {
		return nil, "", fmt.Errorf("create content part: %w", err)
	}
	if _, err := contPart.Write(content); err != nil {
		return nil, "", fmt.Errorf("write content part: %w", err)
	}

	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart writer: %w", err)
	}
	return buf.Bytes(), "multipart/related; boundary=" + w.Boundary(), nil
}

// isTextLikeMIME reports whether a non-native Drive mimeType is safe
// to round-trip through the host-ABI's JSON-string body field. The
// stdin/stdout ABI encodes strings as JSON, which coerces arbitrary
// bytes to valid UTF-8 (replacing invalid sequences with U+FFFD), so
// binary downloads would silently corrupt. v1 restricts get_file_content
// to text-shaped mimeTypes; binary support is a follow-up that needs
// a host-ABI binary-body field.
func isTextLikeMIME(m string) bool {
	if strings.HasPrefix(m, "text/") {
		return true
	}
	switch m {
	case "application/json",
		"application/xml",
		"application/javascript",
		"application/yaml",
		"application/x-yaml",
		"application/toml",
		"application/x-toml":
		return true
	}
	return false
}

// driveParentsList normalizes a parent-folder argument into a
// comma-separated list of folder ids. Accepts either a comma-separated
// string ("folder1,folder2") or a JSON array of strings, mirroring
// normalizeAttendees' shape. Returns "" for nil / empty / unsupported
// types so the caller can omit the field cleanly.
func driveParentsList(v any) string {
	switch in := v.(type) {
	case string:
		parts := strings.Split(in, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				out = append(out, t)
			}
		}
		return strings.Join(out, ",")
	case []any:
		out := make([]string, 0, len(in))
		for _, x := range in {
			if s, ok := x.(string); ok {
				if t := strings.TrimSpace(s); t != "" {
					out = append(out, t)
				}
			}
		}
		return strings.Join(out, ",")
	default:
		return ""
	}
}

// normalizeEmailFormat coerces the get_email `format` arg into one of the
// two values the op supports: "metadata" (the default, headers + snippet
// only) or "full" (fetch and decode the MIME body). Any absent, empty, or
// unrecognized value falls back to "metadata" so the cheap fan-out path —
// "summarize my recent emails" drilling into N messages — stays the
// default and an unexpected arg can never silently inflate Gmail quota.
// Matching is case-insensitive and whitespace-tolerant for ergonomics.
func normalizeEmailFormat(v any) string {
	s, ok := v.(string)
	if !ok {
		return "metadata"
	}
	if strings.EqualFold(strings.TrimSpace(s), "full") {
		return "full"
	}
	return "metadata"
}

// decodeGmailBase64URL decodes the base64url-encoded body bytes Gmail puts
// in payload.body.data (and each part's body.data). Gmail uses the URL-safe
// alphabet (-/_ for +//) and omits padding, so RawURLEncoding is the
// primary decoder; some intermediaries re-pad, so we fall back to the
// padded URLEncoding. Returns ("", false) when neither decode succeeds so
// the caller can skip a malformed part rather than emit garbage.
func decodeGmailBase64URL(data string) (string, bool) {
	if data == "" {
		return "", false
	}
	if b, err := base64.RawURLEncoding.DecodeString(data); err == nil {
		return string(b), true
	}
	if b, err := base64.URLEncoding.DecodeString(data); err == nil {
		return string(b), true
	}
	return "", false
}

// extractEmailBody walks a Gmail message `payload` tree (the object
// returned by users.messages.get?format=full) and returns the decoded
// body text plus the MIME type it came from.
//
// Strategy, depth-first across the nested `parts` tree:
//   - Prefer the first decodable text/plain part — it is the cheapest
//     faithful representation for an LLM reader.
//   - Fall back to the first decodable text/html part when no text/plain
//     exists (some senders ship HTML-only).
//
// multipart/mixed-with-attachment is the normal case, not an edge case:
// the walk recurses into every `parts[]` child and simply ignores parts
// whose mimeType is neither text/plain nor text/html (attachments,
// images, nested non-text containers), so an email with a PDF attached
// still yields its text body. Attachment bytes are out of scope.
//
// A non-multipart message carries its content directly in payload.body.data
// with the top-level mimeType, which the same code path handles because the
// root payload is walked like any other part.
//
// Returns ("", "") when no decodable text part is found.
func extractEmailBody(payload map[string]any) (body, mimeType string) {
	plain, plainOK := emailPartByType(payload, "text/plain")
	if plainOK {
		return plain, "text/plain"
	}
	html, htmlOK := emailPartByType(payload, "text/html")
	if htmlOK {
		return html, "text/html"
	}
	return "", ""
}

// emailPartByType depth-first searches a Gmail payload/part subtree for
// the first part whose mimeType equals want (case-insensitive) and whose
// body.data decodes cleanly, returning its decoded text. Recurses into
// nested `parts[]` so multipart/alternative and multipart/mixed trees of
// any depth are handled uniformly.
func emailPartByType(part map[string]any, want string) (string, bool) {
	if part == nil {
		return "", false
	}
	mt, _ := part["mimeType"].(string)
	if strings.EqualFold(mt, want) {
		if data := emailPartData(part); data != "" {
			if decoded, ok := decodeGmailBase64URL(data); ok {
				return decoded, true
			}
		}
	}
	parts, _ := part["parts"].([]any)
	for _, p := range parts {
		child, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if decoded, ok := emailPartByType(child, want); ok {
			return decoded, true
		}
	}
	return "", false
}

// emailPartData pulls the base64url `data` string out of a part's `body`
// object, or "" when the part has no inline body (e.g. an attachment, which
// carries an attachmentId instead of data, or a container part).
func emailPartData(part map[string]any) string {
	body, ok := part["body"].(map[string]any)
	if !ok {
		return ""
	}
	data, _ := body["data"].(string)
	return data
}
