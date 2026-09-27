package actionlint

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRemoteActionsCacheFindsFormatsAtExactRefs(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.EscapedPath())
		mu.Unlock()
		if r.Method != http.MethodGet {
			t.Errorf("method was %s, wanted GET", r.Method)
		}
		switch r.URL.EscapedPath() {
		case "/owner/repo/v1/action.yaml":
			_, _ = w.Write([]byte("name: version one\nruns:\n  using: node20\n  main: index.js\n"))
		case "/owner/repo/v1/action.yml":
			_, _ = w.Write([]byte("name: version one fallback\nruns:\n  using: node20\n  main: index.js\n"))
		case "/owner/repo/v2/action.yaml":
			http.NotFound(w, r)
		case "/owner/repo/v2/action.yml":
			_, _ = w.Write([]byte("name: version two\nruns:\n  using: node20\n  main: index.js\n"))
		case "/owner/repo/refs%2Ftags%2Fv3/nested-dir/action.yaml":
			_, _ = w.Write([]byte("name: nested action\nruns:\n  using: node20\n  main: index.js\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cache := newRemoteActionsCache(server.Client(), 2, t.TempDir(), "", server.URL)
	for _, test := range []struct {
		spec string
		name string
	}{
		{"owner/repo@v1", "version one"},
		{"owner/repo@v2", "version two"},
		{"owner/repo/nested-dir@refs/tags/v3", "nested action"},
	} {
		metadata, err := cache.FindMetadata(test.spec)
		if err != nil {
			t.Fatalf("FindMetadata(%q) failed: %v", test.spec, err)
		}
		if metadata.Name != test.name {
			t.Errorf("metadata for %q had name %q, wanted %q", test.spec, metadata.Name, test.name)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 4 {
		t.Fatalf("made %d requests, wanted 4: %v", len(paths), paths)
	}
}
func TestRemoteActionsCacheUsesPersistedYMLWithoutRetryingMissingYAML(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if strings.HasSuffix(r.URL.Path, "/action.yaml") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("name: persisted yml\nruns:\n  using: node24\n  main: index.js\n"))
	}))
	defer server.Close()

	dir := t.TempDir()
	cache := newRemoteActionsCache(server.Client(), 1, dir, "", server.URL)
	if _, err := cache.FindMetadata("owner/repo@v1"); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("expected both metadata filenames on initial fetch, got %d requests", got)
	}
	cache = newRemoteActionsCache(server.Client(), 1, dir, "", server.URL)
	if _, err := cache.FindMetadata("owner/repo@v1"); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("persistent yml cache should avoid the missing yaml request, got %d requests", got)
	}
}

func TestRemoteActionsCacheDeduplicatesConcurrentFetches(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("name: cached\nruns:\n  using: node20\n  main: index.js\n"))
	}))
	defer server.Close()

	cache := newRemoteActionsCache(server.Client(), 8, t.TempDir(), "", server.URL)
	start := make(chan struct{})
	errs := make(chan error, 100)
	var group sync.WaitGroup
	for range 100 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			metadata, err := cache.FindMetadata("owner/repo@v1")
			if err == nil && metadata.Name != "cached" {
				errs <- &remoteActionsTestError{message: "received unexpected metadata"}
				return
			}
			errs <- err
		}()
	}
	close(start)
	group.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("made %d requests for one specification, wanted 1", got)
	}
}

func TestRemoteActionsCacheDeduplicatesConcurrentErrors(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	defer server.Close()

	cache := newRemoteActionsCache(server.Client(), 8, t.TempDir(), "", server.URL)
	start := make(chan struct{})
	errs := make(chan error, 100)
	var group sync.WaitGroup
	for range 100 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := cache.FindMetadata("owner/repo@v1")
			errs <- err
		}()
	}
	close(start)
	group.Wait()
	close(errs)

	var message string
	for err := range errs {
		if err == nil {
			t.Error("FindMetadata unexpectedly succeeded")
			continue
		}
		if message == "" {
			message = err.Error()
		} else if err.Error() != message {
			t.Errorf("error %q differed from cached error %q", err, message)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("made %d requests for one failing specification, wanted 1", got)
	}
}

