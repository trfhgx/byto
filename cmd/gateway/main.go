package main

import (
	"log"
	"net/http"
	"os"

	"github.com/example/go-llm-gateway/internal/config"
	"github.com/example/go-llm-gateway/internal/server"
)

func main() {
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
	log.Printf("llm gateway listening on :%s project=%s location=%s default_model=%s", port, cfg.Project, cfg.Location, cfg.DefaultModel)
	log.Fatal(http.ListenAndServe(":"+port, srv.Routes()))
}
