package update

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alexwoo79/duckdb-report/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestDownloadProgressReaderReportsProgress(t *testing.T) {
	t.Parallel()
	var downloaded int64
	var total int64
	reader := &downloadProgressReader{
		reader: strings.NewReader("update package"),
		total:  100,
		onProgress: func(current, expected int64) {
			downloaded = current
			total = expected
		},
	}

	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatal(err)
	}
	if downloaded != int64(len("update package")) {
		t.Fatalf("downloaded = %d", downloaded)
	}
	if total != 100 {
		t.Fatalf("total = %d", total)
	}
}

func TestDownloadWithProgressReportsAllPhases(t *testing.T) {
	t.Parallel()
	packageData := []byte("verified update package")
	sum := sha256.Sum256(packageData)
	checksum := fmt.Sprintf("%s  duckdb-report.zip\n", hex.EncodeToString(sum[:]))
	client := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			var body []byte
			switch request.URL.Host {
			case "checksums.example":
				body = []byte(checksum)
			case "download.example":
				body = packageData
			default:
				return nil, fmt.Errorf("unexpected URL %s", request.URL)
			}
			return &http.Response{
				StatusCode:    http.StatusOK,
				Status:        "200 OK",
				Header:        make(http.Header),
				Body:          io.NopCloser(bytes.NewReader(body)),
				ContentLength: int64(len(body)),
				Request:       request,
			}, nil
		}),
	}
	checker := NewChecker(Config{Client: client})
	release := &Release{
		info: model.UpdateInfo{
			LatestVersion: "2.0.8",
			AssetName:     "duckdb-report.zip",
			AssetSize:     int64(len(packageData)),
		},
		asset: &githubAsset{
			Name:               "duckdb-report.zip",
			BrowserDownloadURL: "https://download.example/duckdb-report.zip",
		},
		checksums: &githubAsset{
			Name:               checksumAssetName,
			BrowserDownloadURL: "https://checksums.example/checksums.txt",
		},
	}

	var phases []string
	download, err := checker.DownloadWithProgress(
		t.Context(),
		release,
		t.TempDir(),
		func(progress Progress) {
			phases = append(phases, progress.Phase)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if download.Size != int64(len(packageData)) {
		t.Fatalf("download size = %d", download.Size)
	}
	if len(phases) < 4 ||
		phases[0] != ProgressPreparing ||
		phases[len(phases)-2] != ProgressVerifying ||
		phases[len(phases)-1] != ProgressComplete {
		t.Fatalf("progress phases = %#v", phases)
	}
	if !slices.Contains(phases, ProgressDownloading) {
		t.Fatalf("progress phases = %#v, missing %q", phases, ProgressDownloading)
	}
}

func TestCheckerCheck(t *testing.T) {
	tests := []struct {
		name       string
		goos       string
		goarch     string
		current    string
		tag        string
		wantAsset  string
		wantUpdate bool
	}{
		{
			name:       "macOS update",
			goos:       "darwin",
			goarch:     "arm64",
			current:    "2.0.7",
			tag:        "v2.0.8",
			wantAsset:  "duckdb-report-v2.0.8-macos-universal.zip",
			wantUpdate: true,
		},
		{
			name:       "Windows update",
			goos:       "windows",
			goarch:     "amd64",
			current:    "2.0.7",
			tag:        "v2.0.8",
			wantAsset:  "duckdb-report-v2.0.8-windows-amd64.zip",
			wantUpdate: true,
		},
		{
			name:       "Linux current",
			goos:       "linux",
			goarch:     "amd64",
			current:    "2.0.8",
			tag:        "v2.0.8",
			wantAsset:  "duckdb-report-v2.0.8-linux-amd64.zip",
			wantUpdate: false,
		},
		{
			name:       "unsupported platform",
			goos:       "linux",
			goarch:     "arm64",
			current:    "2.0.7",
			tag:        "v2.0.8",
			wantAsset:  "",
			wantUpdate: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/repos/acme/app/releases/latest" {
					http.NotFound(writer, request)
					return
				}
				writer.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(writer, `{
					"tag_name": %q,
					"name": "DuckDB Report Studio 2.0.8",
					"html_url": "https://github.com/acme/app/releases/tag/v2.0.8",
					"published_at": "2026-11-24T00:00:00Z",
					"body": "Release notes",
					"draft": false,
					"prerelease": false,
					"assets": [
						{"name": "duckdb-report-v2.0.8-macos-universal.zip", "browser_download_url": %q, "size": 1024},
						{"name": "duckdb-report-v2.0.8-windows-amd64.zip", "browser_download_url": %q, "size": 2048},
						{"name": "duckdb-report-v2.0.8-linux-amd64.zip", "browser_download_url": %q, "size": 4096},
						{"name": "checksums.txt", "browser_download_url": %q, "size": 128}
					]
				}`,
					test.tag,
					server.URL+"/macos.zip",
					server.URL+"/windows.zip",
					server.URL+"/linux.zip",
					server.URL+"/checksums.txt",
				)
			}))
			defer server.Close()

			checker := NewChecker(Config{
				Repository:     "acme/app",
				CurrentVersion: test.current,
				APIBaseURL:     server.URL,
				Client:         server.Client(),
				GOOS:           test.goos,
				GOARCH:         test.goarch,
			})
			release, err := checker.Check(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			info := release.Info()
			if info.Available != test.wantUpdate {
				t.Fatalf("Available = %t, want %t", info.Available, test.wantUpdate)
			}
			if info.LatestVersion != "2.0.8" {
				t.Fatalf("LatestVersion = %q", info.LatestVersion)
			}
			if info.AssetName != test.wantAsset {
				t.Fatalf("AssetName = %q, want %q", info.AssetName, test.wantAsset)
			}
			if info.ReleaseNotes != "Release notes" {
				t.Fatalf("ReleaseNotes = %q", info.ReleaseNotes)
			}
		})
	}
}

