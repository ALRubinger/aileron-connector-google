// Pure helpers for the connector — no host-import dependencies, so this
// file builds on every Go target (including the host platform). main.go
// is wasip1-only because it imports the aileron_host module; keeping
// these helpers in a separate, untagged file lets `go test` exercise
// them as ordinary Go unit tests.

package main

import "strings"

// buildRFC2822 constructs a minimal RFC 2822 / RFC 5322 message that
// Gmail's drafts.create accepts as the `raw` field. Gmail expects the
// body to follow the same wire format an SMTP server would handle:
// CRLF-terminated headers, blank line, body. Encoding is the caller's
// concern — Gmail accepts UTF-8 in headers (Q-encoded for non-ASCII)
// but plain ASCII bodies and subjects are the safe v0.0.1 path.
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
	b.WriteString(subject)
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
