// Command dyk shows one fact at a time from Wikipedia's "Did you know"
// archive. One process serves the pages and, in the background, fills its
// SQLite database from Wikipedia.
package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed web
var embedded embed.FS

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "dyk.db", "database file")
	since := flag.String("since", "2010-01", "first archive month to ingest, as YYYY-MM")
	every := flag.Duration("ingest-every", 24*time.Hour, "how often to check for new months")
	contact := flag.String("contact", "", "email or URL for the User-Agent sent to Wikipedia")
	flag.Parse()

	first, err := time.Parse("2006-01", *since)
	if err != nil {
		log.Fatalf("-since: want YYYY-MM, got %q", *since)
	}
	userAgent := "dyk"
	if *contact != "" {
		userAgent += " (" + *contact + ")"
	} else {
		log.Print("warning: no -contact given; Wikipedia asks for contact details in the User-Agent")
	}

	db, err := openDB(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	web, _ := fs.Sub(embedded, "web")
	srv, err := newServer(db, web)
	if err != nil {
		log.Fatal(err)
	}
	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	in := &ingester{
		db:        db,
		client:    &http.Client{Timeout: time.Minute},
		endpoint:  "https://en.wikipedia.org/w/index.php",
		userAgent: userAgent,
		since:     first,
		pause:     2 * time.Second,
	}
	ingestDone := make(chan struct{})
	go func() {
		defer close(ingestDone)
		in.loop(ctx, *every)
	}()

	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.ListenAndServe() }()
	log.Printf("listening on %s", *addr)

	select {
	case err := <-serveErr:
		log.Fatal(err)
	case <-ctx.Done():
	}
	stop() // a second signal stops the program at once

	log.Print("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Print(err)
	}
	<-ingestDone
	db.Close()
}