func TestCheckerCheckRejectsInvalidRelease(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		tag     string
		message string
	}{
		{
			name:    "non-success status",
			status:  http.StatusBadGateway,
			message: "request github release: HTTP 502",
		},
		{
			name:    "invalid version",
			status:  http.StatusOK,
			tag:     "nightly",
			message: "parse latest release version",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
				if test.status == http.StatusOK {
					fmt.Fprintf(writer, `{"tag_name": %q, "assets": []}`, test.tag)
				}
			}))
			defer server.Close()
			checker := NewChecker(Config{
				Repository:     "acme/app",
				CurrentVersion: "2.0.7",
				APIBaseURL:     server.URL,
				Client:         server.Client(),
				GOOS:           "darwin",
				GOARCH:         "arm64",
			})
			_, err := checker.Check(t.Context())
			if err == nil {
				t.Fatal("expected release check to fail")
			}
			if got := err.Error(); !strings.Contains(got, test.message) {
				t.Fatalf("error = %q, want substring %q", got, test.message)
			}
		})
	}
}

func TestCheckerCheckDoesNotExposeHTTPResponseBody(t *testing.T) {
	t.Parallel()
	const serverDetail = "upstream detail that should not reach the UI"
	client := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Status:     "502 Bad Gateway",
				Header:     make(http.Header),
				Body: io.NopCloser(
					strings.NewReader(strings.Repeat(serverDetail, 100)),
				),
				Request: request,
			}, nil
		}),
	}
	checker := NewChecker(Config{
		Repository:     "acme/app",
		CurrentVersion: "2.0.7",
		APIBaseURL:     "https://api.example",
		Client:         client,
	})

	_, err := checker.Check(t.Context())
	if err == nil {
		t.Fatal("expected release check to fail")
	}
	if strings.Contains(err.Error(), serverDetail) || len(err.Error()) > 120 {
		t.Fatalf("error exposes response body: %q", err)
	}
}

