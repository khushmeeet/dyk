package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func testDB(t *testing.T) *testDatabase {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &testDatabase{t, db}
}

func TestArchivePages(t *testing.T) {
	since := time.Date(2010, time.January, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		now  time.Time
		want []string
	}{
		{time.Date(2010, time.February, 3, 23, 0, 0, 0, time.UTC), nil},
		{time.Date(2010, time.February, 4, 0, 0, 0, 0, time.UTC), []string{
			"Wikipedia:Did you know archive/2010/January",
		}},
		{time.Date(2010, time.March, 10, 0, 0, 0, 0, time.UTC), []string{
			"Wikipedia:Did you know archive/2010/January",
			"Wikipedia:Did you know archive/2010/February",
		}},
	}
	for _, tt := range tests {
		if got := archivePages(since, tt.now); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("archivePages(%v) = %q, want %q", tt.now, got, tt.want)
		}
	}
}

func TestIngest(t *testing.T) {
	pages := map[string]string{
		// Moved, as the "Recent additions" pages were.
		"Wikipedia:Did you know archive/2010/January": "#REDIRECT [[Wikipedia:Old archive/2010/January]]",
		"Wikipedia:Old archive/2010/January": "*... that '''[[Felix Barker]]''' was a critic?\n" +
			"*... that '''[[Ngwenya Mine]]''' is old?\n",
		// February is missing.
		"Wikipedia:Did you know archive/2010/March": "*... that '''[[Felix Barker]]''' was a critic?\n" +
			"*... that '''[[Tom Prydie]]''' plays rugby?\n",
	}
	var mu sync.Mutex
	var requested []string
	var failing bool
	wiki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if got := r.Header.Get("User-Agent"); got != "dyk (test@example.com)" {
			t.Errorf("User-Agent = %q", got)
		}
		if r.URL.Query().Get("action") != "raw" {
			t.Errorf("action = %q, want raw", r.URL.Query().Get("action"))
		}
		if failing {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		title := r.URL.Query().Get("title")
		requested = append(requested, title)
		text, ok := pages[title]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(text))
	}))
	defer wiki.Close()

	// takeRequested returns the titles requested so far and starts a new list.
	takeRequested := func() []string {
		mu.Lock()
		defer mu.Unlock()
		r := requested
		requested = nil
		return r
	}

	db := testDB(t)
	in := &ingester{
		db:        db.DB,
		client:    wiki.Client(),
		endpoint:  wiki.URL,
		userAgent: "dyk (test@example.com)",
		since:     time.Date(2010, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	ctx := context.Background()
	april := time.Date(2010, time.April, 10, 0, 0, 0, 0, time.UTC)
	if err := in.run(ctx, april); err != nil {
		t.Fatal(err)
	}

	wantRequests := []string{
		"Wikipedia:Did you know archive/2010/January",
		"Wikipedia:Old archive/2010/January",
		"Wikipedia:Did you know archive/2010/February",
		"Wikipedia:Did you know archive/2010/March",
	}
	if got := takeRequested(); !reflect.DeepEqual(got, wantRequests) {
		t.Errorf("requested %q, want %q", got, wantRequests)
	}
	db.wantRows(`SELECT text || ' / ' || article || ' / ' || source FROM cards ORDER BY id`,
		"that Felix Barker was a critic? / Felix Barker / Wikipedia:Old archive/2010/January",
		"that Ngwenya Mine is old? / Ngwenya Mine / Wikipedia:Old archive/2010/January",
		"that Tom Prydie plays rugby? / Tom Prydie / Wikipedia:Did you know archive/2010/March",
	)
	db.wantRows(`SELECT page || ' ' || cards FROM ingested_pages ORDER BY rowid`,
		"Wikipedia:Did you know archive/2010/January 2",
		"Wikipedia:Did you know archive/2010/February 0",
		"Wikipedia:Did you know archive/2010/March 1",
	)

	// The next run fetches only months it hasn't seen.
	if err := in.run(ctx, april); err != nil {
		t.Fatal(err)
	}
	if got := takeRequested(); len(got) != 0 {
		t.Errorf("second run requested %q, want nothing", got)
	}

	// When Wikipedia is down, the run stops and nothing is recorded.
	mu.Lock()
	failing = true
	mu.Unlock()
	if err := in.run(ctx, time.Date(2010, time.May, 10, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Error("run with Wikipedia down: want an error")
	}
	db.wantRows(`SELECT count(*) FROM ingested_pages`, "3")
}

// testDatabase wraps a database with helpers for checking its contents.
type testDatabase struct {
	t *testing.T
	*sql.DB
}

func (db *testDatabase) wantRows(query string, want ...string) {
	db.t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		db.t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			db.t.Fatal(err)
		}
		got = append(got, s)
	}
	if !reflect.DeepEqual(got, want) {
		db.t.Errorf("%s\n got: %q\nwant: %q", query, got, want)
	}
}
