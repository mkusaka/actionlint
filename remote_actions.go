package actionlint

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v4"
)

const (
	defaultRemoteActionsEndpoint    = "https://raw.githubusercontent.com"
	defaultRemoteActionsAPIEndpoint = "https://api.github.com"
	remoteActionsMaxBodySize        = 1 << 20
	remoteActionsDefaultTimeout     = 10 * time.Second
	remoteActionsCacheTTL           = 24 * time.Hour
)

// RemoteActionsCache fetches and caches action metadata from GitHub repositories.
// Its methods are safe for concurrent use.
type RemoteActionsCache struct {
	client   *http.Client
	endpoint *url.URL
	token    string
	cacheDir string
	fetches  chan struct{}

	mu      sync.Mutex
	entries map[string]*remoteActionEntry
}

type remoteActionEntry struct {
	metadata *ActionMetadata
	err      error
	done     chan struct{}
}

type remoteActionSpec struct {
	owner string
	repo  string
	path  []string
	ref   string
}

// NewRemoteActionsCache creates a cache for remote action metadata. cacheDir stores successfully
// fetched metadata across process invocations. With a non-empty token, metadata is fetched through
// GitHub's authenticated contents API; otherwise it is fetched from raw.githubusercontent.com.
// concurrency limits simultaneous fetches of distinct action specs. A non-positive concurrency uses
// one fetch worker. When client is nil, a client with a timeout and redirects disabled is used.
func NewRemoteActionsCache(client *http.Client, concurrency int, cacheDir, token string) *RemoteActionsCache {
	endpoint := defaultRemoteActionsEndpoint
	if token != "" {
		endpoint = defaultRemoteActionsAPIEndpoint
	}
	return newRemoteActionsCache(client, concurrency, cacheDir, token, endpoint)
}

// newRemoteActionsCache allows package tests to use an httptest server for raw and API requests.
func newRemoteActionsCache(client *http.Client, concurrency int, cacheDir, token, endpoint string) *RemoteActionsCache {
	endpointURL, err := url.Parse(endpoint)
	if err != nil || (endpointURL.Scheme != "http" && endpointURL.Scheme != "https") || endpointURL.Host == "" {
		panic("remote actions endpoint must be an absolute HTTP URL")
	}
	endpointPath := strings.TrimRight(endpointURL.Path, "/")
	endpointRawPath := strings.TrimRight(endpointURL.EscapedPath(), "/")
	endpointURL.Path = endpointPath
	endpointURL.RawPath = endpointRawPath
	endpointURL.RawQuery = ""
	endpointURL.Fragment = ""

	if concurrency < 1 {
		concurrency = 1
	}
	client = remoteActionsHTTPClient(client)

	return &RemoteActionsCache{
		client:   client,
		endpoint: endpointURL,
		token:    token,
		cacheDir: cacheDir,
		fetches:  make(chan struct{}, concurrency),
		entries:  make(map[string]*remoteActionEntry),
	}
}

func remoteActionsHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{Timeout: remoteActionsDefaultTimeout}
	}
	clone := *client
	clone.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone
}

// FindMetadata finds metadata for a static GitHub action specification in
// owner/repo[/subpath]@ref form. The exact supplied ref is requested; this cache never resolves
// it to another version. Both successful results and errors are cached per specification.
func (c *RemoteActionsCache) FindMetadata(spec string) (*ActionMetadata, error) {
	c.mu.Lock()
	if entry, ok := c.entries[spec]; ok {
		c.mu.Unlock()
		<-entry.done
		return entry.metadata, entry.err
	}
	entry := &remoteActionEntry{done: make(chan struct{})}
	c.entries[spec] = entry
	c.mu.Unlock()

	metadata, err := c.findMetadata(spec)

	c.mu.Lock()
	entry.metadata = metadata
	entry.err = err
	close(entry.done)
	c.mu.Unlock()
	return metadata, err
}

