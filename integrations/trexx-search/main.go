package main

import (
	"context"
	"embed"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultAddr        = "127.0.0.1:8787"
	defaultProwlarr    = "http://localhost:9696"
	defaultBitmagnet   = "http://localhost:3333"
	defaultLimit       = 100
	maxResponseBytes   = 4 << 20
	maxQueryLength     = 200
	defaultHTTPTimeout = 15 * time.Second
	maxConfigBytes     = 64 << 10
)

//go:embed static/*
var staticFiles embed.FS

type config struct {
	Addr           string
	ProwlarrURL    string
	ProwlarrAPIKey string
	BitmagnetURL   string
	ResultLimit    int
	HTTPTimeout    time.Duration
}

type app struct {
	config config
	client *http.Client
}

type searchResult struct {
	Title       string     `json:"title"`
	Sources     []string   `json:"sources"`
	Size        int64      `json:"size"`
	Seeders     *int       `json:"seeders"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	InfoHash    string     `json:"infoHash,omitempty"`
	MagnetURI   string     `json:"magnetUri,omitempty"`
}

type searchResponse struct {
	Query    string         `json:"query"`
	Results  []searchResult `json:"results"`
	Warnings []string       `json:"warnings,omitempty"`
	Meta     searchMeta     `json:"meta"`
}

type searchMeta struct {
	Returned     int `json:"returned"`
	Deduplicated int `json:"deduplicated"`
}

type backendResponse struct {
	name    string
	results []searchResult
	err     error
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           newApp(cfg).routes(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("T-Rexx Search listening at http://%s", cfg.Addr)
	if cfg.ProwlarrAPIKey == "" {
		log.Print("Prowlarr searches are disabled until PROWLARR_API_KEY is set")
	}
	log.Fatal(server.ListenAndServe())
}

func loadConfig() (config, error) {
	prowlarrAPIKey := strings.TrimSpace(os.Getenv("PROWLARR_API_KEY"))
	if prowlarrAPIKey == "" {
		if path := strings.TrimSpace(os.Getenv("PROWLARR_CONFIG_FILE")); path != "" {
			var err error
			prowlarrAPIKey, err = readProwlarrAPIKey(path)
			if err != nil {
				return config{}, err
			}
		}
	}

	limit := defaultLimit
	if raw := strings.TrimSpace(os.Getenv("TREXX_RESULT_LIMIT")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			return config{}, fmt.Errorf("TREXX_RESULT_LIMIT must be a number from 1 to 200")
		}
		limit = parsed
	}

	timeout := defaultHTTPTimeout
	if raw := strings.TrimSpace(os.Getenv("TREXX_HTTP_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 || parsed > time.Minute {
			return config{}, fmt.Errorf("TREXX_HTTP_TIMEOUT must be a duration from 1ns to 1m")
		}
		timeout = parsed
	}

	return config{
		Addr:           envOrDefault("TREXX_ADDR", defaultAddr),
		ProwlarrURL:    envOrDefault("PROWLARR_URL", defaultProwlarr),
		ProwlarrAPIKey: prowlarrAPIKey,
		BitmagnetURL:   envOrDefault("BITMAGNET_URL", defaultBitmagnet),
		ResultLimit:    limit,
		HTTPTimeout:    timeout,
	}, nil
}

func readProwlarrAPIKey(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read Prowlarr config: %w", err)
	}
	defer file.Close()

	var document struct {
		APIKey string `xml:"ApiKey"`
	}
	if err := xml.NewDecoder(io.LimitReader(file, maxConfigBytes)).Decode(&document); err != nil {
		return "", fmt.Errorf("parse Prowlarr config: %w", err)
	}
	key := strings.TrimSpace(document.APIKey)
	if key == "" {
		return "", errors.New("Prowlarr config does not contain an API key")
	}
	return key, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func newApp(cfg config) *app {
	return &app{
		config: cfg,
		client: &http.Client{Timeout: cfg.HTTPTimeout},
	}
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.handleHealth)
	mux.HandleFunc("GET /api/search", a.handleSearch)

	assets, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServer(http.FS(assets)))
	return withSecurityHeaders(mux)
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func (a *app) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"backends": map[string]any{
			"prowlarr":  map[string]any{"url": a.config.ProwlarrURL, "configured": a.config.ProwlarrAPIKey != ""},
			"bitmagnet": map[string]any{"url": a.config.BitmagnetURL, "configured": true},
		},
	})
}

func (a *app) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	if len([]rune(query)) > maxQueryLength {
		writeError(w, http.StatusBadRequest, "q must be 200 characters or fewer")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), a.config.HTTPTimeout)
	defer cancel()

	responses := make(chan backendResponse, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		results, err := a.searchProwlarr(ctx, query)
		responses <- backendResponse{name: "Prowlarr", results: results, err: err}
	}()
	go func() {
		defer wg.Done()
		results, err := a.searchBitmagnet(ctx, query)
		responses <- backendResponse{name: "bitmagnet", results: results, err: err}
	}()
	go func() {
		wg.Wait()
		close(responses)
	}()

	var all []searchResult
	var warnings []string
	succeeded := 0
	for response := range responses {
		if response.err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", response.name, response.err))
			continue
		}
		succeeded++
		all = append(all, response.results...)
	}

	if succeeded == 0 {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error":    "Both search backends failed",
			"warnings": warnings,
		})
		return
	}

	merged := deduplicate(all)
	deduplicatedCount := len(all) - len(merged)
	if len(merged) > a.config.ResultLimit {
		merged = merged[:a.config.ResultLimit]
	}
	writeJSON(w, http.StatusOK, searchResponse{
		Query:    query,
		Results:  merged,
		Warnings: warnings,
		Meta: searchMeta{
			Returned:     len(merged),
			Deduplicated: deduplicatedCount,
		},
	})
}

type prowlarrResult struct {
	Title       string     `json:"title"`
	Guid        string     `json:"guid"`
	DownloadURL string     `json:"downloadUrl"`
	MagnetURL   string     `json:"magnetUrl"`
	MagnetURI   string     `json:"magnetUri"`
	InfoHash    string     `json:"infoHash"`
	Size        int64      `json:"size"`
	Seeders     *int       `json:"seeders"`
	PublishDate *time.Time `json:"publishDate"`
	Indexer     string     `json:"indexer"`
}

func (a *app) searchProwlarr(ctx context.Context, query string) ([]searchResult, error) {
	if a.config.ProwlarrAPIKey == "" {
		return nil, errors.New("disabled because PROWLARR_API_KEY is not set")
	}
	endpoint, err := appendPath(a.config.ProwlarrURL, "api/v1/search")
	if err != nil {
		return nil, err
	}
	params := endpoint.Query()
	params.Set("query", query)
	params.Set("type", "search")
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Api-Key", a.config.ProwlarrAPIKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, friendlyRequestError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("returned HTTP %d", resp.StatusCode)
	}

	var payload []prowlarrResult
	if err := decodeLimitedJSON(resp.Body, &payload); err != nil {
		return nil, fmt.Errorf("invalid response: %w", err)
	}
	if len(payload) > a.config.ResultLimit {
		payload = payload[:a.config.ResultLimit]
	}

	results := make([]searchResult, 0, len(payload))
	for _, item := range payload {
		magnet := firstMagnet(item.MagnetURI, item.MagnetURL, item.DownloadURL, item.Guid)
		hash := normalizeInfoHash(item.InfoHash)
		if hash == "" {
			hash = infoHashFromMagnet(magnet)
		}
		if magnet == "" && hash != "" {
			magnet = buildMagnet(hash, item.Title)
		}
		source := "Prowlarr"
		if strings.TrimSpace(item.Indexer) != "" {
			source += " · " + strings.TrimSpace(item.Indexer)
		}
		results = append(results, searchResult{
			Title:       strings.TrimSpace(item.Title),
			Sources:     []string{source},
			Size:        item.Size,
			Seeders:     item.Seeders,
			PublishedAt: item.PublishDate,
			InfoHash:    hash,
			MagnetURI:   magnet,
		})
	}
	return results, nil
}

const bitmagnetQuery = `query TrexxSearch($input: TorrentContentSearchQueryInput!) {
  torrentContent {
    search(input: $input) {
      items {
        title
        infoHash
        seeders
        publishedAt
        torrent {
          name
          size
          seeders
          magnetUri
        }
      }
    }
  }
}`

type bitmagnetPayload struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type bitmagnetResponse struct {
	Data struct {
		TorrentContent struct {
			Search struct {
				Items []struct {
					Title       string     `json:"title"`
					InfoHash    string     `json:"infoHash"`
					Seeders     *int       `json:"seeders"`
					PublishedAt *time.Time `json:"publishedAt"`
					Torrent     struct {
						Name      string `json:"name"`
						Size      int64  `json:"size"`
						Seeders   *int   `json:"seeders"`
						MagnetURI string `json:"magnetUri"`
					} `json:"torrent"`
				} `json:"items"`
			} `json:"search"`
		} `json:"torrentContent"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (a *app) searchBitmagnet(ctx context.Context, query string) ([]searchResult, error) {
	endpoint, err := appendGraphQLPath(a.config.BitmagnetURL)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(bitmagnetPayload{
		Query: bitmagnetQuery,
		Variables: map[string]any{
			"input": map[string]any{
				"queryString": query,
				"limit":       a.config.ResultLimit,
				"orderBy": []map[string]any{{
					"field":      "relevance",
					"descending": true,
				}},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, friendlyRequestError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("returned HTTP %d", resp.StatusCode)
	}

	var payload bitmagnetResponse
	if err := decodeLimitedJSON(resp.Body, &payload); err != nil {
		return nil, fmt.Errorf("invalid response: %w", err)
	}
	if len(payload.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL error: %s", payload.Errors[0].Message)
	}

	items := payload.Data.TorrentContent.Search.Items
	results := make([]searchResult, 0, len(items))
	for _, item := range items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = strings.TrimSpace(item.Torrent.Name)
		}
		seeders := item.Seeders
		if seeders == nil {
			seeders = item.Torrent.Seeders
		}
		hash := normalizeInfoHash(item.InfoHash)
		magnet := firstMagnet(item.Torrent.MagnetURI)
		if hash == "" {
			hash = infoHashFromMagnet(magnet)
		}
		if magnet == "" && hash != "" {
			magnet = buildMagnet(hash, title)
		}
		results = append(results, searchResult{
			Title:       title,
			Sources:     []string{"bitmagnet"},
			Size:        item.Torrent.Size,
			Seeders:     seeders,
			PublishedAt: item.PublishedAt,
			InfoHash:    hash,
			MagnetURI:   magnet,
		})
	}
	return results, nil
}

func appendPath(baseURL, suffix string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid backend URL %q", baseURL)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + strings.TrimLeft(suffix, "/")
	return parsed, nil
}

func appendGraphQLPath(baseURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid backend URL %q", baseURL)
	}
	if !strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/graphql") {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/graphql"
	}
	return parsed, nil
}

