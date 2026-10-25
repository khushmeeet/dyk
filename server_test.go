package main

import (
	"compress/gzip"
	"context"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func testServer(t *testing.T) (*server, *testDatabase) {
	t.Helper()
	db := testDB(t)
	web, err := fs.Sub(embedded, "web")
	if err != nil {
		t.Fatal(err)
	}
	s, err := newServer(db.DB, web)
	if err != nil {
		t.Fatal(err)
	}
	return s, db
}

func (db *testDatabase) addCards(texts ...string) {
	db.t.Helper()
	for _, text := range texts {
		_, err := db.Exec(`INSERT INTO cards (text, article, source) VALUES (?, ?, ?)`,
			text, "Felix Barker", "Wikipedia:Did you know archive/2010/January")
		if err != nil {
			db.t.Fatal(err)
		}
	}
}

func get(t *testing.T, h http.Handler, target string, header ...string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestHomeWithNoCards(t *testing.T) {
	s, _ := testServer(t)
	resp := get(t, s, "/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	b := body(t, resp)
	for _, want := range []string{"being collected from Wikipedia", `<meta http-equiv="refresh" content="20">`} {
		if !strings.Contains(b, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
}

func TestHomeRedirectsToACard(t *testing.T) {
	s, db := testServer(t)
	db.addCards("that one?")
	resp := get(t, s, "/")
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/c/1" {
		t.Errorf("got %d to %q, want 302 to /c/1", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestCard(t *testing.T) {
	s, db := testServer(t)
	db.addCards("that the first card is <first>?", "that the second card is second?")

	resp := get(t, s, "/c/1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	b := body(t, resp)
	for _, want := range []string{
		"Did you know that the first card is &lt;first&gt;?",
		`<a href="https://en.wikipedia.org/wiki/Felix_Barker">Read about Felix Barker on Wikipedia</a>`,
		`<a href="https://en.wikipedia.org/wiki/Wikipedia:Did_you_know_archive/2010/January">`,
		"CC BY-SA 4.0",
		// The only other card is always next.
		`<link rel="prefetch" href="/c/2">`,
		`<a class="btn btn-primary btn-large next" href="/c/2">Next</a>`,
	} {
		if !strings.Contains(b, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "script-src 'none'") {
		t.Errorf("Content-Security-Policy = %q", resp.Header.Get("Content-Security-Policy"))
	}
	if got := resp.Header.Get("Cache-Control"); got != "private, max-age=300" {
		t.Errorf("Cache-Control = %q", got)
	}
}

func TestOnlyCardLinksHome(t *testing.T) {
	s, db := testServer(t)
	db.addCards("that it is alone?")
	b := body(t, get(t, s, "/c/1"))
	if strings.Contains(b, "prefetch") || !strings.Contains(b, `href="/">Next</a>`) {
		t.Errorf("with one card, Next should go to / with nothing prefetched:\n%s", b)
	}
}

func TestMissingCards(t *testing.T) {
	s, db := testServer(t)
	db.addCards("that it is hidden?")
	db.Exec(`UPDATE cards SET hidden = 1`)
	for _, path := range []string{"/c/1", "/c/2", "/c/0", "/c/abc"} {
		resp := get(t, s, path)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, resp.StatusCode)
		}
		if b := body(t, resp); !strings.Contains(b, "That card doesn&#39;t exist or has been removed.") {
			t.Errorf("%s: page is missing the message:\n%s", path, b)
		}
	}
	if resp := get(t, s, "/nowhere"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("/nowhere: status %d, want 404", resp.StatusCode)
	}
}

func TestStatic(t *testing.T) {
	s, _ := testServer(t)
	b := body(t, get(t, s, "/c/1"))
	m := regexp.MustCompile(`href="(/static/bevel\.css\?v=[0-9a-f]{8})"`).FindStringSubmatch(b)
	if m == nil {
		t.Fatalf("no bevel.css link in:\n%s", b)
	}

	resp := get(t, s, m[1], "Accept-Encoding", "gzip, br")
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Errorf("Content-Type = %q", got)
	}
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatal("not gzipped")
	}
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	css, _ := io.ReadAll(zr)
	if !strings.HasPrefix(string(css), "/*! bevel.css ") {
		t.Errorf("bevel.css doesn't start with its version line: %.40q", css)
	}

	// An old hash still gets the file, but not cached for good.
	resp = get(t, s, "/static/bevel.css?v=00000000")
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("old hash: Cache-Control = %q", got)
	}
	if resp.Header.Get("Content-Encoding") != "" {
		t.Error("gzipped without Accept-Encoding")
	}
	if resp := get(t, s, "/static/nope.css"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing file: status %d", resp.StatusCode)
	}
}

func TestRandomCard(t *testing.T) {
	db := testDB(t)
	db.addCards("that a?", "that b?", "that c?", "that d?", "that e?")
	db.Exec(`UPDATE cards SET hidden = 1 WHERE id IN (2, 5)`)
	ctx := context.Background()
	seen := map[int64]int{}
	for range 300 {
		id, err := randomCard(ctx, db.DB, 3)
		if err != nil {
			t.Fatal(err)
		}
		seen[id]++
	}
	if len(seen) != 2 || seen[1] == 0 || seen[4] == 0 {
		t.Errorf("picked %v, want only cards 1 and 4", seen)
	}
}

func TestWikipediaURL(t *testing.T) {
	tests := map[string]string{
		"Felix Barker":           "https://en.wikipedia.org/wiki/Felix_Barker",
		"AC/DC":                  "https://en.wikipedia.org/wiki/AC/DC",
		"What Is a Man?":         "https://en.wikipedia.org/wiki/What_Is_a_Man%3F",
		"Lý Anh Tông":            "https://en.wikipedia.org/wiki/L%C3%BD_Anh_T%C3%B4ng",
		"Palacio (Málaga) #1%":   "https://en.wikipedia.org/wiki/Palacio_%28M%C3%A1laga%29_%231%25",
		"Wikipedia:Did you know": "https://en.wikipedia.org/wiki/Wikipedia:Did_you_know",
	}
	for title, want := range tests {
		if got := wikipediaURL(title); got != want {
			t.Errorf("wikipediaURL(%q) = %q, want %q", title, got, want)
		}
	}
}
