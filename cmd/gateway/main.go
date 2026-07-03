package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/example/go-llm-gateway/internal/config"
	"github.com/example/go-llm-gateway/internal/server"
	"github.com/example/go-llm-gateway/internal/versioncheck"
)

func main() {
	if err := versioncheck.Check(context.Background()); err != nil {
		log.Fatalf("version check: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	srv, err := server.NewFromConfig(cfg)
	if err != nil {
		log.Fatalf("server init error: %v", err)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("llm gateway listening on :%s project=%s location=%s", port, cfg.Project, cfg.Location)
	log.Fatal(http.ListenAndServe(":"+port, srv.Routes()))
}
