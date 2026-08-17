# T-Rexx Torrent Search + qBittorrent Integration

This document describes a clean architecture for using **bitmagnet** and **Prowlarr** for torrent discovery while keeping **qBittorrent on Windows** as the download client.

> Use this setup only for torrents you are legally authorized to download or share, such as Linux distributions, public-domain media, open datasets, and your own files.

## Architecture

```text
                    ┌────────────────────────────┐
                    │        T-Rexx Search       │
                    │  local companion service  │
                    └──────────────┬─────────────┘
                                   │
                    ┌──────────────┴─────────────┐
                    │                            │
              ┌─────▼─────┐                ┌─────▼─────┐
              │ Prowlarr  │                │ bitmagnet │
              │ indexers  │                │ DHT index │
              └─────┬─────┘                └─────┬─────┘
                    │                            │
                    └──────────────┬─────────────┘
                                   │
                              magnet URI
                                   │
                                   ▼
                           Windows magnet handler
                                   │
                                   ▼
                             qBittorrent
```

## Why this design

Prowlarr is useful as a centralized indexer manager and manual search interface. bitmagnet provides a self-hosted torrent metadata index, web UI, and API surface. qBittorrent remains the local Windows client.

The handoff stays client-neutral: Windows opens magnet links with the application registered for the `magnet:` protocol. qBittorrent is the recommended client for this setup, but the T-Rexx service does not store client credentials or call a client-specific API.

## Phase 1 — Verify qBittorrent magnet handling

1. Open **qBittorrent** on Windows.
2. Open Windows **Settings → Apps → Default apps**.
3. Search for the `MAGNET` protocol.
4. Confirm that qBittorrent is the default handler.
5. Test with a legal/public torrent magnet link.

If Windows opens qBittorrent when you click the magnet link, no helper application is required.

## Phase 2 — Run the complete local stack

The integration includes a focused Docker Compose stack with bitmagnet, PostgreSQL, Prowlarr, and T-Rexx Search. From the repository root:

```powershell
docker compose -f integrations/trexx-search/compose.yml up --build -d
```

Local endpoints:

- bitmagnet web UI: `http://localhost:3333`
- Prowlarr web UI: `http://localhost:9696`
- T-Rexx Search: `http://localhost:8787`

The web interfaces listen only on localhost. bitmagnet's DHT port uses `3334` TCP/UDP. Persistent application data is stored under the ignored `data/trexx` directory.

T-Rexx reads Prowlarr's generated API key from its read-only local configuration. Manual API-key copying is only needed when Prowlarr runs outside this Compose stack.

The repository's full root `docker-compose.yml` demonstrates routing bitmagnet through Gluetun. bitmagnet recommends VPN routing for longer-running DHT crawling; adapt that example before extended use if this is part of your network plan.

## Phase 3 — Configure Prowlarr

Open Prowlarr and add only indexers you are authorized to use. Until an indexer is configured, Prowlarr correctly returns an empty search result.

Recommended local layout:

```text
Prowlarr:   http://localhost:9696
bitmagnet:  http://localhost:3333
qBittorrent: Windows desktop application
```

## Phase 4 — Search-to-client workflow

The simplest workflow is:

1. Search Prowlarr or bitmagnet.
2. Select a result.
3. Open/copy its magnet URI.
4. Let Windows hand the `magnet:` URI to qBittorrent.
5. Confirm the torrent in qBittorrent before starting the transfer.

This keeps discovery and downloading separate and avoids storing download-client credentials inside the search services.

## Phase 5 — T-Rexx Command integration

The first T-Rexx Command search module now lives in [`integrations/trexx-search`](../integrations/trexx-search/README.md). It is a lightweight Go companion service with an embedded browser interface. It searches Prowlarr and bitmagnet concurrently, normalizes results, merges matching v1 info hashes, and keeps the final torrent-client handoff as an explicit click.

Suggested fields:

```text
Title
Source
Size
Seeders
Leechers
Age / discovered date
Info hash
Magnet URI
Category
```

Suggested UI actions:

```text
[Search]
[Copy Magnet]
[Open in torrent client]
[Open Source]
```

The **Open in torrent client** button simply navigates to the magnet URI. On Windows, the registered magnet handler launches qBittorrent.

## Implemented API layer

The local service exposes:

```text
GET /api/search?q=<query>
GET /api/health
```

The service merges and normalizes results, removes duplicates using the torrent info hash, and supports sorting by title, source, seeders, age, or size in the UI. If one source is temporarily unavailable, the other source can still return partial results with a visible warning.

Do **not** automatically start downloads by default. Require the user to click **Open in torrent client** so the final action stays explicit.

## Recommended build order

1. Confirm Windows/qBittorrent magnet handling.
2. Bring up bitmagnet locally.
3. Bring up Prowlarr locally.
4. Verify searches independently.
5. ~~Add a lightweight local aggregator API.~~ Complete in `integrations/trexx-search`.
6. ~~Add the T-Rexx Command search UI.~~ Complete with the embedded responsive UI.
7. ~~Add deduplication and ranking.~~ Complete for BitTorrent v1 info hashes, with seeder-first ranking.
8. ~~Connect the generated Prowlarr API key and run the live backend smoke test.~~ Complete through read-only config discovery; a live `ubuntu` query reached both backends successfully.

## NotebookLM note structure

For project documentation, create a NotebookLM notebook with these source groups:

- bitmagnet installation/configuration
- Prowlarr indexer configuration
- qBittorrent/Windows magnet protocol notes
- Docker/networking notes
- T-Rexx Command API/UI design
- troubleshooting log

That notebook can become the permanent research and operations manual for the project.

