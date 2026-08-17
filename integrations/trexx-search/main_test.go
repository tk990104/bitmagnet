package main

import (
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testHash = "0123456789abcdef0123456789abcdef01234567"

func intPointer(value int) *int { return &value }

func TestNormalizeInfoHash(t *testing.T) {
	t.Parallel()
	hashBytes, err := hex.DecodeString(testHash)
	if err != nil {
		t.Fatal(err)
	}
	base32Hash := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(hashBytes)

	tests := map[string]string{
		"hex":    strings.ToUpper(testHash),
		"base32": base32Hash,
		"urn":    "urn:btih:" + testHash,
		"magnet": "magnet:?dn=Example&xt=urn%3Abtih%3A" + strings.ToUpper(testHash),
		"bad":    "not-a-hash",
	}
	for name, input := range tests {
		name, input := name, input
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want := testHash
			if name == "bad" {
				want = ""
			}
			if got := normalizeInfoHash(input); got != want {
				t.Fatalf("normalizeInfoHash(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestSearchHandlerReturnsPartialResults(t *testing.T) {
	t.Parallel()

	bitmagnet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"torrentContent":{"search":{"items":[{"title":"Public dataset","infoHash":"` + testHash + `","seeders":3,"publishedAt":"2026-08-02T12:00:00Z","torrent":{"name":"Public dataset","size":2048,"seeders":3,"magnetUri":"magnet:?xt=urn:btih:` + testHash + `"}}]}}}}`))
	}))
	defer bitmagnet.Close()

	cfg := config{
		ProwlarrURL:  "http://127.0.0.1:1",
		BitmagnetURL: bitmagnet.URL,
		ResultLimit:  100,
		HTTPTimeout:  5 * time.Second,
	}
	request := httptest.NewRequest(http.MethodGet, "/api/search?q=dataset", nil)
	recorder := httptest.NewRecorder()
	newApp(cfg).routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response searchResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || len(response.Warnings) != 1 || !strings.Contains(response.Warnings[0], "PROWLARR_API_KEY") {
		t.Fatalf("response = %#v", response)
	}
}

func TestDeduplicateMergesByInfoHash(t *testing.T) {
	t.Parallel()
	published := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	later := published.Add(24 * time.Hour)

	got := deduplicate([]searchResult{
		{Title: "Example Linux ISO", Sources: []string{"Prowlarr · Example"}, Size: 10, Seeders: intPointer(5), PublishedAt: &published, InfoHash: strings.ToUpper(testHash)},
		{Title: "Example Linux ISO", Sources: []string{"bitmagnet"}, Size: 20, Seeders: intPointer(12), PublishedAt: &later, InfoHash: testHash, MagnetURI: buildMagnet(testHash, "Example Linux ISO")},
	})

	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	if len(got[0].Sources) != 2 || got[0].Size != 20 || got[0].Seeders == nil || *got[0].Seeders != 12 {
		t.Fatalf("merged result = %#v", got[0])
	}
	if got[0].MagnetURI == "" {
		t.Fatal("merged result has no magnet URI")
	}
}

func TestSearchHandlerMergesBackends(t *testing.T) {
	t.Parallel()

	prowlarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" || r.URL.Query().Get("query") != "ubuntu" {
			t.Fatalf("unexpected Prowlarr request: %s", r.URL.String())
		}
		if r.Header.Get("X-Api-Key") != "test-key" {
			t.Fatal("Prowlarr API key was not sent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"title":"Ubuntu ISO","guid":"magnet:?xt=urn:btih:` + strings.ToUpper(testHash) + `","infoHash":"` + strings.ToUpper(testHash) + `","size":1024,"seeders":4,"publishDate":"2026-08-01T12:00:00Z","indexer":"Legal Torrents"}]`))
	}))
	defer prowlarr.Close()

	bitmagnet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" || r.Method != http.MethodPost {
			t.Fatalf("unexpected bitmagnet request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"torrentContent":{"search":{"items":[{"title":"Ubuntu ISO","infoHash":"` + testHash + `","seeders":10,"publishedAt":"2026-08-02T12:00:00Z","torrent":{"name":"Ubuntu ISO","size":2048,"seeders":10,"magnetUri":"magnet:?xt=urn:btih:` + testHash + `"}}]}}}}`))
	}))
	defer bitmagnet.Close()

	cfg := config{
		Addr:           defaultAddr,
		ProwlarrURL:    prowlarr.URL,
		ProwlarrAPIKey: "test-key",
		BitmagnetURL:   bitmagnet.URL,
		ResultLimit:    100,
		HTTPTimeout:    5 * time.Second,
	}
	request := httptest.NewRequest(http.MethodGet, "/api/search?q=ubuntu", nil)
	recorder := httptest.NewRecorder()
	newApp(cfg).routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response searchResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Meta.Deduplicated != 1 {
		t.Fatalf("response = %#v", response)
	}
	result := response.Results[0]
	if len(result.Sources) != 2 || result.Seeders == nil || *result.Seeders != 10 || result.MagnetURI == "" {
		t.Fatalf("result = %#v", result)
	}
}

func TestSearchHandlerRejectsEmptyQuery(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodGet, "/api/search?q=", nil)
	recorder := httptest.NewRecorder()
	newApp(config{HTTPTimeout: time.Second}).routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}
