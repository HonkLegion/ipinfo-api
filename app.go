package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

/*
========================================================
MODELY
========================================================
*/

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

type IPv4Range struct {
	Start uint32
	End   uint32
	Data  *IPInfo
}

type IPv6Range struct {
	StartHi, StartLo uint64
	EndHi, EndLo     uint64
	Data             *IPInfo
}

type Dataset struct {
	IPv4 []IPv4Range
	IPv6 []IPv6Range
}

/*
========================================================
KONFIGURACE
========================================================
*/

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

/*
========================================================
APLIKACE
========================================================
*/

type App struct {
	config    *Config
	dataset   atomic.Pointer[Dataset]
	ready     atomic.Bool
	reloading atomic.Bool
}

/*
========================================================
MAIN
========================================================
*/

func main() {
	cfg := loadConfig()
	app := &App{config: cfg}

	// initial load (sync)
	if err := app.reloadDataset(context.Background()); err != nil {
		log.Fatalf("Initial load failed: %v", err)
	}

	// HTTP
	mux := http.NewServeMux()
	mux.HandleFunc("/ip/", app.handleLookup)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", app.handleReady)
	mux.HandleFunc("/reload", app.handleManualReload)

	srv := &http.Server{
		Addr:    cfg.Server.Listen,
		Handler: mux,
	}

	// background refresh + SIGHUP
	go app.backgroundWorker()

	// start server
	go func() {
		log.Printf("Listening on %s", cfg.Server.Listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP error: %v", err)
		}
	}()

	// shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	<-stop
	log.Println("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	srv.Shutdown(ctx)
}

/*
========================================================
HANDLERY
========================================================
*/

func (a *App) handleLookup(w http.ResponseWriter, r *http.Request) {
	ipStr := strings.TrimPrefix(r.URL.Path, "/ip/")
	ip := net.ParseIP(ipStr)
	if ip == nil {
		http.Error(w, "invalid ip", http.StatusBadRequest)
		return
	}

	ds := a.dataset.Load()
	if ds == nil {
		http.Error(w, "dataset not ready", http.StatusServiceUnavailable)
		return
	}

	var res *IPInfo
	if ip4 := ip.To4(); ip4 != nil {
		res = lookupIPv4(ds, ip4)
	} else {
		res = lookupIPv6(ds, ip)
	}

	if res == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func (a *App) handleReady(w http.ResponseWriter, _ *http.Request) {
	if a.ready.Load() {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready\n"))
		return
	}
	http.Error(w, "not ready", http.StatusServiceUnavailable)
}

func (a *App) handleManualReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	go a.reloadDataset(context.Background())
	w.WriteHeader(http.StatusAccepted)
}

/*
========================================================
LOOKUP
========================================================
*/

func lookupIPv4(ds *Dataset, ip net.IP) *IPInfo {
	val := ipToUint32(ip)
	i := sort.Search(len(ds.IPv4), func(i int) bool {
		return ds.IPv4[i].End >= val
	})
	if i < len(ds.IPv4) && val >= ds.IPv4[i].Start {
		return ds.IPv4[i].Data
	}
	return nil
}

func lookupIPv6(ds *Dataset, ip net.IP) *IPInfo {
	hi, lo := ipToUint128(ip)
	i := sort.Search(len(ds.IPv6), func(i int) bool {
		r := ds.IPv6[i]
		if hi < r.EndHi {
			return true
		}
		return hi == r.EndHi && lo <= r.EndLo
	})
	if i < len(ds.IPv6) {
		r := ds.IPv6[i]
		if hi > r.StartHi || (hi == r.StartHi && lo >= r.StartLo) {
			return r.Data
		}
	}
	return nil
}

/*
========================================================
BACKGROUND + RELOAD
========================================================
*/

func (a *App) backgroundWorker() {
	ticker := time.NewTicker(a.config.IPInfo.RefreshInterval)
	defer ticker.Stop()

	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)

	for {
		select {
		case <-ticker.C:
			log.Println("Periodic refresh")
			_ = a.reloadDataset(context.Background())
		case <-sighup:
			log.Println("SIGHUP reload")
			_ = a.reloadDataset(context.Background())
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

	// use cache on first start if fresh
	if !a.ready.Load() {
		if info, err := os.Stat(cache); err == nil {
			if time.Since(info.ModTime()) <= cfg.RefreshInterval {
				if ds, err := parseCSV(cache); err == nil {
					a.dataset.Store(ds)
					a.ready.Store(true)
					log.Println("Loaded dataset from disk cache")
					return nil
				}
			}
		}
	}

	// download
	log.Println("Downloading dataset...")
	if err := downloadFile(ctx, cfg.DumpURL, cfg.Token, tmp, cfg.DownloadTimeout); err != nil {
		return err
	}

	// parse tmp
	ds, err := parseCSV(tmp)
	if err != nil {
		return err
	}

	// atomický disk swap
	if err := os.Rename(tmp, cache); err != nil {
		return err
	}

	// atomický RAM swap
	a.dataset.Store(ds)
	a.ready.Store(true)

	log.Printf("Dataset updated: v4=%d v6=%d", len(ds.IPv4), len(ds.IPv6))
	return nil
}

/*
========================================================
DOWNLOAD + PARSE
========================================================
*/

func downloadFile(
	parent context.Context,
	url, token, dest string,
	timeout time.Duration,
) error {
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

func parseCSV(path string) (*Dataset, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var reader io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}

	csvr := csv.NewReader(bufio.NewReader(reader))
	header, err := csvr.Read()
	if err != nil {
		return nil, err
	}

	idx := map[string]int{}
	for i, h := range header {
		idx[h] = i
	}

	ds := &Dataset{}

	for {
		rec, err := csvr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		_, ipNet, err := net.ParseCIDR(rec[idx["network"]])
		if err != nil {
			continue
		}

		info := &IPInfo{
			Network:       rec[idx["network"]],
			Country:       rec[idx["country"]],
			CountryCode:   rec[idx["country_code"]],
			Continent:     rec[idx["continent"]],
			ContinentCode: rec[idx["continent_code"]],
			ASN:           rec[idx["asn"]],
			ASName:        rec[idx["as_name"]],
			ASDomain:      rec[idx["as_domain"]],
		}

		start, end := ipRange(ipNet)

		if ip4 := start.To4(); ip4 != nil {
			ds.IPv4 = append(ds.IPv4, IPv4Range{
				Start: ipToUint32(start),
				End:   ipToUint32(end),
				Data:  info,
			})
		} else {
			sHi, sLo := ipToUint128(start)
			eHi, eLo := ipToUint128(end)
			ds.IPv6 = append(ds.IPv6, IPv6Range{
				StartHi: sHi, StartLo: sLo,
				EndHi: eHi, EndLo: eLo,
				Data: info,
			})
		}
	}

	sort.Slice(ds.IPv4, func(i, j int) bool { return ds.IPv4[i].Start < ds.IPv4[j].Start })
	sort.Slice(ds.IPv6, func(i, j int) bool {
		if ds.IPv6[i].StartHi != ds.IPv6[j].StartHi {
			return ds.IPv6[i].StartHi < ds.IPv6[j].StartHi
		}
		return ds.IPv6[i].StartLo < ds.IPv6[j].StartLo
	})

	return ds, nil
}

/*
========================================================
HELPERS
========================================================
*/

func loadConfig() *Config {
	f, err := os.Open("config.yaml")
	if err != nil {
		log.Fatalf("Cannot open config.yaml: %v", err)
	}
	defer f.Close()

	cfg := &Config{}
	yaml.NewDecoder(f).Decode(cfg)

	if t := os.Getenv("HL_APP_IPINFO_TOKEN"); t != "" {
		cfg.IPInfo.Token = t
	}
	return cfg
}

func ipRange(n *net.IPNet) (net.IP, net.IP) {
	start := n.IP
	end := make(net.IP, len(n.IP))
	copy(end, n.IP)
	for i := range n.Mask {
		end[i] |= ^n.Mask[i]
	}
	return start, end
}

func ipToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func ipToUint128(ip net.IP) (uint64, uint64) {
	ip = ip.To16()
	hi := uint64(ip[0])<<56 | uint64(ip[1])<<48 | uint64(ip[2])<<40 | uint64(ip[3])<<32 |
		uint64(ip[4])<<24 | uint64(ip[5])<<16 | uint64(ip[6])<<8 | uint64(ip[7])
	lo := uint64(ip[8])<<56 | uint64(ip[9])<<48 | uint64(ip[10])<<40 | uint64(ip[11])<<32 |
		uint64(ip[12])<<24 | uint64(ip[13])<<16 | uint64(ip[14])<<8 | uint64(ip[15])
	return hi, lo
}