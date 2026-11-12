// Package update compares this binary with the latest GitHub release and,
// when asked, replaces it after the bytes match the SHA-256 in SHA256SUMS.
//
// The comparison is pure. Download and replace are separate and only run
// from the CLI, never from a poll.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/maci0/subman/internal/model"
	"golang.org/x/mod/semver"
)

// noRedirect refuses every redirect. FetchLatest may carry a GitHub token,
// and Install fetches the binary that replaces this one; either following a
// redirect would trust a host this code never checked.
var noRedirect = &http.Client{
	// The callers also bound the request with a context. This caps a
	// connection that never reaches a response, which a context does not:
	// it only ends a request once the headers have started.
	Timeout: 30 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

const (
	DefaultRepo = "maci0/subman"
	maxMeta     = 1 << 20
	maxSums     = 64 << 10
	maxAsset    = 80 << 20
	assetPrefix = "subman-"
)

// ErrNoRelease is a repo with no published release. Check treats it as
// "nothing to install", not a transport failure.
var ErrNoRelease = errors.New("no release published")

// Release is the slice of a GitHub release this package needs.
type Release struct {
	Tag    string
	Assets []Asset
}

type Asset struct {
	Name string
	URL  string
}

// Plan is the decision Check makes before any byte is written.
type Plan struct {
	Status   model.UpdateStatus
	Asset    Asset
	Sums     Asset
	WantName string
}

// Check reports whether tag is newer than running, and which asset would
// be installed. It does not download the binary.
func Check(running, tag string, assets []Asset) Plan {
	return checkFor(running, tag, assets, AssetName(runtime.GOOS, runtime.GOARCH))
}

// checkFor is Check for a named asset, so the decision is tested for every
// released target and not only the host the test happens to run on.
func checkFor(running, tag string, assets []Asset, name string) Plan {
	st := model.UpdateStatus{Current: running, Latest: bare(tag)}
	plan := Plan{Status: st, WantName: name}
	if name == "" {
		st.Note = "no release asset for this os/arch"
		plan.Status = st
		return plan
	}
	if !semver.IsValid("v"+bare(running)) || !semver.IsValid("v"+bare(tag)) {
		st.Note = "version is not semver; no comparison"
		plan.Status = st
		return plan
	}
	switch semver.Compare("v"+bare(running), "v"+bare(tag)) {
	case 0:
		st.Note = "up to date"
	case 1:
		st.Ahead = true
		st.Note = "this build is newer than the published release"
	default:
		st.Available = true
		st.Note = "update available"
	}
	for _, a := range assets {
		switch a.Name {
		case name:
			plan.Asset = a
		case "SHA256SUMS":
			plan.Sums = a
		}
	}
	if st.Available && plan.Asset.URL == "" {
		st.Available = false
		st.Note = "release has no " + name
	}
	if st.Available && plan.Sums.URL == "" {
		st.Available = false
		st.Note = "release has no SHA256SUMS"
	}
	plan.Status = st
	return plan
}

// AssetName is the file a release publishes for this target.
func AssetName(goos, goarch string) string {
	switch goos {
	case "linux", "darwin", "windows":
	default:
		return ""
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return ""
	}
	name := assetPrefix + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// FetchLatest reads the newest release. Only api.github.com is contacted.
func FetchLatest(ctx context.Context, repo string) (Release, error) {
	if !validRepo(repo) {
		return Release{}, fmt.Errorf("refusing repo %q", repo)
	}
	url := "https://api.github.com/repos/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "subman-update")
	// The token was issued for this tool's own repo. SUBMAN_UPDATE_REPO may
	// name another one, and the token must not be sent there.
	if repo == DefaultRepo {
		if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	// The token, when set, was issued for api.github.com. A redirect would
	// carry it to the host the answer names.
	res, err := noRedirect.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxMeta+1))
	if err != nil {
		return Release{}, err
	}
	if res.StatusCode == 404 {
		return Release{}, ErrNoRelease
	}
	if res.StatusCode != 200 || len(body) > maxMeta {
		return Release{}, fmt.Errorf("releases api answered %d", res.StatusCode)
	}
	var raw struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Release{}, err
	}
	rel := Release{Tag: raw.TagName}
	for _, a := range raw.Assets {
		if !trustedURL(a.URL) {
			continue
		}
		rel.Assets = append(rel.Assets, Asset{Name: a.Name, URL: a.URL})
	}
	return rel, nil
}

// Install downloads the planned asset, checks SHA256SUMS, and replaces dest.
func Install(ctx context.Context, plan Plan, dest string) error {
	if !plan.Status.Available {
		return errors.New(plan.Status.Note)
	}
	if !trustedURL(plan.Asset.URL) || !trustedURL(plan.Sums.URL) {
		return errors.New("refusing untrusted asset url")
	}
	sums, err := download(ctx, plan.Sums.URL, maxSums)
	if err != nil {
		return fmt.Errorf("checksums: %w", err)
	}
	want, ok := sumFor(string(sums), plan.WantName)
	if !ok {
		return fmt.Errorf("SHA256SUMS has no line for %s", plan.WantName)
	}
	blob, err := download(ctx, plan.Asset.URL, maxAsset)
	if err != nil {
		return fmt.Errorf("asset: %w", err)
	}
	got := sha256.Sum256(blob)
	if hex.EncodeToString(got[:]) != want {
		return errors.New("checksum mismatch")
	}
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, "subman-update-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(blob); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Windows keeps an executable locked while it runs, so renaming onto dest
	// fails for the very binary this command is replacing. Moving dest aside
	// first is allowed, and the new file then takes its name. A failed
	// replace puts dest back. Other platforms rename onto dest directly.
	if err := replaceFile(tmpName, dest); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "subman-update")
	// A redirect leaves the host trustedURL already checked. Following it
	// would fetch the replacement binary from whatever host the answer names.
	res, err := noRedirect.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("answered %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("over size cap")
	}
	return body, nil
}

func sumFor(sums, name string) (string, bool) {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != name {
			continue
		}
		if len(fields[0]) != 64 || fields[0] != strings.ToLower(fields[0]) {
			return "", false
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return "", false
		}
		return fields[0], true
	}
	return "", false
}

func trustedURL(raw string) bool {
	// The host has to be exactly one of the two, not a string that starts
	// with one: "https://github.com.evil.example/…" has the same prefix as
	// "https://github.com/" and is a different host. Userinfo is refused
	// for the same reason: the prefix would match while the request goes
	// where the userinfo names.
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	switch u.Scheme + "://" + u.Host + "/" {
	case "https://github.com/", "https://release-assets.github.com/":
		return true
	}
	return false
}

func validRepo(repo string) bool {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || strings.Contains(name, "/") {
		return false
	}
	return partOK(owner) && partOK(name)
}

func partOK(s string) bool {
	// GitHub rejects an empty part, one over 100 characters, and one that
	// starts with '.' or '-'. The first two also stop a path segment.
	if s == "" || len(s) > 100 || s[0] == '.' || s[0] == '-' {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

func bare(tag string) string {
	return strings.TrimPrefix(tag, "v")
}
