# dyk

Did you know? One fact at a time from Wikipedia's [Did you know archive](https://en.wikipedia.org/wiki/Wikipedia:Did_you_know_archive), each linking to the article behind it. No accounts, no tracking, no JavaScript. See [dyk-product-spec.md](dyk-product-spec.md) for the full spec.

One Go program serves the pages and, in the background, fills a SQLite file from Wikipedia. HTML and CSS are compiled in, so deploying means copying one binary.

## Run

Needs only Go. The SQLite driver is pure Go, so `CGO_ENABLED=0 GOOS=linux go build` cross-compiles from any machine.

```sh
go run . -contact you@example.com    # http://localhost:8080
go test ./...
```

The first ingest starts a few seconds after launch and fetches every month since `-since`, two seconds apart, so it takes about ten minutes. Cards appear as each month lands.

| Flag | Default | Purpose |
|---|---|---|
| `-addr` | `:8080` | Listen address |
| `-db` | `dyk.db` | Database file |
| `-since` | `2010-01` | First archive month to ingest |
| `-ingest-every` | `24h` | How often to check for new months |
| `-contact` | none | Email or URL for the User-Agent sent to Wikipedia |

## Operate

- **Deploy:** `go build` and copy the binary to the server, behind a reverse proxy that handles TLS.
- **Back up:** `sqlite3 dyk.db ".backup dyk-backup.db"` works while the server is running.
- **Hide a card:** `sqlite3 dyk.db "UPDATE cards SET hidden = 1 WHERE id = 4821"`. Cards are never deleted, so links stay stable.
- **Re-fetch a month:** `sqlite3 dyk.db "DELETE FROM ingested_pages WHERE page = 'Wikipedia:Did you know archive/2010/January'"`. The next ingest fetches it again.
- **Monitor:** the log has one line per month fetched, with how many hooks it found, how many were new cards and how many were skipped. A month with no hooks suggests Wikipedia changed its archive format.

## Styling

`web/static/bevel.css` is a release of [bevel.css](https://bevel.khush.sh), copied in unchanged; its first line names the version. dyk's own styles are in `web/static/app.css`. Static files are linked with a hash of their content, so browsers cache them for a year and pick up a new version as soon as it's deployed.
