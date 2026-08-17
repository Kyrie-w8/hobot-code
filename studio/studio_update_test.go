package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCheckStudioUpdateFindsVerifiedMacRelease(t *testing.T) {
	client := releaseClient(t, "0.28.0", true, true)

	check, err := checkStudioUpdate(context.Background(), client, "https://api.example.test/latest", "0.27.0", "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if check.Status != "available" || check.AvailableVersion != "0.28.0" {
		t.Fatalf("unexpected update check: %+v", check)
	}
	if check.ReleaseURL != "https://github.com/bryant-w/hobot-code/releases/tag/v0.28.0" {
		t.Fatalf("unexpected release URL: %q", check.ReleaseURL)
	}
	if check.downloadURL != "https://github.com/bryant-w/hobot-code/releases/download/v0.28.0/hobot-code-0.28.0-macos-arm64.dmg" {
		t.Fatalf("unexpected download URL: %q", check.downloadURL)
	}
}

func TestCheckStudioUpdateRejectsIncompleteRelease(t *testing.T) {
	client := releaseClient(t, "0.28.0", true, false)

	_, err := checkStudioUpdate(context.Background(), client, "https://api.example.test/latest", "0.27.0", "darwin", "arm64")
	if err == nil || !strings.Contains(err.Error(), "missing its ARM64 DMG or checksum") {
		t.Fatalf("incomplete update was accepted: %v", err)
	}
}

func TestCheckStudioUpdateHandlesCurrentPrereleaseAndUnsupportedTarget(t *testing.T) {
	client := releaseClient(t, "0.27.0", true, true)

	current, err := checkStudioUpdate(context.Background(), client, "https://api.example.test/latest", "0.27.0", "darwin", "arm64")
	if err != nil || current.Status != "current" || current.AvailableVersion != "0.27.0" {
		t.Fatalf("current release result = %+v, err = %v", current, err)
	}
	upgrade, err := checkStudioUpdate(context.Background(), client, "https://api.example.test/latest", "0.27.0-beta.1", "darwin", "arm64")
	if err != nil || upgrade.Status != "available" {
		t.Fatalf("stable release should replace prerelease: result = %+v, err = %v", upgrade, err)
	}
	unsupported, err := checkStudioUpdate(context.Background(), client, "https://api.example.test/latest", "0.27.0", "linux", "arm64")
	if err != nil || unsupported.Status != "unsupported" {
		t.Fatalf("unsupported target result = %+v, err = %v", unsupported, err)
	}
}

func TestCheckStudioUpdateDistinguishesBuildAheadOfPublicRelease(t *testing.T) {
	client := releaseClient(t, "0.21.0", true, true)

	check, err := checkStudioUpdate(context.Background(), client, "https://api.example.test/latest", "0.28.0", "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if check.Status != "ahead" || check.AvailableVersion != "0.21.0" {
		t.Fatalf("unexpected ahead result: %+v", check)
	}
	if !strings.Contains(check.Message, "newer than the latest public stable release") {
		t.Fatalf("ahead result is not actionable: %q", check.Message)
	}
}

func TestStudioVersionComparison(t *testing.T) {
	older, ok := parseStudioVersion("v0.27.9")
	if !ok {
		t.Fatal("valid version was rejected")
	}
	newer, _ := parseStudioVersion("0.28.0")
	if compareStudioVersions(newer, older) <= 0 {
		t.Fatal("minor version comparison is incorrect")
	}
	if _, ok := parseStudioVersion("0.28"); ok {
		t.Fatal("incomplete version was accepted")
	}
}

func TestCheckStudioUpdateRejectsOversizedMetadata(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		body := strings.Repeat(" ", maximumReleaseBytes+1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}
	_, err := checkStudioUpdate(context.Background(), client, "https://api.example.test/latest", "0.27.0", "darwin", "arm64")
	if err == nil || !strings.Contains(err.Error(), "metadata is too large") {
		t.Fatalf("oversized metadata was accepted: %v", err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func releaseClient(t *testing.T, version string, includeDMG, includeChecksum bool) *http.Client {
	t.Helper()
	assets := make([]string, 0, 2)
	base := fmt.Sprintf("https://github.com/bryant-w/hobot-code/releases/download/v%s/hobot-code-%s-macos-arm64.dmg", version, version)
	if includeDMG {
		assets = append(assets, fmt.Sprintf(`{"name":"hobot-code-%s-macos-arm64.dmg","browser_download_url":%q}`, version, base))
	}
	if includeChecksum {
		assets = append(assets, fmt.Sprintf(`{"name":"hobot-code-%s-macos-arm64.dmg.sha256","browser_download_url":%q}`, version, base+".sha256"))
	}
	body := fmt.Sprintf(`{"tag_name":"v%s","html_url":"https://github.com/bryant-w/hobot-code/releases/tag/v%s","draft":false,"prerelease":false,"assets":[%s]}`, version, version, strings.Join(assets, ","))
	return &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Accept") != "application/vnd.github+json" || !strings.HasPrefix(request.Header.Get("User-Agent"), "Hobot-Code-Studio/") {
			t.Errorf("missing release request headers: %v", request.Header)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}
}
