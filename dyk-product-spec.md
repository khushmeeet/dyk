# dyk — Product Spec

**Status:** Draft v1
**Owner:** Khushmeet Shergill
**Last updated:** October 20, 2026

## 1. Summary

dyk (short for "Did you know") is a small web app that shows one interesting fact at a time. A person opens the site, reads a card, taps Next, and reads another. Each card links to the Wikipedia article behind it for anyone who wants to go deeper. There are no accounts, no tracking, and nothing to set up: open the site for a couple of minutes a day, scroll through some facts, close it.

The facts come from Wikipedia's "Did you know…" archive. Every hook in that archive was reviewed by Wikipedia editors and is backed by a cited source in its article, which makes it a better source than the viral "facts" posts that inspired the idea, many of which are inaccurate or invented.

## 2. Goals

- Show one random, sourced fact per card, with a link to the full Wikipedia article.
- Make reading effortless on a phone: open the site, read, tap Next.
- Run for years with little or no maintenance.
- Stay light enough to work well on slow connections and old or low-power hardware.

## 3. Non-goals (v1)

- User accounts, sign-ups, profiles, or saved favorites.
- Analytics, tracking, cookies, or any per-user data on the server.
- Images on cards (planned for later; see section 11).
- Comments, sharing buttons, or social features.
- Content from sources other than Wikipedia's "Did you know" archive.
- An admin interface. Occasional moderation is done directly in the database.

## 4. Guiding principles

These apply to every decision, in this order when they conflict:

1. **Simple code.** Small codebase, plain structure, easy to read years later.
2. **Minimal dependencies.** Go standard library plus a SQLite driver and nothing else.
3. **Durable.** Built on technology that will still compile and run in ten years.
4. **Minimal maintenance.** Runs for years without changes. External failures must not take the site down.
5. **Lightweight.** Works on degraded networks and less powerful hardware.
6. **Low energy use.** Small pages, no client-side scripting, an idle-friendly server.

## 5. User experience

### 5.1 Flow

1. The person opens the site.
2. They see one card with a single fact.
3. They tap **Next** and see a different random card.
4. If a fact interests them, they tap the link to read the Wikipedia article.
5. They close the site. Next time, they see new random cards.

### 5.2 Card contents

Each card shows:

- The fact as a single sentence, phrased as a question: "Did you know that … ?"
- A link reading "Read about [article title] on Wikipedia".
- A **Next** button below the card.

A small footer credits the source: "From Wikipedia's Did you know archive, shared under CC BY-SA 4.0", with a link to the archive page the fact came from.

### 5.3 Behaviour

- Cards are random. Opening the home page always shows a random card.
- Every card has its own permanent address (for example `/c/4821`). Links to a card keep working indefinitely, and the browser's back button returns to the previous card.
- Next is instant: each card page already knows which card comes next and asks the browser to load it in the background.
- With tens of thousands of cards and a few minutes of use a day, repeats are rare enough that the app does not track what a person has seen.

### 5.4 Design

- Styled with **bevel.css**, the shared CSS library used across personal apps, with a small app-specific stylesheet (`app.css`) for the card layout. See section 7.8 for how bevel.css is versioned and updated.
- Mobile-first. On phones, the card fills most of the screen and the Next button spans the full width so it is easy to reach with a thumb. On larger screens, the card is centred with a comfortable reading width.
- The fact is set large in a serif typeface for easy reading; everything else stays quiet.
- Light and dark modes follow the device setting. Dark mode also saves power on OLED screens.
- System fonts only, no web fonts.
- One subtle entrance animation on the card, disabled when the device asks for reduced motion.
- Accessible: visible keyboard focus, readable contrast in both modes, real links and buttons.

### 5.5 Edge states

| State | What the person sees |
|---|---|
| No cards yet (first start) | A message that the first cards are being collected from Wikipedia. The page reloads on its own every 20 seconds. |
| Card doesn't exist or was hidden | "That card doesn't exist or has been removed", with a button to show a random card. |
| Server error | "The card couldn't be loaded. Try another one." |

## 6. Content

### 6.1 Source

