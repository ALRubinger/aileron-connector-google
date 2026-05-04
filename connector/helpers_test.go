package main

import (
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
