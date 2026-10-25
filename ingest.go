package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Wikipedia files each month's hooks on a page named after it, such as
// "Wikipedia:Did you know archive/2010/January".
const archivePrefix = "Wikipedia:Did you know archive/"

var (
	errNotFound = errors.New("page not found")
	redirect    = regexp.MustCompile(`(?i)^\s*#redirect\s*\[\[([^\]|#]+)`)
)

// An ingester fills the database from Wikipedia's monthly archives.
type ingester struct {
	db        *sql.DB
	client    *http.Client
	endpoint  string // MediaWiki's index.php
	userAgent string
	since     time.Time     // first month to fetch
	pause     time.Duration // between requests
}

// loop runs the ingest a few seconds after it starts, then every so often,
// until ctx is done.
func (in *ingester) loop(ctx context.Context, every time.Duration) {
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if err := in.run(ctx, time.Now()); err != nil && ctx.Err() == nil {
			log.Printf("ingest: %v; trying again in %s", err, every)
		}
		timer.Reset(every)
	}
}

// run fetches every finished month that hasn't been fetched yet, oldest
// first. It stops at the first page it can't fetch, and the next run picks
// up from there.
func (in *ingester) run(ctx context.Context, now time.Time) error {
	done, err := ingestedPages(ctx, in.db)
	if err != nil {
		return err
	}
	fetched := 0
	for _, page := range archivePages(in.since, now) {
		if done[page] {
			continue
		}
		if fetched > 0 {
			if err := sleep(ctx, in.pause); err != nil {
				return err
			}
		}
		fetched++
		if err := in.ingest(ctx, page); err != nil {
			return err
		}
	}
	return nil
}

// ingest fetches one archive page and saves its hooks.
func (in *ingester) ingest(ctx context.Context, page string) error {
	source, text, err := in.fetch(ctx, page)
	if errors.Is(err, errNotFound) {
		log.Printf("ingest: %s: not found, skipping", page)
		_, err = savePage(ctx, in.db, page, page, nil)
		return err
	}
	if err != nil {
		return err
	}

	hooks, skipped := parseArchive(text)
	added, err := savePage(ctx, in.db, page, source, hooks)
	if err != nil {
		return err
	}
	log.Printf("ingest: %s: %d hooks, %d new cards, %d skipped", page, len(hooks), added, skipped)
	if len(hooks) == 0 {
		log.Printf("ingest: %s had no hooks; has Wikipedia changed the archive format?", page)
	}
	return nil
}

// fetch returns a page's wikitext and its title, following one redirect so
// a page Wikipedia moves keeps working.
func (in *ingester) fetch(ctx context.Context, page string) (title, text string, err error) {
	text, err = in.get(ctx, page)
	if err != nil {
		return "", "", err
	}
	if m := redirect.FindStringSubmatch(text); m != nil {
		page = strings.TrimSpace(m[1])
		if err := sleep(ctx, in.pause); err != nil {
			return "", "", err
		}
		if text, err = in.get(ctx, page); err != nil {
			return "", "", err
		}
	}
	return page, text, nil
}

// get returns a page's wikitext, or errNotFound if there's no such page.
func (in *ingester) get(ctx context.Context, page string) (string, error) {
	u := in.endpoint + "?" + url.Values{"title": {page}, "action": {"raw"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", in.userAgent)
	resp, err := in.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return "", errNotFound
	default:
		return "", fmt.Errorf("%s: %s", page, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	return string(b), err
}

// archivePages returns the archive page for every month from since that
// ended at least three days before now, oldest first. The wait lets late
// additions to a month's page land before it is fetched, once.
func archivePages(since, now time.Time) []string {
	var pages []string
	m := time.Date(since.Year(), since.Month(), 1, 0, 0, 0, 0, time.UTC)
	for !m.AddDate(0, 1, 3).After(now) {
		pages = append(pages, fmt.Sprintf("%s%d/%s", archivePrefix, m.Year(), m.Month()))
		m = m.AddDate(0, 1, 0)
	}
	return pages
}

// sleep waits for d, or returns early with ctx's error.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
