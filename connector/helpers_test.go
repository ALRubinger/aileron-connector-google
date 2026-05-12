package main

import (
	"mime"
	"reflect"
	"strings"
	"testing"
)

// --- buildRFC2822 ---

func TestBuildRFC2822_BasicShape(t *testing.T) {
	got := buildRFC2822("alice@example.com", "", "", "Hello", "world\n")
	want := "To: alice@example.com\r\n" +
		"Subject: Hello\r\n" +
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
		"\r\n" +
		"world\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestBuildRFC2822_IncludesCcAndBccWhenSet(t *testing.T) {
	got := buildRFC2822(
		"alice@example.com",
		"bob@example.com",
		"carol@example.com",
		"Subject",
		"body",
	)
	for _, want := range []string{
		"To: alice@example.com\r\n",
		"Cc: bob@example.com\r\n",
		"Bcc: carol@example.com\r\n",
		"Subject: Subject\r\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestBuildRFC2822_OmitsCcAndBccWhenEmpty(t *testing.T) {
	got := buildRFC2822("alice@example.com", "", "", "S", "B")
	if strings.Contains(got, "Cc:") {
		t.Errorf("unexpected Cc header: %q", got)
	}
	if strings.Contains(got, "Bcc:") {
		t.Errorf("unexpected Bcc header: %q", got)
	}
}

func TestBuildRFC2822_HeadersSeparatedFromBodyByBlankLine(t *testing.T) {
	got := buildRFC2822("a@b.c", "", "", "S", "the body")
	idx := strings.Index(got, "\r\n\r\n")
	if idx < 0 {
		t.Fatalf("no CRLF CRLF separator: %q", got)
	}
	if got[idx+4:] != "the body" {
		t.Errorf("body after separator = %q, want %q", got[idx+4:], "the body")
	}
}

func TestBuildRFC2822_PreservesBodyExactly(t *testing.T) {
	body := "line one\nline two\n\nparagraph after blank line\n"
	got := buildRFC2822("a@b.c", "", "", "S", body)
	if !strings.HasSuffix(got, body) {
		t.Errorf("body not preserved verbatim; got tail %q", got[len(got)-len(body):])
	}
}

func TestBuildRFC2822_AsciiSubjectIsNotEncoded(t *testing.T) {
	got := buildRFC2822("a@b.c", "", "", "Plain ASCII subject", "body")
	if !strings.Contains(got, "Subject: Plain ASCII subject\r\n") {
		t.Errorf("ASCII subject should appear unencoded; got:\n%s", got)
	}
	if strings.Contains(got, "=?") {
		t.Errorf("ASCII subject should not be RFC 2047 encoded; got:\n%s", got)
	}
}

func TestBuildRFC2822_NonAsciiSubjectIsRFC2047Encoded(t *testing.T) {
	// Em dash (U+2014) in headers as raw UTF-8 gets mis-decoded as
	// Latin-1 by downstream agents and renders as "Ã¢Â€Â"" mojibake.
	// RFC 2047 encoded-word wrapping is the cure.
	subject := "Aileron weekly recap — May 4 to May 11, 2026"
	got := buildRFC2822("a@b.c", "", "", subject, "body")

	start := strings.Index(got, "Subject: ")
	if start < 0 {
		t.Fatalf("no Subject header in:\n%s", got)
	}
	end := strings.Index(got[start:], "\r\n")
	if end < 0 {
		t.Fatalf("Subject header not CRLF-terminated in:\n%s", got)
	}
	value := got[start+len("Subject: ") : start+end]

	if !strings.HasPrefix(strings.ToLower(value), "=?utf-8?q?") {
		t.Errorf("Subject not RFC 2047 q-encoded; got %q", value)
	}
	if strings.Contains(value, "—") {
		t.Errorf("Subject still contains raw non-ASCII; got %q", value)
	}

	decoded, err := new(mime.WordDecoder).DecodeHeader(value)
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if decoded != subject {
		t.Errorf("round-trip mismatch:\n got  %q\n want %q", decoded, subject)
	}
}

// --- normalizeAttendees ---

func TestNormalizeAttendees_StringFormSplitsAndTrims(t *testing.T) {
	got := normalizeAttendees("alice@example.com, bob@example.com,carol@example.com")
	want := []string{"alice@example.com", "bob@example.com", "carol@example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormalizeAttendees_StringFormDropsEmpties(t *testing.T) {
	got := normalizeAttendees("alice@example.com,, ,bob@example.com")
	want := []string{"alice@example.com", "bob@example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormalizeAttendees_EmptyStringReturnsNil(t *testing.T) {
	if got := normalizeAttendees(""); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestNormalizeAttendees_ArrayForm(t *testing.T) {
	got := normalizeAttendees([]any{"alice@example.com", " bob@example.com "})
	want := []string{"alice@example.com", "bob@example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormalizeAttendees_ArrayDropsNonStrings(t *testing.T) {
	got := normalizeAttendees([]any{"alice@example.com", 42, nil, "bob@example.com"})
	want := []string{"alice@example.com", "bob@example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormalizeAttendees_UnsupportedTypeReturnsNil(t *testing.T) {
	cases := []any{nil, 42, 3.14, map[string]any{"k": "v"}, true}
	for _, in := range cases {
		if got := normalizeAttendees(in); got != nil {
			t.Errorf("input %#v: got %v, want nil", in, got)
		}
	}
}

// --- buildListDraftsURL ---

func TestBuildListDraftsURL_BaseEndpoint(t *testing.T) {
	got := buildListDraftsURL("", 10, "")
	const prefix = "https://gmail.googleapis.com/gmail/v1/users/me/drafts?"
	if !strings.HasPrefix(got, prefix) {
		t.Errorf("got %q, want prefix %q", got, prefix)
	}
}

func TestBuildListDraftsURL_AlwaysSetsMaxResults(t *testing.T) {
	got := buildListDraftsURL("", 25, "")
	if !strings.Contains(got, "maxResults=25") {
		t.Errorf("missing maxResults=25 in %q", got)
	}
}

func TestBuildListDraftsURL_OmitsEmptyOptionals(t *testing.T) {
	got := buildListDraftsURL("", 10, "")
	if strings.Contains(got, "q=") {
		t.Errorf("empty query should be omitted; got %q", got)
	}
	if strings.Contains(got, "pageToken=") {
		t.Errorf("empty page_token should be omitted; got %q", got)
	}
}

func TestBuildListDraftsURL_IncludesQueryWhenSet(t *testing.T) {
	got := buildListDraftsURL("subject:invoice", 10, "")
	// url.Values encodes ":" as "%3A".
	if !strings.Contains(got, "q=subject%3Ainvoice") {
		t.Errorf("missing url-encoded q=subject:invoice in %q", got)
	}
}

func TestBuildListDraftsURL_IncludesPageTokenWhenSet(t *testing.T) {
	got := buildListDraftsURL("", 10, "next-page-abc123")
	if !strings.Contains(got, "pageToken=next-page-abc123") {
		t.Errorf("missing pageToken=next-page-abc123 in %q", got)
	}
}

func TestBuildListDraftsURL_AllParamsCombined(t *testing.T) {
	got := buildListDraftsURL("is:unread", 50, "tok")
	for _, want := range []string{"maxResults=50", "q=is%3Aunread", "pageToken=tok"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

// --- readMaxResults ---

func TestReadMaxResults_MissingKeyReturnsDefault(t *testing.T) {
	if got := readMaxResults(map[string]any{}, 7); got != 7 {
		t.Errorf("got %d, want 7", got)
	}
}

func TestReadMaxResults_Float64Coerces(t *testing.T) {
	// JSON unmarshal yields float64 for numbers; this is the production path.
	got := readMaxResults(map[string]any{"max_results": float64(25)}, 10)
	if got != 25 {
		t.Errorf("got %d, want 25", got)
	}
}

func TestReadMaxResults_IntPassesThrough(t *testing.T) {
	got := readMaxResults(map[string]any{"max_results": 25}, 10)
	if got != 25 {
		t.Errorf("got %d, want 25", got)
	}
}

func TestReadMaxResults_ZeroOrNegativeFallsBackToDefault(t *testing.T) {
	for _, n := range []any{0, -1, float64(0), float64(-5)} {
		got := readMaxResults(map[string]any{"max_results": n}, 10)
		if got != 10 {
			t.Errorf("input %v: got %d, want 10 (default)", n, got)
		}
	}
}

func TestReadMaxResults_ExceedingCapClamps(t *testing.T) {
	for _, n := range []any{500, float64(1_000_000)} {
		got := readMaxResults(map[string]any{"max_results": n}, 10)
		if got != 100 {
			t.Errorf("input %v: got %d, want 100 (cap)", n, got)
		}
	}
}

func TestReadMaxResults_UnsupportedTypeFallsBackToDefault(t *testing.T) {
	for _, n := range []any{"twenty", true, []any{}, map[string]any{}} {
		got := readMaxResults(map[string]any{"max_results": n}, 10)
		if got != 10 {
			t.Errorf("input %v: got %d, want 10 (default)", n, got)
		}
	}
}
