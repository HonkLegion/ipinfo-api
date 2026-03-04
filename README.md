# IPInfo MMDB API

IPInfo MMDB API is a high-performance **HTTP service written in Go** for IPv4 and IPv6 metadata lookup using the IPInfo offline MMDB dataset.

The service is designed for **read-heavy workloads**, predictable behavior, and a lower memory footprint than the previous CSV materialization approach. All lookups are local to the on-disk MMDB file — no external API calls are made during request handling.

---

## 🚀 Key Features

* **IPv4 and IPv6 support**
* **Offline IPInfo MMDB dataset** (`.mmdb`)
* **Memory-conservative lookups** via memory-mapped MMDB reader
* **Automatic gzip handling** for compressed downloads
* **Atomic dataset reload** (no downtime)
* **Disk cache** of the last successful dataset
* **Periodic background refresh**
* **Manual reload** via HTTP or `SIGHUP`
* **Separate liveness and readiness endpoints**
* **Graceful shutdown**
* **Single static binary** (CGO disabled)

---

## 🛠 How It Works

1. The IPInfo dataset is downloaded as an MMDB file.
2. The downloaded file is validated by opening it as a MaxMind DB.
3. The validated file is atomically swapped into the local disk cache.
4. Lookups are performed directly against the MMDB reader.
5. Dataset reloads atomically replace the active MMDB reader.

> **Note:** During normal operation, **no network calls** are made.

---

## 📊 Dataset Format

The service expects an IPInfo MMDB dataset exposing the following fields:

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
  dump_url: "https://ipinfo.io/data/ipinfo_lite.mmdb"
  token: ""
  refresh_interval: 24h
  download_timeout: 30s
  cache_file: "./data/ipinfo.mmdb"
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
go run app.go
```
The first startup may take longer due to dataset download and MMDB validation.

## 📦 Binary Releases
Prebuilt binaries are available via GitHub Releases for:

- Linux (amd64, arm64)
- macOS (arm64)

Each release includes SHA256 checksums.

## 🐳 Docker Releases

Tagged releases also publish a multi-platform Docker image to Docker Hub for:

- Linux (amd64, arm64)

Docker images cannot target `darwin/arm64`; macOS support is provided via the standalone binary release above.

Stable release tags publish these Docker tags:

- `x.y.z`
- `x.y`
- `x`
- `latest`
- `sha-<commit>`

Pre-release or test tags such as `v1.2.3-rc1` or `v1.2.3-test` publish the exact version tag plus `sha-<commit>` and do not update `latest`.

### GitHub Actions configuration

Set these repository secrets before creating a release tag:

- `DOCKERHUB_USERNAME`
- `DOCKERHUB_TOKEN`

Optional repository variable:

- `DOCKERHUB_REPOSITORY` to override the default image name `${DOCKERHUB_USERNAME}/ipinfo-api`

The release workflow uploads binaries and `checksums.txt` to an existing GitHub Release for the pushed tag. It does not create the release entry automatically.

## 🧠 Design Principles

- No mutexes in the request path
- Immutable on-disk datasets
- Atomic reader swaps
- Disk and memory consistency
- Simple failure modes

This service favors determinism and performance over configurability.