func TestCheckerDownload(t *testing.T) {
	tests := []struct {
		name        string
		checksum    bool
		wantFailure bool
	}{
		{
			name:     "verified package",
			checksum: true,
		},
		{
			name:        "checksum mismatch",
			checksum:    false,
			wantFailure: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			packageData := []byte("verified update package")
			sum := sha256.Sum256(packageData)
			checksumValue := hex.EncodeToString(sum[:])
			if !test.checksum {
				checksumValue = "0000000000000000000000000000000000000000000000000000000000000000"
			}
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/repos/acme/app/releases/latest":
					fmt.Fprintf(writer, `{
						"tag_name": "v2.0.8",
						"name": "Release 2.0.8",
						"html_url": "https://github.com/acme/app/releases/tag/v2.0.8",
						"assets": [
							{"name": "duckdb-report-v2.0.8-macos-universal.zip", "browser_download_url": %q, "size": %d},
							{"name": "checksums.txt", "browser_download_url": %q, "size": 128}
						]
					}`,
						server.URL+"/package.zip",
						len(packageData),
						server.URL+"/checksums.txt",
					)
				case "/package.zip":
					writer.Write(packageData)
				case "/checksums.txt":
					fmt.Fprintf(
						writer,
						"%s  duckdb-report-v2.0.8-macos-universal.zip\n",
						checksumValue,
					)
				default:
					http.NotFound(writer, request)
				}
			}))
			defer server.Close()

			checker := NewChecker(Config{
				Repository:     "acme/app",
				CurrentVersion: "2.0.7",
				APIBaseURL:     server.URL,
				Client:         server.Client(),
				GOOS:           "darwin",
				GOARCH:         "arm64",
			})
			release, err := checker.Check(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			destination := t.TempDir()
			var phases []string
			var lastProgress Progress
			download, err := checker.DownloadWithProgress(
				t.Context(),
				release,
				destination,
				func(progress Progress) {
					phases = append(phases, progress.Phase)
					lastProgress = progress
				},
			)
			if test.wantFailure {
				if err == nil {
					t.Fatal("expected checksum verification to fail")
				}
				if _, statErr := os.Stat(
					filepath.Join(destination, "duckdb-report-v2.0.8-macos-universal.zip"),
				); !os.IsNotExist(statErr) {
					t.Fatalf("invalid package was stored: %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if download.Size != int64(len(packageData)) {
				t.Fatalf("download size = %d", download.Size)
			}
			if len(phases) < 4 ||
				phases[0] != ProgressPreparing ||
				phases[len(phases)-2] != ProgressVerifying ||
				phases[len(phases)-1] != ProgressComplete ||
				!slices.Contains(phases, ProgressDownloading) {
				t.Fatalf("progress phases = %#v", phases)
			}
			if lastProgress.Downloaded != int64(len(packageData)) {
				t.Fatalf("progress downloaded = %d", lastProgress.Downloaded)
			}
			stored, err := os.ReadFile(download.Path)
			if err != nil {
				t.Fatal(err)
			}
			if string(stored) != string(packageData) {
				t.Fatalf("stored package = %q", stored)
			}
		})
	}
}

func TestParseSemanticVersion(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "release tag", value: "v2.10.3", want: "2.10.3"},
		{name: "prerelease", value: "2.10.3-beta.1", want: "2.10.3-beta.1"},
		{name: "build metadata", value: "2.10.3+abc", want: "2.10.3"},
		{name: "missing patch", value: "2.10", wantErr: true},
		{name: "invalid number", value: "v2.x.3", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			version, err := parseSemanticVersion(test.value)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected version parsing to fail")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := versionString(version); got != test.want {
				t.Fatalf("versionString() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
		want  int
	}{
		{name: "newer patch", left: "2.0.8", right: "2.0.7", want: 1},
		{name: "newer minor", left: "2.1.0", right: "2.0.9", want: 1},
		{name: "same version", left: "v2.0.7", right: "2.0.7", want: 0},
		{name: "older version", left: "1.9.9", right: "2.0.0", want: -1},
		{name: "stable beats prerelease", left: "2.0.0", right: "2.0.0-beta.1", want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			left, err := parseSemanticVersion(test.left)
			if err != nil {
				t.Fatal(err)
			}
			right, err := parseSemanticVersion(test.right)
			if err != nil {
				t.Fatal(err)
			}
			if got := compareVersions(left, right); got != test.want {
				t.Fatalf("compareVersions() = %d, want %d", got, test.want)
			}
		})
	}
}
