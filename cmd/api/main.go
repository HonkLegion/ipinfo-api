package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"ipinfo-memory-api/internal/config"
	"ipinfo-memory-api/internal/dataset"
	"ipinfo-memory-api/internal/handler"
	"ipinfo-memory-api/internal/server"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	store := dataset.NewStore()

	reload := func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, cfg.IPInfo.DownloadTimeout)
		defer cancel()

		data, err := dataset.Download(ctx, cfg.IPInfo.DumpURL, cfg.IPInfo.Token)
		if err != nil {
			return err
		}

		v4, v6, err := dataset.ParseCSV(data)
		if err != nil {
			return err
		}

		store.Load(v4, v6)
		log.Printf("dataset loaded: v4=%d v6=%d", len(v4), len(v6))
		return nil
	}

	if err := reload(context.Background()); err != nil {
		log.Fatal("initial load failed:", err)
	}

	go func() {
		t := time.NewTicker(cfg.IPInfo.RefreshInterval)
		for range t.C {
			_ = reload(context.Background())
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/ip/", handler.IP(store))
	mux.HandleFunc("/reload", handler.Reload(reload))
	mux.HandleFunc("/healthz", handler.Health())
	mux.HandleFunc("/readyz", handler.Ready(store))

	srv := &http.Server{
		Addr:    cfg.Server.Listen,
		Handler: mux,
	}

	server.Run(srv, cfg.Server.ShutdownTimeout, func() {
		_ = reload(context.Background())
	})
}
