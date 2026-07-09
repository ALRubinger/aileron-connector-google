package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
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

// --- buildRFC2822Reply / threading headers ---

func TestBuildRFC2822_NoReplyArgsIsByteForByteUnchanged(t *testing.T) {
	// The non-reply convenience wrapper must produce output identical to
	// the reply builder called with empty threading args — this is the
	// "param absent => unchanged" guarantee in structural form.
	plain := buildRFC2822("a@b.c", "x@y.z", "", "Hi", "body")
	reply := buildRFC2822Reply("a@b.c", "x@y.z", "", "Hi", "body", "", "")
	if plain != reply {
		t.Errorf("empty-threading reply differs from plain:\n plain %q\n reply %q", plain, reply)
	}
}

func TestBuildRFC2822Reply_InjectsThreadingHeaders(t *testing.T) {
	got := buildRFC2822Reply(
		"a@b.c", "", "", "Re: Hi", "body",
		"<orig@mail.example>",
		"<root@mail.example> <orig@mail.example>",
	)
	for _, want := range []string{
		"In-Reply-To: <orig@mail.example>\r\n",
		"References: <root@mail.example> <orig@mail.example>\r\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// Threading headers must precede the body separator.
	sep := strings.Index(got, "\r\n\r\n")
	if sep < 0 || strings.Index(got, "In-Reply-To:") > sep {
		t.Errorf("In-Reply-To header not in header block:\n%s", got)
	}
}

func TestBuildRFC2822Reply_OmitsHeadersWhenEmpty(t *testing.T) {
	got := buildRFC2822Reply("a@b.c", "", "", "S", "B", "", "")
	if strings.Contains(got, "In-Reply-To:") {
		t.Errorf("unexpected In-Reply-To header: %q", got)
	}
	if strings.Contains(got, "References:") {
		t.Errorf("unexpected References header: %q", got)
	}
}

func hdrs(pairs ...[2]string) []any {
	out := make([]any, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, map[string]any{"name": p[0], "value": p[1]})
	}
	return out
}

func TestReplyThreadingHeaders_WithExistingReferencesChain(t *testing.T) {
	h := hdrs(
		[2]string{"Subject", "Hello"},
		[2]string{"Message-ID", "<msg2@example.com>"},
		[2]string{"References", "<msg0@example.com> <msg1@example.com>"},
	)
	inReplyTo, references := replyThreadingHeaders(h)
	if inReplyTo != "<msg2@example.com>" {
		t.Errorf("inReplyTo = %q", inReplyTo)
	}
	want := "<msg0@example.com> <msg1@example.com> <msg2@example.com>"
	if references != want {
		t.Errorf("references = %q, want %q", references, want)
	}
}

func TestReplyThreadingHeaders_ThreadRootHasNoReferences(t *testing.T) {
	h := hdrs([2]string{"Message-ID", "<root@example.com>"})
	inReplyTo, references := replyThreadingHeaders(h)
	if inReplyTo != "<root@example.com>" {
		t.Errorf("inReplyTo = %q", inReplyTo)
	}
	// With no prior References, the chain seeds from the Message-ID alone.
	if references != "<root@example.com>" {
		t.Errorf("references = %q, want seed from Message-ID", references)
	}
}

func TestReplyThreadingHeaders_CaseInsensitiveHeaderNames(t *testing.T) {
	h := hdrs(
		[2]string{"message-id", "<lower@example.com>"},
		[2]string{"REFERENCES", "<a@example.com>"},
	)
	inReplyTo, references := replyThreadingHeaders(h)
	if inReplyTo != "<lower@example.com>" {
		t.Errorf("inReplyTo = %q (case-insensitive Message-ID lookup failed)", inReplyTo)
	}
	if references != "<a@example.com> <lower@example.com>" {
		t.Errorf("references = %q", references)
	}
}

func TestReplyThreadingHeaders_NoMessageIDYieldsEmpty(t *testing.T) {
	h := hdrs([2]string{"Subject", "no id here"})
	inReplyTo, references := replyThreadingHeaders(h)
	if inReplyTo != "" || references != "" {
		t.Errorf("expected empty threading headers; got (%q, %q)", inReplyTo, references)
	}
}

func TestExtractHeader_SkipsNonMapEntries(t *testing.T) {
	// Non-conforming (non-map) elements must be skipped, not panic the
	// lookup, so a later well-formed header is still found.
	h := []any{
		"not a map",
		42,
		map[string]any{"name": "Subject", "value": "Found"},
	}
	if got := extractHeader(h, "Subject"); got != "Found" {
		t.Errorf("extractHeader = %q, want Found", got)
	}
	if got := extractHeader(h, "Missing"); got != "" {
		t.Errorf("extractHeader(Missing) = %q, want empty", got)
	}
}

