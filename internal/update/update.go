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
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"github.com/alexwoo79/duckdb-report/internal/model"
)

const (
	defaultAPIBaseURL = "https://api.github.com"
	checksumAssetName = "checksums.txt"
	maxChecksumSize   = 2 << 20
	maxUpdateSize     = int64(1 << 30)
	maxReleaseNotes   = 50000
)

type Config struct {
	Repository     string
	CurrentVersion string
	APIBaseURL     string
	Client         *http.Client
	GOOS           string
	GOARCH         string
	UserAgent      string
}

type Checker struct {
	repository     string
	currentVersion string
	apiBaseURL     string
	client         *http.Client
	goos           string
	goarch         string
	userAgent      string
}

type Release struct {
	info      model.UpdateInfo
	asset     *githubAsset
	checksums *githubAsset
}

func (r *Release) Info() model.UpdateInfo {
	if r == nil {
		return model.UpdateInfo{}
	}
	return r.info
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	HTMLURL     string        `json:"html_url"`
	PublishedAt string        `json:"published_at"`
	Body        string        `json:"body"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type semanticVersion struct {
	major      int
	minor      int
	patch      int
	prerelease []string
}

func NewChecker(config Config) *Checker {
	apiBaseURL := strings.TrimSpace(config.APIBaseURL)
	if apiBaseURL == "" {
		apiBaseURL = defaultAPIBaseURL
	}
	client := config.Client
	if client == nil {
		client = http.DefaultClient
	}
	goos := strings.TrimSpace(config.GOOS)
	if goos == "" {
		goos = goruntime.GOOS
	}
	goarch := strings.TrimSpace(config.GOARCH)
	if goarch == "" {
		goarch = goruntime.GOARCH
	}
	userAgent := strings.TrimSpace(config.UserAgent)
	if userAgent == "" {
		userAgent = "duckdb-report-update-checker"
	}
	return &Checker{
		repository:     strings.TrimSpace(config.Repository),
		currentVersion: strings.TrimSpace(config.CurrentVersion),
		apiBaseURL:     strings.TrimRight(apiBaseURL, "/"),
		client:         client,
		goos:           goos,
		goarch:         goarch,
		userAgent:      userAgent,
	}
}

func (c *Checker) Check(ctx context.Context) (*Release, error) {
	if c.repository == "" {
		return nil, errors.New("update repository is not configured")
	}
	endpoint := c.apiBaseURL + "/repos/" + c.repository + "/releases/latest"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create github release request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", c.userAgent)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request github release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf(
			"request github release: %s: %s",
			response.Status,
			strings.TrimSpace(string(body)),
		)
	}

	var payload githubRelease
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode github release: %w", err)
	}
	if payload.Draft || payload.Prerelease {
		return nil, errors.New("github did not return a stable release")
	}
	latest, err := parseSemanticVersion(payload.TagName)
	if err != nil {
		return nil, fmt.Errorf("parse latest release version %q: %w", payload.TagName, err)
	}
	current, err := parseSemanticVersion(c.currentVersion)
	if err != nil {
		return nil, fmt.Errorf("parse current application version %q: %w", c.currentVersion, err)
	}

	targetAsset := selectTargetAsset(payload.Assets, c.goos, c.goarch)
	checksums := findAsset(payload.Assets, checksumAssetName)
	notes := payload.Body
	if len(notes) > maxReleaseNotes {
		notes = notes[:maxReleaseNotes]
	}
	release := &Release{
		info: model.UpdateInfo{
			CurrentVersion: versionString(current),
			LatestVersion:  versionString(latest),
			Available:      compareVersions(latest, current) > 0,
			ReleaseName:    fallback(payload.Name, payload.TagName),
			ReleaseURL:     payload.HTMLURL,
			PublishedAt:    payload.PublishedAt,
			ReleaseNotes:   notes,
		},
		asset:     targetAsset,
		checksums: checksums,
	}
	if targetAsset != nil {
		release.info.AssetName = targetAsset.Name
		release.info.AssetSize = targetAsset.Size
	}
	return release, nil
}

func (c *Checker) Download(
	ctx context.Context,
	release *Release,
	destinationDir string,
) (model.UpdateDownload, error) {
	if release == nil {
		return model.UpdateDownload{}, errors.New("check for updates before downloading")
	}
	if release.asset == nil {
		return model.UpdateDownload{}, fmt.Errorf(
			"release %s has no package for %s/%s",
			release.info.LatestVersion,
			c.goos,
			c.goarch,
		)
	}
	if release.checksums == nil {
		return model.UpdateDownload{}, errors.New("release checksums are not available")
	}
	destinationDir = strings.TrimSpace(destinationDir)
	if destinationDir == "" {
		return model.UpdateDownload{}, errors.New("update download directory is empty")
	}
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return model.UpdateDownload{}, fmt.Errorf("create update download directory: %w", err)
	}

	assetName := filepath.Base(release.asset.Name)
	if assetName != release.asset.Name {
		return model.UpdateDownload{}, fmt.Errorf("invalid update asset name %q", release.asset.Name)
	}
	expectedChecksum, err := c.readChecksum(ctx, release.checksums, assetName)
	if err != nil {
		return model.UpdateDownload{}, err
	}

	destination := filepath.Join(destinationDir, assetName)
	temporary := filepath.Join(
		destinationDir,
		"."+assetName+".part-"+time.Now().Format("20060102150405.000000000"),
	)
	defer os.Remove(temporary)

	written, err := c.downloadFile(ctx, release.asset.BrowserDownloadURL, temporary, maxUpdateSize)
	if err != nil {
		return model.UpdateDownload{}, fmt.Errorf("download update package: %w", err)
	}
	actualChecksum, err := fileSHA256(temporary)
	if err != nil {
		return model.UpdateDownload{}, err
	}
	if !strings.EqualFold(actualChecksum, expectedChecksum) {
		return model.UpdateDownload{}, fmt.Errorf(
			"verify update package: checksum mismatch for %q",
			assetName,
		)
	}
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return model.UpdateDownload{}, fmt.Errorf("replace existing update package: %w", err)
	}
	if err := os.Rename(temporary, destination); err != nil {
		return model.UpdateDownload{}, fmt.Errorf("store update package: %w", err)
	}
	return model.UpdateDownload{
		Name: assetName,
		Path: destination,
		Size: written,
	}, nil
}

func (c *Checker) readChecksum(
	ctx context.Context,
	asset *githubAsset,
	targetName string,
) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("create checksum request: %w", err)
	}
	request.Header.Set("User-Agent", c.userAgent)
	response, err := c.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download release checksums: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download release checksums: %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxChecksumSize+1))
	if err != nil {
		return "", fmt.Errorf("read release checksums: %w", err)
	}
	if len(body) > maxChecksumSize {
		return "", errors.New("release checksums are too large")
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.TrimPrefix(fields[1], "*") != targetName {
			continue
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size {
			return "", fmt.Errorf("invalid checksum for %q", targetName)
		}
		return fields[0], nil
	}
	return "", fmt.Errorf("checksum for %q was not found", targetName)
}

func (c *Checker) downloadFile(
	ctx context.Context,
	url string,
	destination string,
	maxSize int64,
) (int64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("create download request: %w", err)
	}
	request.Header.Set("User-Agent", c.userAgent)
	response, err := c.client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("request update package: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("request update package: %s", response.Status)
	}

	file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, fmt.Errorf("create downloaded update: %w", err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, maxSize+1))
	closeErr := file.Close()
	if copyErr != nil {
		return 0, fmt.Errorf("write downloaded update: %w", copyErr)
	}
	if closeErr != nil {
		return 0, fmt.Errorf("close downloaded update: %w", closeErr)
	}
	if written > maxSize {
		return 0, errors.New("update package exceeds the size limit")
	}
	return written, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open downloaded update: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash downloaded update: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func selectTargetAsset(assets []githubAsset, goos, goarch string) *githubAsset {
	var suffix string
	switch goos {
	case "darwin":
		suffix = "-macos-universal.zip"
	case "windows":
		if goarch == "amd64" {
			suffix = "-windows-amd64.zip"
		}
	case "linux":
		if goarch == "amd64" {
			suffix = "-linux-amd64.zip"
		}
	}
	if suffix == "" {
		return nil
	}
	for index := range assets {
		if strings.HasSuffix(strings.ToLower(assets[index].Name), suffix) {
			return &assets[index]
		}
	}
	return nil
}

func findAsset(assets []githubAsset, name string) *githubAsset {
	for index := range assets {
		if strings.EqualFold(assets[index].Name, name) {
			return &assets[index]
		}
	}
	return nil
}

func parseSemanticVersion(value string) (semanticVersion, error) {
	normalized := strings.TrimSpace(value)
	normalized = strings.TrimPrefix(normalized, "v")
	if index := strings.IndexByte(normalized, '+'); index >= 0 {
		normalized = normalized[:index]
	}
	prerelease := []string{}
	if index := strings.IndexByte(normalized, '-'); index >= 0 {
		prerelease = strings.Split(normalized[index+1:], ".")
		normalized = normalized[:index]
	}
	parts := strings.Split(normalized, ".")
	if len(parts) != 3 {
		return semanticVersion{}, errors.New("version must contain major, minor and patch")
	}
	numbers := make([]int, len(parts))
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return semanticVersion{}, fmt.Errorf("invalid version component %q", part)
		}
		numbers[index] = number
	}
	return semanticVersion{
		major:      numbers[0],
		minor:      numbers[1],
		patch:      numbers[2],
		prerelease: prerelease,
	}, nil
}

func compareVersions(left, right semanticVersion) int {
	for _, pair := range [][2]int{
		{left.major, right.major},
		{left.minor, right.minor},
		{left.patch, right.patch},
	} {
		switch {
		case pair[0] > pair[1]:
			return 1
		case pair[0] < pair[1]:
			return -1
		}
	}
	switch {
	case len(left.prerelease) == 0 && len(right.prerelease) > 0:
		return 1
	case len(left.prerelease) > 0 && len(right.prerelease) == 0:
		return -1
	}
	for index := 0; index < len(left.prerelease) && index < len(right.prerelease); index++ {
		if left.prerelease[index] == right.prerelease[index] {
			continue
		}
		if left.prerelease[index] < right.prerelease[index] {
			return -1
		}
		return 1
	}
	switch {
	case len(left.prerelease) < len(right.prerelease):
		return -1
	case len(left.prerelease) > len(right.prerelease):
		return 1
	default:
		return 0
	}
}

func versionString(value semanticVersion) string {
	result := fmt.Sprintf("%d.%d.%d", value.major, value.minor, value.patch)
	if len(value.prerelease) > 0 {
		result += "-" + strings.Join(value.prerelease, ".")
	}
	return result
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}