Wikipedia's monthly "Did you know" archives, at pages titled `Wikipedia:Recent additions/YYYY/Month`. Each month contains several hundred hooks. Archives from January 2010 onward give a starting library of tens of thousands of cards.

### 6.2 Processing

Each hook is converted from wikitext to plain text:

- Links, bold and italic markup, references, HTML tags and comments are removed.
- Simple formatting templates (non-breaking spaces, dashes, language wrappers) are expanded.
- Image notes such as "(pictured)" are removed, since cards are text-only.
- The bolded link in each hook identifies the article the card links to.
- Any hook that can't be converted cleanly is skipped rather than shown broken.

### 6.3 Quality and moderation

Did you know hooks are reviewed by Wikipedia editors before publication and must be supported by cited sources, so all ingested hooks are shown by default. Any card can be hidden by setting a flag in the database. Hidden cards are never shown and their addresses return "not found".

### 6.4 Licensing

Hook text is from Wikipedia and is licensed CC BY-SA 4.0. Every card credits Wikipedia and links to both the article and the archive page the hook came from.

## 7. Architecture

### 7.1 Overview

A single Go program that does two things in one process:

- **Web server:** serves the card pages and two CSS files.
- **Ingest job:** a background task that fills the database from Wikipedia without interfering with page requests.

Data lives in one SQLite file. HTML and CSS are compiled into the program, so deploying means copying a single binary.

### 7.2 Technology

| Area | Choice |
|---|---|
| Language | Go, standard library only (`net/http`, `html/template`, `embed`) |
| Database | SQLite, one file, write-ahead logging so reads continue during writes |
| SQLite driver | modernc.org/sqlite, pure Go, so building needs no C compiler and cross-compiling is easy (the only direct third-party dependency) |
| Front end | Server-rendered HTML, bevel.css plus app.css, no JavaScript |
| Hosting | Behind a reverse proxy that handles TLS, deployed with hearth |

### 7.3 Routes

| Route | Behaviour |
|---|---|
| `GET /` | Redirect to a random card. Shows the "collecting cards" page if the database is empty. |
| `GET /c/{id}` | Render that card, with the next random card linked and prefetched. |
| `GET /static/…` | bevel.css and app.css. URLs carry a content hash, so files are cached by the browser for a year (see 7.8). |

### 7.4 Data model

**cards**

| Column | Meaning |
|---|---|
| `id` | Permanent card number, used in card addresses |
| `text` | The hook, starting with "that" and ending with "?" (unique) |
| `article` | Title of the Wikipedia article the hook is about |
| `source` | Archive page the hook came from |
| `hidden` | 1 if the card should never be shown |
| `added_at` | When the card was ingested |

**ingested_pages**

| Column | Meaning |
|---|---|
| `page` | Archive page title |
| `cards` | Number of new cards it added |
| `fetched_at` | When it was fetched |

Cards are never deleted, only hidden, so card numbers and links stay stable.

### 7.5 Random selection

The server picks a random number between 1 and the highest card number and takes the first visible card at or after it, wrapping around if needed. This takes the same tiny amount of work whether there are a hundred cards or a million.

### 7.6 Ingest job

- Starts a few seconds after the program starts, then runs once a day.
- Fetches each monthly archive page once, starting from a configurable month (default January 2010), and records it as done. Past months never change, so after the first run the job only fetches the newest finished month.
- A month is only fetched once it has been over for a few days, so late additions are included.
- Waits about two seconds between requests to be considerate to Wikipedia.
- Sends a User-Agent with contact details, as Wikipedia's policy asks.
- Each month is saved in one short database transaction. Duplicate hooks are ignored.
- If Wikipedia can't be reached, the job stops and retries on the next run. A missing page is recorded and skipped.
- The website never depends on Wikipedia being available. It always serves the cards already in the database.

### 7.7 Configuration

All configuration is through command-line flags, with sensible defaults:

| Flag | Default | Purpose |
|---|---|---|
| `-addr` | `:8080` | Listen address |
| `-db` | `dyk.db` | Database file |
| `-since` | `2010-01` | First archive month to ingest |
| `-ingest-every` | `24h` | How often to check for new months |
| `-contact` | none | Email or URL for the User-Agent |

### 7.8 Shared styling: bevel.css versioning

