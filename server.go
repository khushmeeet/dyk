package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

const (
	msgNotFound    = "That card doesn't exist or has been removed."
	msgNoPage      = "There's nothing at this address."
	msgServerError = "The card couldn't be loaded. Try another one."
)

// The security policy allows the page's own styles and nothing else: no
// scripts, no forms, no embedding in other sites. default-src covers the
// prefetch of the next card; data: covers the empty favicon.
const securityPolicy = "default-src 'self'; img-src 'self' data:; script-src 'none'; object-src 'none'; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// An asset is a static file, held in memory with a gzipped copy and a short
// hash of its content for its URL.
type asset struct {
	body, gzipped []byte
	hash          string
	contentType   string
}

type server struct {
	db        *sql.DB
	templates *template.Template
	assets    map[string]asset
	mux       *http.ServeMux
}

// page is what every template is given.
type page struct {
	Title   string
	Refresh bool  // reload the page every 20 seconds
	Next    int64 // the card that Next links to and prefetches, if any
	Card    *cardView
	Message string
}

type cardView struct {
	card
	ArticleURL string
	SourceURL  string
}

func newServer(db *sql.DB, web fs.FS) (*server, error) {
	s := &server{db: db, assets: map[string]asset{}, mux: http.NewServeMux()}

	files, err := fs.ReadDir(web, "static")
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		body, err := fs.ReadFile(web, "static/"+f.Name())
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(body)
		var gz bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
		zw.Write(body)
		zw.Close()
		s.assets[f.Name()] = asset{
			body:        body,
			gzipped:     gz.Bytes(),
			hash:        hex.EncodeToString(sum[:4]),
			contentType: mime.TypeByExtension(path.Ext(f.Name())),
		}
	}

	s.templates, err = template.New("").Funcs(template.FuncMap{"asset": s.assetURL}).ParseFS(web, "templates/*.html")
	if err != nil {
		return nil, err
	}

	s.mux.HandleFunc("GET /{$}", s.home)
	s.mux.HandleFunc("GET /c/{id}", s.card)
	s.mux.HandleFunc("GET /static/{name}", s.static)
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.message(w, http.StatusNotFound, msgNoPage)
	})
	return s, nil
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", securityPolicy)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	s.mux.ServeHTTP(w, r)
}

// assetURL returns a static file's URL, carrying its hash so the browser can
// cache it for good: when the file changes, so does its URL.
func (s *server) assetURL(name string) (string, error) {
	a, ok := s.assets[name]
	if !ok {
		return "", fmt.Errorf("no static file %q", name)
	}
	return "/static/" + name + "?v=" + a.hash, nil
}

// home redirects to a random card.
func (s *server) home(w http.ResponseWriter, r *http.Request) {
	id, err := randomCard(r.Context(), s.db, 0)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		w.Header().Set("Cache-Control", "no-store")
		s.render(w, http.StatusOK, "empty.html", page{Title: "Collecting cards · dyk", Refresh: true})
	case err != nil:
		s.fail(w, err)
	default:
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, "/c/"+strconv.FormatInt(id, 10), http.StatusFound)
	}
}

// card shows one card, with a random next card linked and prefetched.
func (s *server) card(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		s.message(w, http.StatusNotFound, msgNotFound)
		return
	}
	c, err := getCard(r.Context(), s.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		s.message(w, http.StatusNotFound, msgNotFound)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	next, err := randomCard(r.Context(), s.db, id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.fail(w, err)
		return
	}

	// Cached briefly, so the browser can use the prefetched copy when Next is
	// tapped. Private, so a proxy never hands everyone the same next card.
	w.Header().Set("Cache-Control", "private, max-age=300")
	s.render(w, http.StatusOK, "card.html", page{
		Title: c.Article + " · dyk",
		Next:  next,
		Card: &cardView{
			card:       c,
			ArticleURL: wikipediaURL(c.Article),
			SourceURL:  wikipediaURL(c.Source),
		},
	})
}

// static serves a static file. A URL carrying the file's current hash is
// cached for a year; any other is checked every time, so an old URL never
// gets stuck holding new content.
func (s *server) static(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assets[r.PathValue("name")]
	if !ok {
		s.message(w, http.StatusNotFound, msgNoPage)
		return
	}
	h := w.Header()
	h.Set("Content-Type", a.contentType)
	h.Set("Vary", "Accept-Encoding")
	if r.URL.Query().Get("v") == a.hash {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	body := a.body
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		body = a.gzipped
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.Write(body)
}

// message shows a short message with a button to a random card.
func (s *server) message(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Cache-Control", "no-store")
	s.render(w, status, "message.html", page{Title: "dyk", Message: msg})
}

func (s *server) fail(w http.ResponseWriter, err error) {
	log.Printf("error: %v", err)
	s.message(w, http.StatusInternalServerError, msgServerError)
}

// render executes a template in full before writing anything, so a
// template error becomes a plain error page instead of half of one.
func (s *server) render(w http.ResponseWriter, status int, name string, data page) {
	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, msgServerError, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

// wikipediaURL returns the address of a page on English Wikipedia.
func wikipediaURL(title string) string {
	u := url.URL{Scheme: "https", Host: "en.wikipedia.org", Path: "/wiki/" + strings.ReplaceAll(title, " ", "_")}
	return u.String()
}
