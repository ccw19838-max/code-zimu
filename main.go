package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18091", "listen address")
	public := flag.String("public-url", "http://127.0.0.1:18091", "exact browser-facing origin")
	demo := flag.Bool("demo", false, "show illustrative projects without GitHub requests")
	org := flag.String("github-org", "", "GitHub organization login")
	navigation := flag.String("navigation-url", "", "optional link back to the navigation application")
	profiles := flag.String("profiles", "", "optional server-side JSON project profiles for live repositories")
	flag.Parse()
	a, err := New(Config{PublicURL: *public, GitHubDemo: *demo, GitHubOrg: *org, NavigationURL: *navigation, ProfilesPath: *profiles}, os.Getenv("GITHUB_CATALOG_ADMIN_PASSWORD"))
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Addr: *addr, Handler: a, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Printf("GitHub catalog listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