bevel.css is shared across all personal apps but is **never loaded from a CDN or imported as a package**. Each app keeps its own copy, compiled into its binary. This keeps every app self-contained: it builds and runs on its own forever, sends no requests to third-party servers, and works under the strict security policy that only allows files from its own domain.

**In the dyk repo**

- The file lives at a fixed path, `web/static/bevel.css`, with no version in the filename.
- Anything specific to dyk goes in `app.css`. The copy of bevel.css is never edited, so it always matches a released version and can be overwritten cleanly.
- At startup, the server computes a short content hash for each static file, and templates link to them through an `asset` helper: `{{asset "bevel.css"}}` produces `/static/bevel.css?v=3f9a1c2e`.
- Static files are served with `Cache-Control: public, max-age=31536000, immutable`. When a file changes, its hash and URL change, so browsers fetch the new version immediately and never use a stale one.

**In the bevel.css repo**

- Every release starts with a version line, for example `/*! bevel.css v1.1.0 */`. Tools read this line to tell which version an app has.
- `projects.txt` lists every app that uses bevel.css, one per line, with an optional path when the app keeps the file somewhere other than `web/static/bevel.css`.
- `sync.sh` updates the apps:
  - `./sync.sh --check` reports which apps are behind the latest release.
  - `./sync.sh` copies the release into each app that is behind, checks that the app still builds, and commits the change in that app's repo with the message "Update bevel.css to vX.Y.Z".

**Release routine**

1. Finish the change in the bevel.css repo and bump the version line.
2. Run `./sync.sh --check` to see which apps are behind.
3. Run `./sync.sh` to update and commit them all.
4. Open each updated app once to check for visual regressions, since the build check can't catch those.
5. Redeploy the apps as usual through hearth.

Each app gets its own commit, so a bad release can be rolled back per app with `git revert`. An app can be pinned to its current version by removing it from `projects.txt`.

## 8. Non-functional requirements

| Area | Requirement |
|---|---|
| Page size | A card page is around 1–2 KB of HTML; CSS is cached after the first visit. |
| Requests per card | One, since CSS is cached and nothing else loads. |
| Speed | Next feels instant thanks to prefetching. |
| Server resources | Runs comfortably on a Raspberry Pi-class machine and idles with minimal memory and CPU. |
| Resilience | Wikipedia outages don't affect the site. Server timeouts protect against slow or stuck connections. |
| Shutdown | Stops cleanly on interrupt or termination signals. |
| Security | No scripts allowed by the content security policy; no forms; no embedding in other sites. |
| Privacy | No cookies, accounts, analytics, or per-user data stored anywhere. |

## 9. Operations

- **Deploy:** build the binary and copy it to the server.
- **Back up:** copy the SQLite file (use SQLite's `.backup` command while the server is running).
- **Hide a card:** set `hidden = 1` on that card in the database.
- **Re-fetch a month:** delete its row from `ingested_pages`; the next ingest run fetches it again.
- **Update bevel.css:** run `sync.sh` from the bevel.css repo, then redeploy (see 7.8).
- **Monitor:** the program logs each fetched month and how many cards it added. A month reporting zero hooks suggests Wikipedia changed its archive format.

## 10. Success criteria

- A person can open the site on a phone and read a card within a second, even on a slow connection.
- The library holds tens of thousands of cards after the first ingest.
- The site runs for a year without code changes or manual intervention.
- Repeats within a typical few-minute session are rare.

## 11. Future work

- **Images:** the ingest job downloads small thumbnails (around 20 KB) with their attribution and stores them locally, so the site stays self-contained and doesn't depend on Wikimedia's servers at runtime.
- **Categories:** a small tag such as History, Science, or Nature, if a reliable way to classify hooks is found.
- **More sources:** other curated, sourced feeds, such as Wikipedia's "On this day".
- **Avoiding repeats:** keep a short list of recently seen cards in the browser only, never sent to the server.

## 12. Open questions

- **Domain.** Where the site will live, for example a subdomain like `dyk.khush.sh`.
- **Earliest archive month.** January 2010 is the default. Older archives may use different page names or formatting; this needs checking on the first real ingest.