type remoteActionsTestError struct {
	message string
}

func (e *remoteActionsTestError) Error() string {
	return e.message
}

func TestRemoteActionsCacheBoundsConcurrentUniqueFetches(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		active.Add(-1)
		_, _ = w.Write([]byte("name: action\nruns:\n  using: node20\n  main: index.js\n"))
	}))
	defer server.Close()

	cache := newRemoteActionsCache(server.Client(), 2, t.TempDir(), "", server.URL)
	errs := make(chan error, 4)
	var group sync.WaitGroup
	for _, spec := range []string{"owner/repo-one@v1", "owner/repo-two@v1", "owner/repo-three@v1", "owner/repo-four@v1"} {
		group.Add(1)
		go func(spec string) {
			defer group.Done()
			_, err := cache.FindMetadata(spec)
			errs <- err
		}(spec)
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("did not start the two permitted concurrent fetches")
		}
	}
	close(release)
	group.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if got := maximum.Load(); got != 2 {
		t.Errorf("maximum concurrent requests was %d, wanted 2", got)
	}
}

func TestRemoteActionsCacheCachesErrorsAndDoesNotFallback(t *testing.T) {
	tests := []struct {
		name string
		body string
		code int
	}{
		{name: "server error", code: http.StatusInternalServerError},
		{name: "invalid YAML", code: http.StatusOK, body: "name: ["},
		{name: "oversized body", code: http.StatusOK, body: strings.Repeat("x", remoteActionsMaxBodySize+1)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var yamlRequests atomic.Int32
			var ymlRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/action.yaml") {
					yamlRequests.Add(1)
				} else if strings.HasSuffix(r.URL.Path, "/action.yml") {
					ymlRequests.Add(1)
				}
				w.WriteHeader(test.code)
				if test.body != "" {
					_, _ = w.Write([]byte(test.body))
				}
			}))
			defer server.Close()

			cache := newRemoteActionsCache(server.Client(), 1, t.TempDir(), "", server.URL)
			_, first := cache.FindMetadata("owner/repo@v1")
			_, second := cache.FindMetadata("owner/repo@v1")
			if first == nil || second == nil {
				t.Fatalf("errors were first=%v second=%v, wanted two errors", first, second)
			}
			if first.Error() != second.Error() {
				t.Errorf("cached error %q differed from original %q", second, first)
			}
			if got := yamlRequests.Load(); got != 1 {
				t.Errorf("action.yaml requested %d times, wanted 1", got)
			}
			if got := ymlRequests.Load(); got != 0 {
				t.Errorf("action.yml requested %d times after %s, wanted 0", got, test.name)
			}
		})
	}
}

func TestRemoteActionsCacheReportsAbsentMetadata(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	}))
	defer server.Close()

	metadata, err := newRemoteActionsCache(server.Client(), 1, t.TempDir(), "", server.URL).FindMetadata("owner/repo@v1")
	if err == nil {
		t.Fatal("missing metadata did not return an error")
	}
	if metadata != nil {
		t.Errorf("missing metadata returned %#v, wanted nil", metadata)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("made %d requests for missing metadata, wanted 2", got)
	}
}

func TestRemoteActionsCacheDoesNotPersistErrors(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporary failure", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("name: recovered\nruns:\n  using: node20\n  main: index.js\n"))
	}))
	defer server.Close()

	dir := t.TempDir()
	cache := newRemoteActionsCache(server.Client(), 1, dir, "", server.URL)
	if _, err := cache.FindMetadata("owner/repo@v1"); err == nil {
		t.Fatal("initial request unexpectedly succeeded")
	}
	cache = newRemoteActionsCache(server.Client(), 1, dir, "", server.URL)
	metadata, err := cache.FindMetadata("owner/repo@v1")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Name != "recovered" {
		t.Errorf("metadata name was %q, wanted recovered", metadata.Name)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("made %d requests after an error, wanted 2", got)
	}
}

