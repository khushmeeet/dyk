package main

import (
	"context"
	"database/sql"
	"errors"
	"math/rand/v2"

	_ "github.com/mattn/go-sqlite3"
)

// Cards are never deleted, only hidden, so card numbers and links stay
// stable. AUTOINCREMENT keeps a number from being reused even if one is.
const schema = `
CREATE TABLE IF NOT EXISTS cards (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	text     TEXT NOT NULL UNIQUE,
	article  TEXT NOT NULL,
	source   TEXT NOT NULL,
	hidden   INTEGER NOT NULL DEFAULT 0,
	added_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS ingested_pages (
	page       TEXT PRIMARY KEY,
	cards      INTEGER NOT NULL,
	fetched_at TEXT NOT NULL DEFAULT (datetime('now'))
);
`

type card struct {
	ID      int64
	Text    string
	Article string
	Source  string
}

// openDB opens the database, creating it if needed. Write-ahead logging
// lets pages keep reading while the ingest job writes.
func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// getCard returns a visible card, or sql.ErrNoRows.
func getCard(ctx context.Context, db *sql.DB, id int64) (card, error) {
	c := card{ID: id}
	err := db.QueryRowContext(ctx,
		`SELECT text, article, source FROM cards WHERE id = ? AND hidden = 0`, id,
	).Scan(&c.Text, &c.Article, &c.Source)
	return c, err
}

// randomCard returns the number of a random visible card other than skip,
// or sql.ErrNoRows if there is none. It picks a random number up to the
// highest card number and takes the first visible card at or after it,
// wrapping around if needed, so it does the same work however many cards
// there are.
func randomCard(ctx context.Context, db *sql.DB, skip int64) (int64, error) {
	var top int64
	if err := db.QueryRowContext(ctx, `SELECT coalesce(max(id), 0) FROM cards`).Scan(&top); err != nil {
		return 0, err
	}
	if top == 0 {
		return 0, sql.ErrNoRows
	}
	var id int64
	err := db.QueryRowContext(ctx,
		`SELECT id FROM cards WHERE id >= ? AND id != ? AND hidden = 0 ORDER BY id LIMIT 1`,
		rand.Int64N(top)+1, skip,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = db.QueryRowContext(ctx,
			`SELECT id FROM cards WHERE id != ? AND hidden = 0 ORDER BY id LIMIT 1`, skip,
		).Scan(&id)
	}
	return id, err
}

// ingestedPages returns the archive pages already fetched.
func ingestedPages(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT page FROM ingested_pages`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	done := map[string]bool{}
	for rows.Next() {
		var page string
		if err := rows.Scan(&page); err != nil {
			return nil, err
		}
		done[page] = true
	}
	return done, rows.Err()
}

// savePage stores the hooks from an archive page and records the page as
// fetched, in one transaction. Hooks already stored are ignored. It returns
// how many cards were new.
func savePage(ctx context.Context, db *sql.DB, page, source string, hooks []hook) (int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO cards (text, article, source) VALUES (?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	added := 0
	for _, h := range hooks {
		res, err := stmt.ExecContext(ctx, h.Text, h.Article, source)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		added += int(n)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO ingested_pages (page, cards) VALUES (?, ?)`, page, added,
	); err != nil {
		return 0, err
	}
	return added, tx.Commit()
}
