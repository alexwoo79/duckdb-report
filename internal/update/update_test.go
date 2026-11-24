package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
			message: "request github release: 502",
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
			download, err := checker.Download(t.Context(), release, destination)
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