func TestEnsureRePrefix(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Project update", "Re: Project update"},
		{"Re: Project update", "Re: Project update"},
		{"RE: shouting", "RE: shouting"},
		{"re: lower", "re: lower"},
		{"  Re: leading space", "  Re: leading space"},
		{"", "Re: "},
		{"Reply but not prefix", "Re: Reply but not prefix"},
	}
	for _, c := range cases {
		if got := ensureRePrefix(c.in); got != c.want {
			t.Errorf("ensureRePrefix(%q) = %q, want %q", c.in, got, c.want)
		}
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

// --- buildListEventsURL ---

func TestBuildListEventsURL_BaseEndpointAndStaticParams(t *testing.T) {
	got := buildListEventsURL("primary", "2026-06-20T00:00:00Z", "", 10)
	const prefix = "https://www.googleapis.com/calendar/v3/calendars/primary/events?"
	if !strings.HasPrefix(got, prefix) {
		t.Errorf("got %q, want prefix %q", got, prefix)
	}
	// singleEvents + orderBy must always be present so recurring events
	// expand and come back chronologically — the contract the action
	// promises regardless of the time window.
	for _, want := range []string{"singleEvents=true", "orderBy=startTime", "maxResults=10"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestBuildListEventsURL_DefaultSetsTimeMinOnly(t *testing.T) {
	// The "now" default is computed by the caller and passed in as
	// time_min; with no time_max the upper bound is omitted entirely.
	got := buildListEventsURL("primary", "2026-06-20T12:00:00Z", "", 10)
	// url.Values encodes ":" as "%3A".
	if !strings.Contains(got, "timeMin=2026-06-20T12%3A00%3A00Z") {
		t.Errorf("missing url-encoded timeMin in %q", got)
	}
	if strings.Contains(got, "timeMax=") {
		t.Errorf("empty time_max should be omitted; got %q", got)
	}
}

func TestBuildListEventsURL_TimeMinOnlyHonorsSuppliedLowerBound(t *testing.T) {
	got := buildListEventsURL("primary", "2026-06-15T00:00:00Z", "", 10)
	if !strings.Contains(got, "timeMin=2026-06-15T00%3A00%3A00Z") {
		t.Errorf("supplied time_min not honored in %q", got)
	}
	if strings.Contains(got, "timeMax=") {
		t.Errorf("time_max should be absent; got %q", got)
	}
}

func TestBuildListEventsURL_TimeMaxOnlyBoundsUpperEdge(t *testing.T) {
	got := buildListEventsURL("primary", "2026-06-20T00:00:00Z", "2026-06-21T23:59:59Z", 10)
	if !strings.Contains(got, "timeMax=2026-06-21T23%3A59%3A59Z") {
		t.Errorf("missing url-encoded timeMax in %q", got)
	}
}

func TestBuildListEventsURL_BothBoundsSet(t *testing.T) {
	got := buildListEventsURL("primary", "2026-06-15T00:00:00Z", "2026-06-21T23:59:59Z", 25)
	for _, want := range []string{
		"timeMin=2026-06-15T00%3A00%3A00Z",
		"timeMax=2026-06-21T23%3A59%3A59Z",
		"maxResults=25",
		"singleEvents=true",
		"orderBy=startTime",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestBuildListEventsURL_PathEscapesCalendarID(t *testing.T) {
	// A calendar id can contain a "/" (e.g. a group address segment or a
	// resource id); url.PathEscape must encode it so it can't break out
	// of the {calendarId} path segment. "@" is a legal path sub-delim and
	// is left intact, matching url.PathEscape's behavior.
	got := buildListEventsURL("a/b@example.com", "2026-06-20T00:00:00Z", "", 10)
	if !strings.Contains(got, "/calendars/a%2Fb@example.com/events?") {
		t.Errorf("calendar id not path-escaped in %q", got)
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

// --- buildSearchContactsURL ---

func TestBuildSearchContactsURL_BaseEndpoint(t *testing.T) {
	got := buildSearchContactsURL("alice", "names", 10)
	const prefix = "https://people.googleapis.com/v1/people:searchContacts?"
	if !strings.HasPrefix(got, prefix) {
		t.Errorf("got %q, want prefix %q", got, prefix)
	}
}

func TestBuildSearchContactsURL_EncodesQueryAndReadMask(t *testing.T) {
	got := buildSearchContactsURL("alice smith", "names,emailAddresses,phoneNumbers", 10)
	// url.Values encodes space as "+" and "," as "%2C".
	if !strings.Contains(got, "query=alice+smith") {
		t.Errorf("missing url-encoded query in %q", got)
	}
	if !strings.Contains(got, "readMask=names%2CemailAddresses%2CphoneNumbers") {
		t.Errorf("missing url-encoded readMask in %q", got)
	}
}

func TestBuildSearchContactsURL_SetsPageSize(t *testing.T) {
	got := buildSearchContactsURL("a", "names", 15)
	if !strings.Contains(got, "pageSize=15") {
		t.Errorf("missing pageSize=15 in %q", got)
	}
}

func TestBuildSearchContactsURL_ClampsAtAPICap(t *testing.T) {
	// People API caps people:searchContacts.pageSize at 30; above
	// that the call returns HTTP 400 with INVALID_ARGUMENT. The
	// connector clamps to the cap so over-eager callers get results
	// instead of an error.
	got := buildSearchContactsURL("a", "names", 100)
	if !strings.Contains(got, "pageSize=30") {
		t.Errorf("page size should clamp to 30; got %q", got)
	}
}

// --- buildListContactsURL ---

func TestBuildListContactsURL_BaseEndpoint(t *testing.T) {
	got := buildListContactsURL("names", 100, "", "")
	const prefix = "https://people.googleapis.com/v1/people/me/connections?"
	if !strings.HasPrefix(got, prefix) {
		t.Errorf("got %q, want prefix %q", got, prefix)
	}
}

func TestBuildListContactsURL_AlwaysSetsPersonFieldsAndPageSize(t *testing.T) {
	got := buildListContactsURL("names,emailAddresses", 50, "", "")
	if !strings.Contains(got, "personFields=names%2CemailAddresses") {
		t.Errorf("missing url-encoded personFields in %q", got)
	}
	if !strings.Contains(got, "pageSize=50") {
		t.Errorf("missing pageSize=50 in %q", got)
	}
}

func TestBuildListContactsURL_OmitsEmptyOptionals(t *testing.T) {
	got := buildListContactsURL("names", 100, "", "")
	if strings.Contains(got, "pageToken=") {
		t.Errorf("empty page_token should be omitted; got %q", got)
	}
	if strings.Contains(got, "sortOrder=") {
		t.Errorf("empty sort_order should be omitted; got %q", got)
	}
}

func TestBuildListContactsURL_IncludesPageTokenWhenSet(t *testing.T) {
	got := buildListContactsURL("names", 100, "next-tok-xyz", "")
	if !strings.Contains(got, "pageToken=next-tok-xyz") {
		t.Errorf("missing pageToken=next-tok-xyz in %q", got)
	}
}

func TestBuildListContactsURL_IncludesSortOrderWhenSet(t *testing.T) {
	got := buildListContactsURL("names", 100, "", "LAST_MODIFIED_DESCENDING")
	if !strings.Contains(got, "sortOrder=LAST_MODIFIED_DESCENDING") {
		t.Errorf("missing sortOrder in %q", got)
	}
}

func TestBuildListContactsURL_AllParamsCombined(t *testing.T) {
	got := buildListContactsURL("names,birthdays", 25, "tok", "FIRST_NAME_ASCENDING")
	for _, want := range []string{
		"personFields=names%2Cbirthdays",
		"pageSize=25",
		"pageToken=tok",
		"sortOrder=FIRST_NAME_ASCENDING",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

// --- buildSearchFilesURL ---

func TestBuildSearchFilesURL_BaseEndpoint(t *testing.T) {
	got := buildSearchFilesURL("", "", "", "", 25)
	const prefix = "https://www.googleapis.com/drive/v3/files?"
	if !strings.HasPrefix(got, prefix) {
		t.Errorf("got %q, want prefix %q", got, prefix)
	}
}

func TestBuildSearchFilesURL_AlwaysSetsPageSizeAndFields(t *testing.T) {
	got := buildSearchFilesURL("", "", "", "", 25)
	if !strings.Contains(got, "pageSize=25") {
		t.Errorf("missing pageSize=25 in %q", got)
	}
	if !strings.Contains(got, "fields=") {
		t.Errorf("fields should always be set; got %q", got)
	}
}

func TestBuildSearchFilesURL_OmitsEmptyOptionals(t *testing.T) {
	got := buildSearchFilesURL("", "", "", "", 25)
	if strings.Contains(got, "q=") {
		t.Errorf("empty query should be omitted; got %q", got)
	}
	if strings.Contains(got, "orderBy=") {
		t.Errorf("empty order_by should be omitted; got %q", got)
	}
	if strings.Contains(got, "pageToken=") {
		t.Errorf("empty page_token should be omitted; got %q", got)
	}
}

func TestBuildSearchFilesURL_IncludesQueryWhenSet(t *testing.T) {
	got := buildSearchFilesURL("name contains 'budget'", "", "", "", 25)
	// url.Values encodes space as "+" and "'" as "%27".
	if !strings.Contains(got, "q=name+contains+%27budget%27") {
		t.Errorf("missing url-encoded q in %q", got)
	}
}

func TestBuildSearchFilesURL_CallerCanOverrideFields(t *testing.T) {
	got := buildSearchFilesURL("", "files(id,name,owners)", "", "", 25)
	if !strings.Contains(got, "fields=files%28id%2Cname%2Cowners%29") {
		t.Errorf("missing caller-supplied fields in %q", got)
	}
	if strings.Contains(got, "modifiedTime") {
		t.Errorf("default fields leaked when caller overrode; got %q", got)
	}
}

func TestBuildSearchFilesURL_IncludesOrderByWhenSet(t *testing.T) {
	got := buildSearchFilesURL("", "", "modifiedTime desc", "", 25)
	if !strings.Contains(got, "orderBy=modifiedTime+desc") {
		t.Errorf("missing orderBy in %q", got)
	}
}

func TestBuildSearchFilesURL_IncludesPageTokenWhenSet(t *testing.T) {
	got := buildSearchFilesURL("", "", "", "tok-xyz", 25)
	if !strings.Contains(got, "pageToken=tok-xyz") {
		t.Errorf("missing pageToken in %q", got)
	}
}

func TestBuildSearchFilesURL_AllParamsCombined(t *testing.T) {
	got := buildSearchFilesURL("trashed=false", "files(id,name)", "modifiedTime desc", "tok", 50)
	for _, want := range []string{
		"pageSize=50",
		"fields=files%28id%2Cname%29",
		"q=trashed%3Dfalse",
		"orderBy=modifiedTime+desc",
		"pageToken=tok",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

// --- isGoogleNativeMIME / defaultExportMIME ---

func TestIsGoogleNativeMIME(t *testing.T) {
	cases := map[string]bool{
		"application/vnd.google-apps.document":     true,
		"application/vnd.google-apps.spreadsheet":  true,
		"application/vnd.google-apps.presentation": true,
		"application/vnd.google-apps.folder":       true,
		"application/pdf":                          false,
		"text/plain":                               false,
		"image/png":                                false,
		"":                                         false,
	}
	for in, want := range cases {
		if got := isGoogleNativeMIME(in); got != want {
			t.Errorf("isGoogleNativeMIME(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDefaultExportMIME(t *testing.T) {
	cases := map[string]string{
		"application/vnd.google-apps.document":     "text/plain",
		"application/vnd.google-apps.spreadsheet":  "text/csv",
		"application/vnd.google-apps.presentation": "text/plain",
		// Drawings, Forms, Folders have no sensible text export — return ""
		// so the caller surfaces a clear error instead of dispatching a 400.
		"application/vnd.google-apps.drawing": "",
		"application/vnd.google-apps.form":    "",
		"application/vnd.google-apps.folder":  "",
		"application/pdf":                     "",
		"":                                    "",
	}
	for in, want := range cases {
		if got := defaultExportMIME(in); got != want {
			t.Errorf("defaultExportMIME(%q) = %q, want %q", in, got, want)
		}
	}
}

// --- buildMultipartUpload ---

func TestBuildMultipartUpload_ContentTypeAdvertisesRelated(t *testing.T) {
	_, ct, err := buildMultipartUpload(
		map[string]any{"name": "f.txt"},
		"text/plain",
		[]byte("hi"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(ct, "multipart/related; boundary=") {
		t.Errorf("content-type should be multipart/related; got %q", ct)
	}
}

func TestBuildMultipartUpload_BodyContainsMetadataAndContent(t *testing.T) {
	metadata := map[string]any{
		"name":     "notes.md",
		"mimeType": "text/markdown",
	}
	body, ct, err := buildMultipartUpload(metadata, "text/markdown", []byte("# Hello\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const boundaryParam = "boundary="
	idx := strings.Index(ct, boundaryParam)
	if idx < 0 {
		t.Fatalf("no boundary in content-type %q", ct)
	}
	boundary := ct[idx+len(boundaryParam):]
	sep := "--" + boundary

	bodyStr := string(body)
	if !strings.Contains(bodyStr, sep) {
		t.Errorf("body missing boundary separator %q; body=\n%s", sep, bodyStr)
	}
	if !strings.Contains(bodyStr, "Content-Type: application/json; charset=UTF-8") {
		t.Errorf("body missing metadata part Content-Type; body=\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, "Content-Type: text/markdown") {
		t.Errorf("body missing content part Content-Type; body=\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"name":"notes.md"`) {
		t.Errorf("body missing metadata json; body=\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, "# Hello\n") {
		t.Errorf("body missing content; body=\n%s", bodyStr)
	}
}

func TestBuildMultipartUpload_RoundTripsViaStdlibReader(t *testing.T) {
	// Independent verification that the body we build is parseable by
	// the standard library's multipart reader — i.e. boundary, part
	// headers, and CRLF separators are wire-correct. If this breaks,
	// Drive's parser will reject the upload too.
	metadata := map[string]any{"name": "x.txt"}
	body, ct, err := buildMultipartUpload(metadata, "text/plain", []byte("payload"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, params, err := mime.ParseMediaType(ct)
	if err != nil {
		t.Fatalf("parse content-type %q: %v", ct, err)
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	parts := 0
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read part: %v", err)
		}
		buf := new(bytes.Buffer)
		if _, err := buf.ReadFrom(p); err != nil {
			t.Fatalf("buffer part: %v", err)
		}
		parts++
		switch parts {
		case 1:
			if got := p.Header.Get("Content-Type"); got != "application/json; charset=UTF-8" {
				t.Errorf("part 1 content-type = %q", got)
			}
			if !strings.Contains(buf.String(), `"name":"x.txt"`) {
				t.Errorf("part 1 body = %q", buf.String())
			}
		case 2:
			if got := p.Header.Get("Content-Type"); got != "text/plain" {
				t.Errorf("part 2 content-type = %q", got)
			}
			if buf.String() != "payload" {
				t.Errorf("part 2 body = %q, want %q", buf.String(), "payload")
			}
		}
	}
	if parts != 2 {
		t.Errorf("expected 2 parts, got %d", parts)
	}
}

// --- driveParentsList ---

func TestDriveParentsList_StringFormSplitsAndTrims(t *testing.T) {
	got := driveParentsList("folder1, folder2,folder3")
	if got != "folder1,folder2,folder3" {
		t.Errorf("got %q, want %q", got, "folder1,folder2,folder3")
	}
}

func TestDriveParentsList_ArrayForm(t *testing.T) {
	got := driveParentsList([]any{"folder1", " folder2 ", "folder3"})
	if got != "folder1,folder2,folder3" {
		t.Errorf("got %q, want %q", got, "folder1,folder2,folder3")
	}
}

func TestDriveParentsList_DropsEmptiesAndNonStrings(t *testing.T) {
	got := driveParentsList([]any{"a", "", 42, "  ", "b"})
	if got != "a,b" {
		t.Errorf("got %q, want %q", got, "a,b")
	}
}

func TestDriveParentsList_NilAndUnsupportedReturnEmpty(t *testing.T) {
	for _, in := range []any{nil, 42, 3.14, map[string]any{}, true} {
		if got := driveParentsList(in); got != "" {
			t.Errorf("input %#v: got %q, want empty", in, got)
		}
	}
}

// --- normalizeEmailFormat ---

func TestNormalizeEmailFormat(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"absent/nil defaults to metadata", nil, "metadata"},
		{"empty string defaults to metadata", "", "metadata"},
		{"explicit metadata", "metadata", "metadata"},
		{"full", "full", "full"},
		{"full case-insensitive", "FULL", "full"},
		{"full whitespace-tolerant", "  full  ", "full"},
		{"unrecognized falls back to metadata", "raw", "metadata"},
		{"non-string falls back to metadata", 42, "metadata"},
		{"bool falls back to metadata", true, "metadata"},
	}
	for _, c := range cases {
		if got := normalizeEmailFormat(c.in); got != c.want {
			t.Errorf("%s: normalizeEmailFormat(%#v) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// --- decodeGmailBase64URL ---

func b64url(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func TestDecodeGmailBase64URL(t *testing.T) {
	t.Run("decodes unpadded URL-safe", func(t *testing.T) {
		// "subjects?" contains bytes that exercise the URL-safe alphabet.
		enc := base64.RawURLEncoding.EncodeToString([]byte("hello >> world??"))
		got, ok := decodeGmailBase64URL(enc)
		if !ok || got != "hello >> world??" {
			t.Errorf("got (%q, %v)", got, ok)
		}
	})
	t.Run("decodes padded URL-safe fallback", func(t *testing.T) {
		enc := base64.URLEncoding.EncodeToString([]byte("padded body"))
		got, ok := decodeGmailBase64URL(enc)
		if !ok || got != "padded body" {
			t.Errorf("got (%q, %v)", got, ok)
		}
	})
	t.Run("empty returns false", func(t *testing.T) {
		if got, ok := decodeGmailBase64URL(""); ok || got != "" {
			t.Errorf("got (%q, %v), want (\"\", false)", got, ok)
		}
	})
	t.Run("garbage returns false", func(t *testing.T) {
		if got, ok := decodeGmailBase64URL("!!!not base64!!!"); ok || got != "" {
			t.Errorf("got (%q, %v), want (\"\", false)", got, ok)
		}
	})
}

// --- extractEmailBody ---

// emailPart is a tiny builder for a Gmail payload/part map in tests.
func emailPart(mimeType, data string, parts ...map[string]any) map[string]any {
	p := map[string]any{"mimeType": mimeType}
	if data != "" {
		p["body"] = map[string]any{"data": b64url(data)}
	}
	if len(parts) > 0 {
		anyParts := make([]any, len(parts))
		for i, sub := range parts {
			anyParts[i] = sub
		}
		p["parts"] = anyParts
	}
	return p
}

func TestExtractEmailBody_NonMultipartPlain(t *testing.T) {
	// A simple message carries its content directly in the top-level
	// payload.body.data with the top-level mimeType.
	payload := emailPart("text/plain", "the plain body")
	body, mt := extractEmailBody(payload)
	if body != "the plain body" || mt != "text/plain" {
		t.Errorf("got (%q, %q)", body, mt)
	}
}

func TestExtractEmailBody_MultipartAlternativePrefersPlain(t *testing.T) {
	payload := emailPart("multipart/alternative", "",
		emailPart("text/plain", "plain version"),
		emailPart("text/html", "<p>html version</p>"),
	)
	body, mt := extractEmailBody(payload)
	if body != "plain version" || mt != "text/plain" {
		t.Errorf("got (%q, %q), want plain preferred", body, mt)
	}
}

func TestExtractEmailBody_HTMLOnlyFallback(t *testing.T) {
	payload := emailPart("multipart/alternative", "",
		emailPart("text/html", "<p>only html</p>"),
	)
	body, mt := extractEmailBody(payload)
	if body != "<p>only html</p>" || mt != "text/html" {
		t.Errorf("got (%q, %q), want html fallback", body, mt)
	}
}

func TestExtractEmailBody_MultipartMixedWithAttachment(t *testing.T) {
	// The normal case: a body wrapped in multipart/mixed alongside an
	// attachment part that carries no inline `data` (just an attachmentId).
	// The walk must skip the attachment and still return the text body.
	attachment := map[string]any{
		"mimeType": "application/pdf",
		"filename": "invoice.pdf",
		"body":     map[string]any{"attachmentId": "abc123", "size": float64(1024)},
	}
	payload := emailPart("multipart/mixed", "",
		emailPart("text/plain", "see attached"),
		attachment,
	)
	body, mt := extractEmailBody(payload)
	if body != "see attached" || mt != "text/plain" {
		t.Errorf("got (%q, %q), want text body past attachment", body, mt)
	}
}

func TestExtractEmailBody_NestedMultipartAlternativeInsideMixed(t *testing.T) {
	// multipart/mixed → multipart/alternative (plain+html) + attachment.
	// Depth-first recursion must reach the nested plain part.
	attachment := map[string]any{
		"mimeType": "image/png",
		"body":     map[string]any{"attachmentId": "img1"},
	}
	payload := emailPart("multipart/mixed", "",
		emailPart("multipart/alternative", "",
			emailPart("text/plain", "nested plain"),
			emailPart("text/html", "<b>nested html</b>"),
		),
		attachment,
	)
	body, mt := extractEmailBody(payload)
	if body != "nested plain" || mt != "text/plain" {
		t.Errorf("got (%q, %q), want nested plain", body, mt)
	}
}

func TestExtractEmailBody_NoTextPartReturnsEmpty(t *testing.T) {
	// An attachment-only payload (no decodable text part) yields nothing,
	// signaling the caller to omit the body field rather than emit garbage.
	payload := emailPart("multipart/mixed", "",
		map[string]any{
			"mimeType": "application/pdf",
			"body":     map[string]any{"attachmentId": "only-attachment"},
		},
	)
	body, mt := extractEmailBody(payload)
	if body != "" || mt != "" {
		t.Errorf("got (%q, %q), want empty", body, mt)
	}
}

func TestExtractEmailBody_SkipsUndecodableThenFindsNext(t *testing.T) {
	// A text/plain part whose data is not valid base64url must be skipped
	// so a later well-formed text/html part is still returned.
	bad := map[string]any{
		"mimeType": "text/plain",
		"body":     map[string]any{"data": "!!!garbage!!!"},
	}
	payload := emailPart("multipart/alternative", "",
		bad,
		emailPart("text/html", "<p>fallback</p>"),
	)
	body, mt := extractEmailBody(payload)
	if body != "<p>fallback</p>" || mt != "text/html" {
		t.Errorf("got (%q, %q), want html after skipping bad plain", body, mt)
	}
}

// --- normalizeAttachments ---

func TestNormalizeAttachments_NilAndEmptyReturnNil(t *testing.T) {
	for _, in := range []any{nil, []any{}} {
		got, err := normalizeAttachments(in)
		if err != nil {
			t.Fatalf("input %#v: unexpected error %v", in, err)
		}
		if got != nil {
			t.Errorf("input %#v: got %#v, want nil", in, got)
		}
	}
}

func TestNormalizeAttachments_ParsesObjectsAndDefaultsMIME(t *testing.T) {
	in := []any{
		map[string]any{"filename": "report.html", "content": "<html></html>", "mimeType": "text/html"},
		map[string]any{"filename": " notes.txt ", "content": "hi"}, // mimeType defaults, filename trimmed
	}
	got, err := normalizeAttachments(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d attachments, want 2", len(got))
	}
	if got[0].filename != "report.html" || got[0].mimeType != "text/html" || got[0].content != "<html></html>" {
		t.Errorf("attachment 0 = %+v", got[0])
	}
	if got[1].filename != "notes.txt" || got[1].mimeType != "text/plain" || got[1].content != "hi" {
		t.Errorf("attachment 1 = %+v (filename should be trimmed, mimeType default text/plain)", got[1])
	}
}

func TestNormalizeAttachments_RejectsNonArray(t *testing.T) {
	if _, err := normalizeAttachments("not-an-array"); err == nil {
		t.Error("expected error for non-array attachments")
	}
}

func TestNormalizeAttachments_RejectsNonObjectElement(t *testing.T) {
	if _, err := normalizeAttachments([]any{"just-a-string"}); err == nil {
		t.Error("expected error for non-object element")
	}
}

func TestNormalizeAttachments_RequiresFilename(t *testing.T) {
	in := []any{map[string]any{"content": "hi"}}
	if _, err := normalizeAttachments(in); err == nil {
		t.Error("expected error when filename missing")
	}
	in = []any{map[string]any{"filename": "   ", "content": "hi"}}
	if _, err := normalizeAttachments(in); err == nil {
		t.Error("expected error when filename is blank")
	}
}

func TestNormalizeAttachments_RequiresStringContent(t *testing.T) {
	in := []any{map[string]any{"filename": "f.txt"}}
	if _, err := normalizeAttachments(in); err == nil {
		t.Error("expected error when content missing")
	}
	in = []any{map[string]any{"filename": "f.txt", "content": 42}}
	if _, err := normalizeAttachments(in); err == nil {
		t.Error("expected error when content is not a string")
	}
}

func TestNormalizeAttachments_RejectsNonTextMIME(t *testing.T) {
	in := []any{map[string]any{"filename": "doc.pdf", "content": "%PDF-1.4", "mimeType": "application/pdf"}}
	_, err := normalizeAttachments(in)
	if err == nil {
		t.Fatal("expected error for non-text mimeType (binary out of scope)")
	}
	if !strings.Contains(err.Error(), "application/pdf") {
		t.Errorf("error should name the rejected mimeType; got %v", err)
	}
}

// --- buildRFC2822WithAttachments ---

func TestBuildRFC2822WithAttachments_NoAttachmentsIsByteForByteUnchanged(t *testing.T) {
	// The plain and reply outputs must be identical to the pre-attachment
	// helpers when no attachments are present — the no-op guarantee.
	plainWant := buildRFC2822("alice@example.com", "cc@example.com", "", "Hello", "world\n")
	plainGot, err := buildRFC2822WithAttachments("alice@example.com", "cc@example.com", "", "Hello", "world\n", "", "", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plainGot != plainWant {
		t.Errorf("plain path differs:\n got=%q\nwant=%q", plainGot, plainWant)
	}

	replyWant := buildRFC2822Reply("a@x.com", "", "", "Re: Hi", "body", "<msg@x>", "<ref@x> <msg@x>")
	replyGot, err := buildRFC2822WithAttachments("a@x.com", "", "", "Re: Hi", "body", "<msg@x>", "<ref@x> <msg@x>", "", []emailAttachment{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if replyGot != replyWant {
		t.Errorf("reply path differs:\n got=%q\nwant=%q", replyGot, replyWant)
	}
}

func TestBuildRFC2822WithAttachments_EmitsMultipartMixedWithBoundary(t *testing.T) {
	atts := []emailAttachment{{filename: "report.html", mimeType: "text/html", content: "<h1>Hi</h1>"}}
	msg, err := buildRFC2822WithAttachments("alice@example.com", "", "", "Report", "See attached", "", "", "", atts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(msg, "MIME-Version: 1.0\r\n") {
		t.Error("multipart message must carry MIME-Version header")
	}
	if !strings.Contains(msg, "Content-Type: multipart/mixed; boundary=\"") {
		t.Errorf("message must declare multipart/mixed with boundary; got:\n%s", msg)
	}
	if !strings.Contains(msg, "To: alice@example.com\r\n") || !strings.Contains(msg, "Subject: Report\r\n") {
		t.Error("message must preserve To/Subject headers")
	}
}

func TestBuildRFC2822WithAttachments_AttachmentPartIsBase64WithDisposition(t *testing.T) {
	content := "<html><body>Flight report</body></html>"
	atts := []emailAttachment{{filename: "flight.html", mimeType: "text/html", content: content}}
	msg, err := buildRFC2822WithAttachments("a@x.com", "", "", "S", "body text", "", "", "", atts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Parse the multipart body with the stdlib reader as independent
	// verification that our wire format is correct.
	idx := strings.Index(msg, "\r\n\r\n")
	if idx < 0 {
		t.Fatal("no header/body separator")
	}
	headerBlock := msg[:idx]
	bodyBlock := msg[idx+4:]

	ctLine := ""
	for _, line := range strings.Split(headerBlock, "\r\n") {
		if strings.HasPrefix(line, "Content-Type:") {
			ctLine = strings.TrimSpace(strings.TrimPrefix(line, "Content-Type:"))
		}
	}
	_, params, err := mime.ParseMediaType(ctLine)
	if err != nil {
		t.Fatalf("parse content-type %q: %v", ctLine, err)
	}
	mr := multipart.NewReader(strings.NewReader(bodyBlock), params["boundary"])

	// Part 1: body.
	bodyPart, err := mr.NextPart()
	if err != nil {
		t.Fatalf("read body part: %v", err)
	}
	bb := new(bytes.Buffer)
	if _, err := bb.ReadFrom(bodyPart); err != nil {
		t.Fatalf("buffer body part: %v", err)
	}
	if bb.String() != "body text" {
		t.Errorf("body part = %q, want %q", bb.String(), "body text")
	}

	// Part 2: attachment.
	attPart, err := mr.NextPart()
	if err != nil {
		t.Fatalf("read attachment part: %v", err)
	}
	if got := attPart.Header.Get("Content-Transfer-Encoding"); got != "base64" {
		t.Errorf("attachment CTE = %q, want base64", got)
	}
	if got := attPart.Header.Get("Content-Disposition"); !strings.Contains(got, `filename="flight.html"`) {
		t.Errorf("attachment disposition = %q, want attachment; filename=flight.html", got)
	}
	ab := new(bytes.Buffer)
	if _, err := ab.ReadFrom(attPart); err != nil {
		t.Fatalf("buffer attachment part: %v", err)
	}
	// The stdlib multipart reader does NOT base64-decode; the raw part
	// body is the wrapped base64 text. Strip whitespace and decode to
	// confirm it round-trips to the original content.
	raw := strings.ReplaceAll(strings.ReplaceAll(ab.String(), "\r\n", ""), "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("attachment body is not valid base64: %v", err)
	}
	if string(decoded) != content {
		t.Errorf("decoded attachment = %q, want %q", string(decoded), content)
	}

	if _, err := mr.NextPart(); err != io.EOF {
		t.Errorf("expected exactly 2 parts; got extra or error %v", err)
	}
}

func TestBuildRFC2822WithAttachments_Base64LinesWrappedAt76(t *testing.T) {
	// A long attachment forces multiple base64 lines; each must be <=76
	// chars per RFC 2045 so MTAs don't reject the message.
	long := strings.Repeat("A", 500)
	atts := []emailAttachment{{filename: "big.txt", mimeType: "text/plain", content: long}}
	msg, err := buildRFC2822WithAttachments("a@x.com", "", "", "S", "b", "", "", "", atts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Find the attachment part body region and check no base64 line
	// exceeds 76 chars. We look at lines that are pure base64 alphabet.
	for _, line := range strings.Split(msg, "\r\n") {
		if len(line) > 76 {
			// Header lines can legitimately exceed 76; only flag lines
			// that look like base64 payload (no colon, no boundary marker).
			if !strings.Contains(line, ":") && !strings.HasPrefix(line, "--") {
				t.Errorf("base64 line exceeds 76 chars (len=%d): %q", len(line), line)
			}
		}
	}
}

func TestNormalizeAttachments_StripsControlCharsFromFilename(t *testing.T) {
	// A filename with CR/LF must not survive to inject MIME headers.
	in := []any{map[string]any{
		"filename": "evil\r\nBcc: attacker@example.com.txt",
		"content":  "x",
	}}
	got, err := normalizeAttachments(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.ContainsAny(got[0].filename, "\r\n") {
		t.Errorf("filename retained CR/LF: %q", got[0].filename)
	}
	if got[0].filename != "evilBcc: attacker@example.com.txt" {
		t.Errorf("filename = %q", got[0].filename)
	}
}

func TestNormalizeAttachments_RejectsMIMEWithControlOrStructuralChars(t *testing.T) {
	for _, mt := range []string{"text/plain\r\nBcc: x@y.com", "text/plain; boundary=x", `text/plain"`} {
		in := []any{map[string]any{"filename": "f.txt", "content": "x", "mimeType": mt}}
		if _, err := normalizeAttachments(in); err == nil {
			t.Errorf("mimeType %q should be rejected", mt)
		}
	}
}

func TestBuildRFC2822WithAttachments_EscapesQuoteInFilename(t *testing.T) {
	atts := []emailAttachment{{filename: `a"b.txt`, mimeType: "text/plain", content: "x"}}
	msg, err := buildRFC2822WithAttachments("a@x.com", "", "", "S", "b", "", "", "", atts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(msg, `filename="a\"b.txt"`) {
		t.Errorf("quote in filename not escaped; message:\n%s", msg)
	}
}