func (c *RemoteActionsCache) findMetadata(spec string) (*ActionMetadata, error) {
	action, err := parseRemoteActionSpec(spec)
	if err != nil {
		return nil, err
	}

	for _, file := range []string{"action.yaml", "action.yml"} {
		if body, ok := c.readCachedMetadata(spec, file, action.ref); ok {
			metadata, err := parseRemoteActionMetadata(body)
			if err == nil {
				return metadata, nil
			}
			c.removeCachedMetadata(spec, file)
		}
	}

	for _, file := range []string{"action.yaml", "action.yml"} {
		body, notFound, err := c.fetchActionMetadata(action, file)
		if err != nil {
			return nil, fmt.Errorf("could not fetch action metadata for %q: %w", spec, err)
		}
		if notFound {
			continue
		}

		metadata, err := parseRemoteActionMetadata(body)
		if err != nil {
			return nil, fmt.Errorf("could not parse action metadata for %q: %w", spec, err)
		}
		c.writeCachedMetadata(spec, file, body)
		return metadata, nil
	}

	return nil, fmt.Errorf("could not find action metadata for %q", spec)
}

func parseRemoteActionMetadata(body []byte) (*ActionMetadata, error) {
	var metadata ActionMetadata
	if err := yaml.Unmarshal(body, &metadata); err != nil {
		return nil, err
	}
	if metadata.Name == "" || metadata.Runs.Using == "" {
		return nil, fmt.Errorf("action metadata must define name and runs.using")
	}
	return &metadata, nil
}

func (c *RemoteActionsCache) fetchActionMetadata(action remoteActionSpec, file string) ([]byte, bool, error) {
	c.fetches <- struct{}{}
	defer func() { <-c.fetches }()

	requestURL := c.actionMetadataURL(action, file)
	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, false, err
	}
	if c.token != "" && request.URL.Host == c.endpoint.Host {
		request.Header.Set("Authorization", "Bearer "+c.token)
		request.Header.Set("Accept", "application/vnd.github.raw+json")
	}

	response, err := c.client.Do(request)
	if err != nil {
		return nil, false, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return nil, true, nil
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, false, fmt.Errorf("request to %s returned %s", requestURL, response.Status)
	}
	if response.ContentLength > remoteActionsMaxBodySize {
		return nil, false, fmt.Errorf("response from %s exceeds 1 MiB", requestURL)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, remoteActionsMaxBodySize+1))
	if err != nil {
		return nil, false, err
	}
	if len(body) > remoteActionsMaxBodySize {
		return nil, false, fmt.Errorf("response from %s exceeds 1 MiB", requestURL)
	}
	if c.token == "" {
		return body, false, nil
	}
	body, err = decodeGitHubContentsResponse(body)
	if err != nil {
		return nil, false, err
	}
	if len(body) > remoteActionsMaxBodySize {
		return nil, false, fmt.Errorf("response from %s exceeds 1 MiB", requestURL)
	}
	return body, false, nil
}

func decodeGitHubContentsResponse(body []byte) ([]byte, error) {
	var content struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(body, &content); err != nil || content.Encoding == "" {
		return body, nil
	}
	if content.Encoding != "base64" {
		return nil, fmt.Errorf("GitHub contents response has unsupported encoding %q", content.Encoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("could not decode GitHub contents response: %w", err)
	}
	return decoded, nil
}

func (c *RemoteActionsCache) actionMetadataURL(action remoteActionSpec, file string) string {
	parts := make([]string, 0, len(action.path)+5)
	if c.token != "" {
		parts = append(parts, "repos", action.owner, action.repo, "contents")
		parts = append(parts, action.path...)
		parts = append(parts, file)
		return c.endpointURL(parts, url.Values{"ref": {action.ref}})
	}
	parts = append(parts, action.owner, action.repo, action.ref)
	parts = append(parts, action.path...)
	parts = append(parts, file)
	return c.endpointURL(parts, nil)
}

func (c *RemoteActionsCache) endpointURL(parts []string, query url.Values) string {
	endpoint := *c.endpoint
	escapedPath := endpoint.EscapedPath()
	for _, part := range parts {
		endpoint.Path += "/" + part
		escapedPath += "/" + url.PathEscape(part)
	}
	endpoint.RawPath = escapedPath
	if query != nil {
		endpoint.RawQuery = query.Encode()
	}
	return endpoint.String()
}

func (c *RemoteActionsCache) readCachedMetadata(spec, file, ref string) ([]byte, bool) {
	if c.cacheDir == "" {
		return nil, false
	}
	path := c.metadataCachePath(spec, file)
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if (!fullActionCommit.MatchString(ref) && time.Since(info.ModTime()) > remoteActionsCacheTTL) || info.Size() > remoteActionsMaxBodySize {
		c.removeCachedMetadata(spec, file)
		return nil, false
	}

	cache, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(cache, remoteActionsMaxBodySize+1))
	closeErr := cache.Close()
	if err != nil || closeErr != nil || len(body) > remoteActionsMaxBodySize {
		c.removeCachedMetadata(spec, file)
		return nil, false
	}
	return body, true
}

