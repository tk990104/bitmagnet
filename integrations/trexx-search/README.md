# T-Rexx unified torrent search

T-Rexx Search is a small companion service that searches Prowlarr and bitmagnet in parallel, normalizes their results, and merges duplicates when the same BitTorrent v1 info hash appears in both sources. Its browser UI shows title, source, size, seeders, and age, with sortable columns and an explicit **Open in µTorrent** magnet action.

The service is isolated from bitmagnet's core code. It uses only the Go standard library and embeds its static interface in one executable.

> Use T-Rexx Search only for torrents you are legally authorized to download or share.

## Prerequisites

- bitmagnet running at `http://localhost:3333` (or another configured URL)
- Prowlarr running at `http://localhost:9696`
- a Prowlarr API key from **Settings > General > Security**
- µTorrent Classic registered as the Windows handler for `MAGNET` links
- either Go 1.23+ or Docker Desktop

## Run with Go

From the repository root in PowerShell:

```powershell
$env:PROWLARR_API_KEY = "paste-your-key-here"
go run ./integrations/trexx-search
```

Open `http://127.0.0.1:8787`. The API key stays in the local service and is never sent to the browser.

## Run with Docker Compose

From the repository root:

```powershell
Copy-Item integrations/trexx-search/.env.example integrations/trexx-search/.env
# Edit integrations/trexx-search/.env and replace the placeholder API key.
docker compose -f integrations/trexx-search/compose.yml up --build -d
```

Open `http://localhost:8787`. The supplied Compose settings use `host.docker.internal` so a container can reach Prowlarr and bitmagnet running on the Windows host. If all services share a Docker network, set `PROWLARR_URL` and `BITMAGNET_URL` to their Compose service names instead.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `TREXX_ADDR` | `127.0.0.1:8787` | Local listen address. The container overrides this to `0.0.0.0:8787`. |
| `PROWLARR_URL` | `http://localhost:9696` | Prowlarr base URL. Subpaths are supported. |
| `PROWLARR_API_KEY` | none | Prowlarr API key. Prowlarr is skipped with a visible warning when unset. |
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