func decodeLimitedJSON(reader io.Reader, target any) error {
	return json.NewDecoder(io.LimitReader(reader, maxResponseBytes)).Decode(target)
}

func friendlyRequestError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("request timed out")
	}
	return fmt.Errorf("request failed: %w", err)
}

func firstMagnet(candidates ...string) string {
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if strings.HasPrefix(strings.ToLower(candidate), "magnet:?") {
			return candidate
		}
	}
	return ""
}

func normalizeInfoHash(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "magnet:?") {
		return infoHashFromMagnet(value)
	}
	value = strings.TrimPrefix(strings.ToLower(value), "urn:btih:")
	if len(value) == 40 {
		if _, err := hex.DecodeString(value); err == nil {
			return strings.ToLower(value)
		}
	}
	if len(value) == 32 {
		decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(value))
		if err == nil && len(decoded) == 20 {
			return hex.EncodeToString(decoded)
		}
	}
	return ""
}

func infoHashFromMagnet(magnet string) string {
	parsed, err := url.Parse(strings.TrimSpace(magnet))
	if err != nil || !strings.EqualFold(parsed.Scheme, "magnet") {
		return ""
	}
	for _, xt := range parsed.Query()["xt"] {
		if strings.HasPrefix(strings.ToLower(xt), "urn:btih:") {
			return normalizeInfoHash(xt)
		}
	}
	return ""
}

