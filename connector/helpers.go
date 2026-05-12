// Pure helpers for the connector — no host-import dependencies, so this
// file builds on every Go target (including the host platform). main.go
// is wasip1-only because it imports the aileron_host module; keeping
// these helpers in a separate, untagged file lets `go test` exercise
// them as ordinary Go unit tests.

package main

import (
	"mime"
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
	b.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.String()
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