func TestRemoteActionsCachePersistsFetchedMetadata(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("name: persistent\nruns:\n  using: node20\n  main: index.js\n"))
	}))
	defer server.Close()

	dir := filepath.Join(t.TempDir(), "action-metadata")
	spec := "owner/repo@v1"
	cache := newRemoteActionsCache(server.Client(), 1, dir, "", server.URL)
	if _, err := cache.FindMetadata(spec); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	} else if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("cache directory mode was %o, wanted 700", got)
	}
	cachePath := cache.metadataCachePath(spec, "action.yaml")
	if info, err := os.Stat(cachePath); err != nil {
		t.Fatal(err)
	} else if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("cache file mode was %o, wanted 600", got)
	}

	cache = newRemoteActionsCache(server.Client(), 1, dir, "", server.URL)
	if _, err := cache.FindMetadata(spec); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("made %d requests with a fresh cache instance, wanted 1", got)
	}

	old := time.Now().Add(-remoteActionsCacheTTL - time.Second)
	cachePath = cache.metadataCachePath(spec, "action.yaml")
	if err := os.Chtimes(cachePath, old, old); err != nil {
		t.Fatal(err)
	}
	cache = newRemoteActionsCache(server.Client(), 1, dir, "", server.URL)
	if _, err := cache.FindMetadata(spec); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("made %d requests after expiring tag cache, wanted 2", got)
	}

	sha := "0123456789abcdef0123456789abcdef01234567"
	cache = newRemoteActionsCache(server.Client(), 1, dir, "", server.URL)
	if _, err := cache.FindMetadata("owner/repo@" + sha); err != nil {
		t.Fatal(err)
	}
	shaPath := cache.metadataCachePath("owner/repo@"+sha, "action.yaml")
	if err := os.Chtimes(shaPath, old, old); err != nil {
		t.Fatal(err)
	}
	cache = newRemoteActionsCache(server.Client(), 1, dir, "", server.URL)
	if _, err := cache.FindMetadata("owner/repo@" + sha); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 3 {
		t.Errorf("made %d requests for expired commit cache, wanted 3", got)
	}
}

func TestRemoteActionsCacheUsesAuthenticatedContentsAPI(t *testing.T) {
	metadata := "name: authenticated\nruns:\n  using: node20\n  main: index.js\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/owner/repo/contents/nested/action.yaml"; got != want {
			t.Errorf("path was %q, wanted %q", got, want)
		}
		if got, want := r.URL.Query().Get("ref"), "refs/tags/v1"; got != want {
			t.Errorf("ref was %q, wanted %q", got, want)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer test-token"; got != want {
			t.Errorf("Authorization was %q, wanted %q", got, want)
		}
		if got, want := r.Header.Get("Accept"), "application/vnd.github.raw+json"; got != want {
			t.Errorf("Accept was %q, wanted %q", got, want)
		}
		_, _ = w.Write([]byte(`{"content":"` + base64.StdEncoding.EncodeToString([]byte(metadata)) + `","encoding":"base64"}`))
	}))
	defer server.Close()

	cache := newRemoteActionsCache(server.Client(), 1, t.TempDir(), "test-token", server.URL)
	got, err := cache.FindMetadata("owner/repo/nested@refs/tags/v1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "authenticated" {
		t.Errorf("metadata name was %q, wanted authenticated", got.Name)
	}
}

func TestRemoteActionsCacheDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("redirect target received an authorization header")
		}
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()

	cache := newRemoteActionsCache(source.Client(), 1, t.TempDir(), "test-token", source.URL)
	if _, err := cache.FindMetadata("owner/repo@v1"); err == nil {
		t.Fatal("redirect response did not return an error")
	}
	if got := redirected.Load(); got != 0 {
		t.Errorf("followed redirect %d times, wanted 0", got)
	}
}

func TestRemoteActionsCacheRejectsNonRepositoryActions(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	cache := newRemoteActionsCache(server.Client(), 1, t.TempDir(), "", server.URL)
	for _, spec := range []string{
		"./action",
		"$/action",
		"docker://alpine:latest",
		"owner/repo@${{ github.ref }}",
		"owner/repo",
	} {
		if _, err := cache.FindMetadata(spec); err == nil {
			t.Errorf("FindMetadata(%q) succeeded", spec)
		}
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("made %d requests for invalid specifications, wanted 0", got)
	}
}
