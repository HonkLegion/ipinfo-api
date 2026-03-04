package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
	"gopkg.in/yaml.v3"
)

type IPInfo struct {
	Network       string `json:"network"`
	Country       string `json:"country"`
	CountryCode   string `json:"country_code"`
	Continent     string `json:"continent"`
	ContinentCode string `json:"continent_code"`
	ASN           string `json:"asn,omitempty"`
	ASName        string `json:"as_name,omitempty"`
	ASDomain      string `json:"as_domain,omitempty"`
}

type mmdbRecord struct {
	Country       string `maxminddb:"country"`
	CountryCode   string `maxminddb:"country_code"`
	Continent     string `maxminddb:"continent"`
	ContinentCode string `maxminddb:"continent_code"`
	ASN           string `maxminddb:"asn"`
	ASName        string `maxminddb:"as_name"`
	ASDomain      string `maxminddb:"as_domain"`
}

type Config struct {
	Server struct {
		Listen          string        `yaml:"listen"`
		ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	} `yaml:"server"`
	IPInfo struct {
		DumpURL         string        `yaml:"dump_url"`
		Token           string        `yaml:"token"`
		RefreshInterval time.Duration `yaml:"refresh_interval"`
		DownloadTimeout time.Duration `yaml:"download_timeout"`
		CacheFile       string        `yaml:"cache_file"`
	} `yaml:"ipinfo"`
}

type App struct {
	config    *Config
	database  atomic.Pointer[maxminddb.Reader]
	ready     atomic.Bool
	reloading atomic.Bool
}

func main() {
	cfg := loadConfig()
	app := &App{config: cfg}

	if err := app.reloadDataset(context.Background()); err != nil {
		log.Fatalf("Initial load failed: %v", err)
	}
	defer app.closeCurrentDatabase()

	mux := http.NewServeMux()
	mux.HandleFunc("/ip/", app.handleLookup)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", app.handleReady)
	mux.HandleFunc("/reload", app.handleManualReload)

	srv := &http.Server{
		Addr:    cfg.Server.Listen,
		Handler: mux,
	}

	go app.backgroundWorker()

	go func() {
		log.Printf("Listening on %s", cfg.Server.Listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

	<-stop
	log.Println("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}
}

func (a *App) handleLookup(w http.ResponseWriter, r *http.Request) {
	ipStr := strings.TrimPrefix(r.URL.Path, "/ip/")
	ip, err := netip.ParseAddr(ipStr)
	if err != nil {
		http.Error(w, "invalid ip", http.StatusBadRequest)
		return
	}

	db := a.database.Load()
	if db == nil {
		http.Error(w, "dataset not ready", http.StatusServiceUnavailable)
		return
	}

	info, err := lookupIP(db, ip)
	if err != nil {
		log.Printf("lookup failed for %s: %v", ip, err)
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if info == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(info); err != nil {
		log.Printf("response encode failed: %v", err)
	}
}

func (a *App) handleReady(w http.ResponseWriter, _ *http.Request) {
	if a.ready.Load() {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
		return
	}
	http.Error(w, "not ready", http.StatusServiceUnavailable)
}

func (a *App) handleManualReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	go func() {
		if err := a.reloadDataset(context.Background()); err != nil {
			log.Printf("Manual reload failed: %v", err)
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}

func lookupIP(reader *maxminddb.Reader, ip netip.Addr) (*IPInfo, error) {
	result := reader.Lookup(ip)
	if err := result.Err(); err != nil {
		return nil, err
	}
	if !result.Found() {
		return nil, nil
	}

	var rec mmdbRecord
	if err := result.Decode(&rec); err != nil {
		return nil, err
	}

	return &IPInfo{
		Network:       result.Prefix().String(),
		Country:       rec.Country,
		CountryCode:   rec.CountryCode,
		Continent:     rec.Continent,
		ContinentCode: rec.ContinentCode,
		ASN:           rec.ASN,
		ASName:        rec.ASName,
		ASDomain:      rec.ASDomain,
	}, nil
}

func (a *App) backgroundWorker() {
	ticker := time.NewTicker(a.config.IPInfo.RefreshInterval)
	defer ticker.Stop()

	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	defer signal.Stop(sighup)

	for {
		select {
		case <-ticker.C:
			log.Println("Periodic refresh")
			if err := a.reloadDataset(context.Background()); err != nil {
				log.Printf("Periodic refresh failed: %v", err)
			}
		case <-sighup:
			log.Println("SIGHUP reload")
			if err := a.reloadDataset(context.Background()); err != nil {
				log.Printf("SIGHUP reload failed: %v", err)
			}
		}
	}
}

func (a *App) reloadDataset(ctx context.Context) error {
	if !a.reloading.CompareAndSwap(false, true) {
		log.Println("Reload already in progress, skipping")
		return nil
	}
	defer a.reloading.Store(false)

	cfg := a.config.IPInfo
	cache := cfg.CacheFile
	tmp := cache + ".tmp"

	if !a.ready.Load() {
		if info, err := os.Stat(cache); err == nil && time.Since(info.ModTime()) <= cfg.RefreshInterval {
			db, err := openMMDB(cache)
			if err == nil {
				a.swapDatabase(db)
				a.ready.Store(true)
				log.Println("Loaded MMDB from disk cache")
				return nil
			}
			log.Printf("Cached MMDB is unusable, redownloading: %v", err)
		}
	}

	log.Println("Downloading MMDB dataset...")
	if err := downloadFile(ctx, cfg.DumpURL, cfg.Token, tmp, cfg.DownloadTimeout); err != nil {
		return err
	}
	defer os.Remove(tmp)

	validationDB, err := openMMDB(tmp)
	if err != nil {
		return err
	}
	if err := validationDB.Close(); err != nil {
		log.Printf("Validation MMDB close failed: %v", err)
	}

	if err := os.Rename(tmp, cache); err != nil {
		return err
	}

	db, err := openMMDB(cache)
	if err != nil {
		return err
	}

	a.swapDatabase(db)
	a.ready.Store(true)

	log.Println("MMDB dataset updated")
	return nil
}

func downloadFile(parent context.Context, url, token, dest string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil && filepath.Dir(dest) != "." {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	var reader io.Reader = resp.Body
	if strings.HasSuffix(url, ".gz") {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return err
		}
		defer gz.Close()
		reader = gz
	}

	_, err = io.Copy(out, reader)
	return err
}

func loadConfig() *Config {
	f, err := os.Open("config.yaml")
	if err != nil {
		log.Fatalf("Cannot open config.yaml: %v", err)
	}
	defer f.Close()

	cfg := &Config{}
	if err := yaml.NewDecoder(f).Decode(cfg); err != nil {
		log.Fatalf("Cannot decode config.yaml: %v", err)
	}

	if t := os.Getenv("HL_APP_IPINFO_TOKEN"); t != "" {
		cfg.IPInfo.Token = t
	}
	return cfg
}

func openMMDB(path string) (*maxminddb.Reader, error) {
	return maxminddb.Open(path)
}

func (a *App) swapDatabase(next *maxminddb.Reader) {
	prev := a.database.Swap(next)
	closeMMDB(prev, "Failed to close previous MMDB reader")
}

func (a *App) closeCurrentDatabase() {
	closeMMDB(a.database.Swap(nil), "Failed to close MMDB reader")
}

func closeMMDB(reader *maxminddb.Reader, logMessage string) {
	if reader == nil {
		return
	}
	if err := reader.Close(); err != nil {
		log.Printf("%s: %v", logMessage, err)
	}
}
