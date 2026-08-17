# T-Rexx Torrent Search + µTorrent Integration

This document describes a clean architecture for using **bitmagnet** and **Prowlarr** for torrent discovery while keeping **µTorrent Classic on Windows** as the download client.

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
                           µTorrent Classic
```

## Why this design

Prowlarr is useful as a centralized indexer manager and manual search interface. bitmagnet provides a self-hosted torrent metadata index, web UI, and API surface. µTorrent remains the local Windows client.

The current Prowlarr source tree in this fork does not include a native `UTorrent` download-client implementation, so this design avoids maintaining a custom Prowlarr client plugin. Instead, Windows handles magnet links using the application registered for the `magnet:` protocol.

## Phase 1 — Verify µTorrent magnet handling

1. Open **µTorrent Classic** on Windows.
2. Open Windows **Settings → Apps → Default apps**.
3. Search for the `MAGNET` protocol.
4. Confirm that µTorrent is the default handler.
5. Test with a legal/public torrent magnet link.

If Windows opens µTorrent when you click the magnet link, no helper application is required.

## Phase 2 — Run bitmagnet

The repository includes a full Docker Compose example. For an initial test, use the project's documented minimal Compose configuration rather than enabling Grafana/Prometheus immediately.

Typical local endpoints:

- bitmagnet web UI: `http://localhost:3333`
- bitmagnet API: exposed through the bitmagnet HTTP service

The included full `docker-compose.yml` also supports routing bitmagnet through a Gluetun VPN container and exposes ports `3333` and `3334`.

## Phase 3 — Run Prowlarr

Install or run Prowlarr separately and use it for centralized indexer management and manual searching.

Recommended local layout:

```text
Prowlarr:   http://localhost:9696
bitmagnet:  http://localhost:3333
µTorrent:   Windows desktop application
```

## Phase 4 — Search-to-µTorrent workflow

The simplest workflow is:

1. Search Prowlarr or bitmagnet.
2. Select a result.
3. Open/copy its magnet URI.
4. Let Windows hand the `magnet:` URI to µTorrent.
5. Confirm the torrent in µTorrent before starting the transfer.

This keeps discovery and downloading separate and avoids storing µTorrent credentials inside the search services.

## Phase 5 — T-Rexx Command integration

The first T-Rexx Command search module now lives in [`integrations/trexx-search`](../integrations/trexx-search/README.md). It is a lightweight Go companion service with an embedded browser interface. It searches Prowlarr and bitmagnet concurrently, normalizes results, merges matching v1 info hashes, and keeps the final µTorrent handoff as an explicit click.

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
[Open in µTorrent]
[Open Source]
```

The **Open in µTorrent** button should simply navigate to the magnet URI. On Windows, the registered magnet handler will launch µTorrent.

## Implemented API layer

The local service exposes:

```text
GET /api/search?q=<query>
GET /api/health
```

The service merges and normalizes results, removes duplicates using the torrent info hash, and supports sorting by title, source, seeders, age, or size in the UI. If one source is temporarily unavailable, the other source can still return partial results with a visible warning.

Do **not** automatically start downloads by default. Require the user to click **Open in µTorrent** so the final action stays explicit.

## Recommended build order

1. Confirm Windows/µTorrent magnet handling.
2. Bring up bitmagnet locally.
3. Bring up Prowlarr locally.
4. Verify searches independently.
5. ~~Add a lightweight local aggregator API.~~ Complete in `integrations/trexx-search`.
6. ~~Add the T-Rexx Command search UI.~~ Complete with the embedded responsive UI.
7. ~~Add deduplication and ranking.~~ Complete for BitTorrent v1 info hashes, with seeder-first ranking.
8. Add the configured Prowlarr API key and run the live backend smoke test.

## NotebookLM note structure

For project documentation, create a NotebookLM notebook with these source groups:

- bitmagnet installation/configuration
- Prowlarr indexer configuration
- µTorrent/Windows magnet protocol notes
- Docker/networking notes
- T-Rexx Command API/UI design
- troubleshooting log

That notebook can become the permanent research and operations manual for the project.
