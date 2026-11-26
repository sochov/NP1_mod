package update

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	Version      = "0.6.1"
	Socket       = "/run/np1-control-update/control.sock"
	manifestName = "np1cc-update.json"
	sigName      = "np1cc-update.json.sig"
	assetName    = "np1cc-update-linux-arm64.gz"
	maxMetadata  = 64 << 10
	maxBinary    = 48 << 20
	maxAsset     = 32 << 20
)

var (
	stableTag = regexp.MustCompile(`^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$`)
	repoName  = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)
)

type manifest struct {
	Schema     int      `json:"schema"`
	Product    string   `json:"product"`
	Target     string   `json:"target"`
	Version    string   `json:"version"`
	MinUpdater int      `json:"minUpdater"`
	From       []string `json:"fromSha256"`
	DriveRec   string   `json:"driveRecSha256"`
	Hook       string   `json:"hookSha256"`
	Injector   string   `json:"injectorSha256"`
	Asset      string   `json:"asset"`
	AssetSize  int64    `json:"assetSize"`
	AssetSHA   string   `json:"assetSha256"`
	BinarySize int64    `json:"binarySize"`
	BinarySHA  string   `json:"binarySha256"`
}

func hashValid(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && value == strings.ToLower(value)
}

func verifyManifest(raw, sig []byte, key ed25519.PublicKey) (manifest, error) {
	var m manifest
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, raw, sig) {
		return m, errors.New("更新情報の署名が一致しません")
	}
	if err := json.Unmarshal(raw, &m); err != nil || m.Schema != 1 || m.Product != "np1cc" ||
		m.Target != "linux/arm64" || m.MinUpdater != 1 || !stableTag.MatchString("v"+m.Version) ||
		m.Asset != assetName || m.AssetSize < 1 || m.AssetSize > maxAsset || m.BinarySize < 1 || m.BinarySize > maxBinary ||
		!hashValid(m.AssetSHA) || !hashValid(m.BinarySHA) || !hashValid(m.DriveRec) || !hashValid(m.Hook) || !hashValid(m.Injector) || len(m.From) == 0 {
		return m, errors.New("未対応の更新パッケージです")
	}
	for _, hash := range m.From {
		if !hashValid(hash) {
			return m, errors.New("更新元の識別子が不正です")
		}
	}
	return m, nil
}

func versionCompare(a, b string) int {
	partsA, partsB := stableTag.FindStringSubmatch("v"+a), stableTag.FindStringSubmatch("v"+b)
	if partsA == nil || partsB == nil {
		return strings.Compare(a, b)
	}
	for i := 1; i < len(partsA); i++ {
		if len(partsA[i]) != len(partsB[i]) {
			return len(partsA[i]) - len(partsB[i])
		}
		if order := strings.Compare(partsA[i], partsB[i]); order != 0 {
			return order
		}
	}
	return 0
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type githubRelease struct {
	Tag        string         `json:"tag_name"`
	Body       string         `json:"body"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

var errNoWiFi = errors.New("Wi-FiでGitHubに接続できません")

type downloadFunc func(context.Context, bool, string, int64, io.Writer) error

func downloadBytes(ctx context.Context, fetch downloadFunc, wifiOnly bool, address string, limit int64) ([]byte, error) {
	var data strings.Builder
	err := fetch(ctx, wifiOnly, address, limit, &data)
	return []byte(data.String()), err
}

func validURL(raw string, redirect bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return false
	}
	switch u.Hostname() {
	case "api.github.com", "github.com":
		return true
	case "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return redirect
	}
	return false
}

func downloadHTTP(ctx context.Context, client *http.Client, address string, limit int64, out io.Writer) error {
	if !validURL(address, false) {
		return errors.New("配布先のURLが不正です")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "NP1-Control-Center-Updater")
	if req.URL.Hostname() == "api.github.com" {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub HTTP %d", res.StatusCode)
	}
	if res.ContentLength > limit {
		return errors.New("配布ファイルが上限を超えています")
	}
	n, err := io.Copy(out, io.LimitReader(res.Body, limit+1))
	if err == nil && n > limit {
		return errors.New("配布ファイルが上限を超えています")
	}
	return err
}

type candidate struct {
	Version string `json:"version"`
	Notes   string `json:"notes,omitempty"`
	Size    int64  `json:"size,omitempty"`
	Reason  string `json:"reason,omitempty"`
	url     string
	raw     []byte
	sig     []byte
	meta    manifest
}

func releases(ctx context.Context, fetch downloadFunc, repository string, wifiOnly bool, current string) ([]githubRelease, error) {
	if !repoName.MatchString(repository) {
		return nil, errors.New("リリース用リポジトリ名が不正です")
	}
	address := "https://api.github.com/repos/" + repository + "/releases?per_page=30"
	raw, err := downloadBytes(ctx, fetch, wifiOnly, address, 1<<20)
	if err != nil {
		return nil, err
	}
	var list []githubRelease
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, errors.New("GitHubのリリース一覧を読み取れません")
	}
	list = slices.DeleteFunc(list, func(r githubRelease) bool {
		return r.Draft || r.Prerelease || !stableTag.MatchString(r.Tag) || versionCompare(r.Tag[1:], current) <= 0
	})
	slices.SortFunc(list, func(a, b githubRelease) int { return versionCompare(b.Tag[1:], a.Tag[1:]) })
	return list, nil
}

func releaseCandidate(ctx context.Context, fetch downloadFunc, key ed25519.PublicKey, repo string, wifiOnly bool, release githubRelease) (candidate, error) {
	item := candidate{Version: release.Tag[1:], Notes: strings.TrimSpace(release.Body)}
	if notes := []rune(item.Notes); len(notes) > 2000 {
		item.Notes = string(notes[:2000]) + "…"
	}
	assets := make(map[string]string)
	for _, asset := range release.Assets {
		expected := "https://github.com/" + repo + "/releases/download/" + release.Tag + "/" + asset.Name
		if asset.URL == expected {
			assets[asset.Name] = asset.URL
		}
	}
	if assets[manifestName] == "" || assets[sigName] == "" || assets[assetName] == "" {
		item.Reason = "この版は手動配備が必要です"
		return item, nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	var err error
	item.raw, err = downloadBytes(ctx, fetch, wifiOnly, assets[manifestName], maxMetadata)
	if err == nil {
		item.sig, err = downloadBytes(ctx, fetch, wifiOnly, assets[sigName], ed25519.SignatureSize)
	}
	if err != nil {
		return item, err
	}
	item.meta, err = verifyManifest(item.raw, item.sig, key)
	if err != nil {
		item.Reason = err.Error()
		return item, nil
	}
	if item.meta.Version != item.Version {
		item.Reason = "リリースと更新パッケージのバージョンが一致しません"
		return item, nil
	}
	item.Size, item.url = item.meta.AssetSize, assets[assetName]
	return item, nil
}