func buildMagnet(infoHash, title string) string {
	params := url.Values{}
	params.Set("xt", "urn:btih:"+infoHash)
	if strings.TrimSpace(title) != "" {
		params.Set("dn", strings.TrimSpace(title))
	}
	return "magnet:?" + params.Encode()
}

func deduplicate(results []searchResult) []searchResult {
	merged := make([]searchResult, 0, len(results))
	byHash := make(map[string]int)
	for _, result := range results {
		result.InfoHash = normalizeInfoHash(result.InfoHash)
		if result.InfoHash == "" {
			result.InfoHash = infoHashFromMagnet(result.MagnetURI)
		}
		if result.InfoHash == "" {
			merged = append(merged, result)
			continue
		}
		if index, ok := byHash[result.InfoHash]; ok {
			merged[index] = mergeResult(merged[index], result)
			continue
		}
		byHash[result.InfoHash] = len(merged)
		merged = append(merged, result)
	}

	sort.SliceStable(merged, func(i, j int) bool {
		left, right := merged[i], merged[j]
		if valueOrMinusOne(left.Seeders) != valueOrMinusOne(right.Seeders) {
			return valueOrMinusOne(left.Seeders) > valueOrMinusOne(right.Seeders)
		}
		if left.PublishedAt != nil && right.PublishedAt != nil && !left.PublishedAt.Equal(*right.PublishedAt) {
			return left.PublishedAt.After(*right.PublishedAt)
		}
		return strings.ToLower(left.Title) < strings.ToLower(right.Title)
	})
	return merged
}

func mergeResult(left, right searchResult) searchResult {
	left.Sources = uniqueStrings(append(left.Sources, right.Sources...))
	if left.Title == "" || (right.Title != "" && len(right.Title) < len(left.Title)) {
		left.Title = right.Title
	}
	if right.Size > left.Size {
		left.Size = right.Size
	}
	left.Seeders = maxIntPointer(left.Seeders, right.Seeders)
	if left.PublishedAt == nil || (right.PublishedAt != nil && right.PublishedAt.After(*left.PublishedAt)) {
		left.PublishedAt = right.PublishedAt
	}
	if left.MagnetURI == "" {
		left.MagnetURI = right.MagnetURI
	}
	return left
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value != "" && !seen[key] {
			seen[key] = true
			result = append(result, value)
		}
	}
	return result
}

func maxIntPointer(left, right *int) *int {
	if left == nil {
		return right
	}
	if right != nil && *right > *left {
		return right
	}
	return left
}

func valueOrMinusOne(value *int) int {
	if value == nil {
		return -1
	}
	return *value
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("writing JSON response: %v", err)
	}
}
