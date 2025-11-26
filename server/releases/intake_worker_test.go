package releases

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"printmaster/server/storage"
)

func TestIntakeWorkerCachesArtifacts(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	var downloadURL string
	downloadHits := 0

	mux.HandleFunc("/repos/test-owner/printmaster/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		payload := `[
            {
                "tag_name": "server-v0.9.16",
                "draft": false,
                "prerelease": false,
                "body": "notes",
                "published_at": "2025-11-20T12:00:00Z",
                "assets": [
                    {
                        "name": "printmaster-server-v0.9.16-windows-amd64.exe",
                        "browser_download_url": "` + downloadURL + `",
                        "size": 12,
                        "updated_at": "2025-11-20T12:00:00Z"
                    }
                ]
            }
        ]`
		_, _ = w.Write([]byte(payload))
	})

	mux.HandleFunc("/downloads/server", func(w http.ResponseWriter, r *http.Request) {
		downloadHits++
		_, _ = w.Write([]byte("printmaster"))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	downloadURL = server.URL + "/downloads/server"

	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	worker, err := NewIntakeWorker(store, nil, Options{
		CacheDir:     t.TempDir(),
		RepoOwner:    "test-owner",
		RepoName:     "printmaster",
		BaseAPIURL:   server.URL,
		HTTPClient:   server.Client(),
		PollInterval: time.Hour,
		UserAgent:    "test",
	})
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once failed: %v", err)
	}

	art, err := store.GetReleaseArtifact(context.Background(), "server", "0.9.16", "windows", "amd64")
	if err != nil {
		t.Fatalf("artifact not persisted: %v", err)
	}
	if art.CachePath == "" {
		t.Fatalf("expected cache path to be set")
	}
	if _, err := os.Stat(art.CachePath); err != nil {
		t.Fatalf("cached file missing: %v", err)
	}

	firstHits := downloadHits
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	if downloadHits != firstHits {
		t.Fatalf("expected cached artifact to skip download")
	}

}
