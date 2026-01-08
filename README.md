# IPInfo In-Memory API

IPInfo In-Memory API is a high-performance **HTTP service written in Go** for IPv4 and IPv6 metadata lookup using the IPInfo offline CSV dataset.

The service is designed for **read-heavy workloads**, low latency, and predictable behavior. All lookups are performed entirely in memory — no external API calls are made during request handling.

---

## 🚀 Key Features

* **IPv4 and IPv6 support**
* **In-memory lookups only** ($O(\log N)$)
* **Offline IPInfo CSV dataset** (`.csv.gz`)
* **Automatic gzip handling**
* **Atomic dataset reload** (no downtime)
* **Disk cache** of the last successful dataset
* **Periodic background refresh**
* **Manual reload** via HTTP or `SIGHUP`
* **Separate liveness and readiness endpoints**
* **Graceful shutdown**
* **Single static binary** (CGO disabled)

---

## 🛠 How It Works

1. The IPInfo dataset is downloaded as a compressed CSV file.
2. The file is decompressed and parsed.
3. Parsed data is stored in optimized in-memory structures.
4. Lookups are performed using **binary search**.
5. Dataset reloads replace the in-memory data atomically.

> **Note:** During normal operation, **no network calls** are made.

---

## 📊 Dataset Format

The service expects an IPInfo CSV dataset with the following columns:

* `network` (CIDR, IPv4 or IPv6)
* `country`
* `country_code`
* `continent`
* `continent_code`
* `asn` (required, may be empty)
* `as_name` (required, may be empty)
* `as_domain` (required, may be empty)

---

## 📡 API Endpoints

### `GET /ip/{ip}`

Returns metadata for an IPv4 or IPv6 address.

**Example response:**

```json
{
  "network": "8.8.8.0/24",
  "country": "United States",
  "country_code": "US",
  "continent": "North America",
  "continent_code": "NA",
  "asn": "AS15169",
  "as_name": "Google LLC",
  "as_domain": "google.com"
}
```

Returns 404 if the IP is not found.

### `GET /healthz`

Liveness probe.

Always returns 200 OK.

Performs no dataset access.

Intended for container orchestration and monitoring.

### `GET /readyz`

Readiness probe.

Returns 200 OK only when the dataset is loaded.

Returns 503 during startup or failed reload.

### `POST /reload`

Triggers a dataset reload.

Reload runs asynchronously.

Uses the same logic as periodic refresh.

Safe to call while the service is running.

## ⚙️ Configuration
Configuration is provided via `config.yaml`.

```yaml
server:
  listen: ":8090"
  shutdown_timeout: 10s

ipinfo:
  dump_url: "https://ipinfo.io/data/ipinfo_lite.csv.gz"
  token: ""
  refresh_interval: 24h
  download_timeout: 30s
  cache_file: "./data/ipinfo.csv"
```

### Environment Overrides

The IPInfo token can be provided via environment variable:

```bash
HL_APP_IPINFO_TOKEN=your_token_here
```

### 💻 Running Locally

Requirements: Go 1.25.5 or newer

```bash
export HL_APP_IPINFO_TOKEN=your_token
go run main.go
```
The first startup may take longer due to dataset download and parsing.

## 📦 Binary Releases
Prebuilt binaries are available via GitHub Releases for:

- Linux (amd64, arm64)
- macOS (arm64)

Each release includes SHA256 checksums.

## 🧠 Design Principles

- No mutexes in the request path
- No allocations during lookups
- Immutable datasets
- Atomic memory swaps
- Disk and memory consistency
- Simple failure modes

This service favors determinism and performance over configurability.