func (c *RemoteActionsCache) writeCachedMetadata(spec, file string, body []byte) {
	if c.cacheDir == "" {
		return
	}
	if err := os.MkdirAll(c.cacheDir, 0o700); err != nil {
		return
	}
	path := c.metadataCachePath(spec, file)
	cache, err := os.CreateTemp(c.cacheDir, ".remote-action-*")
	if err != nil {
		return
	}
	temp := cache.Name()
	_, err = cache.Write(body)
	if closeErr := cache.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(temp)
		return
	}
	if err := os.Rename(temp, path); err != nil {
		os.Remove(temp)
	}
}

func (c *RemoteActionsCache) removeCachedMetadata(spec, file string) {
	if c.cacheDir != "" {
		os.Remove(c.metadataCachePath(spec, file))
	}
}

func (c *RemoteActionsCache) metadataCachePath(spec, file string) string {
	sum := sha256.Sum256([]byte(c.endpoint.Host + "\x00" + spec + "\x00" + file))
	return filepath.Join(c.cacheDir, hex.EncodeToString(sum[:])+".yaml")
}

func parseRemoteActionSpec(spec string) (remoteActionSpec, error) {
	if ContainsExpression(spec) {
		return remoteActionSpec{}, invalidRemoteActionSpec(spec, "it contains an expression")
	}
	if strings.HasPrefix(spec, "docker://") || strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") || strings.HasPrefix(spec, "$/") {
		return remoteActionSpec{}, invalidRemoteActionSpec(spec, "it is not a GitHub repository action")
	}

	at := strings.IndexByte(spec, '@')
	if at < 0 {
		return remoteActionSpec{}, invalidRemoteActionSpec(spec, "ref is missing")
	}
	action, ref := spec[:at], spec[at+1:]
	parts := strings.Split(action, "/")
	if len(parts) < 2 || !validRemoteActionPart(parts[0]) || !validRemoteActionPart(parts[1]) {
		return remoteActionSpec{}, invalidRemoteActionSpec(spec, "owner and repository are required")
	}
	for _, part := range parts[2:] {
		if !validRemoteActionPart(part) {
			return remoteActionSpec{}, invalidRemoteActionSpec(spec, "action path is invalid")
		}
	}
	if !validRemoteActionRef(ref) {
		return remoteActionSpec{}, invalidRemoteActionSpec(spec, "ref is invalid")
	}

	return remoteActionSpec{owner: parts[0], repo: parts[1], path: parts[2:], ref: ref}, nil
}

func invalidRemoteActionSpec(spec, reason string) error {
	return fmt.Errorf("invalid remote action specification %q: %s", spec, reason)
}

func validRemoteActionPart(part string) bool {
	if part == "" || part == "." || part == ".." {
		return false
	}
	for _, r := range part {
		if r <= ' ' || r == 0x7f || r == '\\' {
			return false
		}
	}
	return true
}

func validRemoteActionRef(ref string) bool {
	if ref == "" || ref == "@" || strings.HasPrefix(ref, "/") || strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".") || strings.Contains(ref, "..") || strings.Contains(ref, "@{") || strings.ContainsAny(ref, " ~^:?*[\\") {
		return false
	}
	for _, part := range strings.Split(ref, "/") {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	for _, r := range ref {
		if r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}
