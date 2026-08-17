# T-Rexx unified torrent search

T-Rexx Search is a small companion service that searches Prowlarr and bitmagnet in parallel, normalizes their results, and merges duplicates when the same BitTorrent v1 info hash appears in both sources. Its browser UI shows title, source, size, seeders, and age, with sortable columns and an explicit **Open in µTorrent** magnet action.

The service is isolated from bitmagnet's core code. It uses only the Go standard library and embeds its static interface in one executable.

> Use T-Rexx Search only for torrents you are legally authorized to download or share.

## Prerequisites

- Docker Desktop with the WSL 2 engine
- µTorrent Classic registered as the Windows handler for `MAGNET` links
- Go 1.23+ only when using the native Go command instead of Docker

## Run with Go

From the repository root in PowerShell:

```powershell
$env:PROWLARR_API_KEY = "paste-your-key-here"
go run ./integrations/trexx-search
```

Open `http://127.0.0.1:8787`. The API key stays in the local service and is never sent to the browser.

## Run the complete local stack with Docker Compose

From the repository root:

```powershell
docker compose -f integrations/trexx-search/compose.yml up --build -d
```

The stack starts four local services and keeps their web interfaces bound to this computer:

| Service | Address |
|---|---|
| T-Rexx Search | `http://127.0.0.1:8787` |
| Prowlarr | `http://127.0.0.1:9696` |
| bitmagnet | `http://127.0.0.1:3333` |
| PostgreSQL | internal Docker network only |

Persistent files live under the repository's ignored `data/trexx` directory. T-Rexx mounts Prowlarr's configuration read-only and discovers its generated API key automatically, so the default Docker setup does not require copying the key into `.env`. An explicit `PROWLARR_API_KEY` still takes precedence when supplied.

The first bitmagnet search may be empty while its new DHT index begins collecting metadata. Prowlarr searches remain empty until at least one indexer is configured in its web interface.

Stop the stack without deleting its data:

```powershell
docker compose -f integrations/trexx-search/compose.yml down
```

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `TREXX_ADDR` | `127.0.0.1:8787` | Local listen address. The container overrides this to `0.0.0.0:8787`. |
| `PROWLARR_URL` | `http://localhost:9696` | Prowlarr base URL. Subpaths are supported. |
| `PROWLARR_API_KEY` | none | Prowlarr API key. Prowlarr is skipped with a visible warning when unset. |
| `PROWLARR_CONFIG_FILE` | none | Optional Prowlarr `config.xml`; its API key is read when `PROWLARR_API_KEY` is unset. |
| `BITMAGNET_URL` | `http://localhost:3333` | bitmagnet base URL or full `/graphql` URL. |
| `TREXX_RESULT_LIMIT` | `100` | Maximum results requested per source and returned after merging (`1`–`200`). |
| `TREXX_HTTP_TIMEOUT` | `15s` | Per-search timeout, up to one minute. |

## API

```text
GET /api/health
GET /api/search?q=<query>
```

Searches run concurrently. If one backend is unavailable, results from the other are returned with a warning. If both fail, the API returns HTTP 502. Results with a recognized 40-character hexadecimal or 32-character base32 v1 info hash are deduplicated; source labels and the highest observed seeder count are retained.

Prowlarr results that do not expose either a magnet URI or an info hash remain visible, but their µTorrent action is disabled because a safe magnet URI cannot be constructed.

## Validate

```powershell
go test ./integrations/trexx-search
go vet ./integrations/trexx-search
```

For a live smoke test, search for a legal/public torrent such as a Linux distribution and confirm that **Open in µTorrent** launches the registered Windows magnet handler.
