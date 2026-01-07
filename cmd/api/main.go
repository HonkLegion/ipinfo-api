package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"ipinfo-api/internal/config"
	"ipinfo-api/internal/dataset"
	"ipinfo-api/internal/handler"
	"ipinfo-api/internal/server"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	store := dataset.NewStore()

	reloadFromRemote := func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, cfg.IPInfo.DownloadTimeout)
		defer cancel()

		if err := dataset.DownloadToFile(
			ctx,
			cfg.IPInfo.DumpURL,
			cfg.IPInfo.Token,
			cfg.IPInfo.CacheFile,
		); err != nil {
			return err
		}

		v4, v6, err := dataset.LoadFromFile(cfg.IPInfo.CacheFile)
		if err != nil {
			return err
		}

		store.Load(v4, v6)
		log.Printf("dataset loaded from remote: v4=%d v6=%d", len(v4), len(v6))
		return nil
	}

	if dataset.CacheFresh(cfg.IPInfo.CacheFile, cfg.IPInfo.RefreshInterval) {
		log.Println("using cached dataset from disk")
		if v4, v6, err := dataset.LoadFromFile(cfg.IPInfo.CacheFile); err == nil {
			store.Load(v4, v6)
		}
	}

	if !store.Ready() {
		log.Println("no valid cache found, downloading dataset")
		if err := reloadFromRemote(context.Background()); err != nil {
			log.Fatal(err)
		}
	}

	go func() {
		t := time.NewTicker(cfg.IPInfo.RefreshInterval)
		for range t.C {
			_ = reloadFromRemote(context.Background())
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/ip/", handler.IP(store))
	mux.HandleFunc("/reload", handler.Reload(reloadFromRemote))
	mux.HandleFunc("/healthz", handler.Health())
	mux.HandleFunc("/readyz", handler.Ready(store))

	srv := &http.Server{
		Addr:    cfg.Server.Listen,
		Handler: mux,
	}

	server.Run(srv, cfg.Server.ShutdownTimeout, func() {
		_ = reloadFromRemote(context.Background())
	})
}
