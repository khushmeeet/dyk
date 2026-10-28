package main

import "testing"

// The connection settings are passed in the database address, where a
// misspelling is silently ignored, so check they took effect.
func TestOpenDBSettings(t *testing.T) {
	db := testDB(t)
	db.wantRows(`PRAGMA journal_mode`, "wal")
	db.wantRows(`PRAGMA busy_timeout`, "5000")
	db.wantRows(`PRAGMA synchronous`, "1") // NORMAL
}
