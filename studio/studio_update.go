package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	studioReleaseAPIURL = "https://api.github.com/repos/bryant-w/hobot-code/releases/latest"
	studioUpdateTimeout = 20 * time.Second
	maximumReleaseBytes = 1024 * 1024
)

var studioVersionPattern = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)(?:-([0-9A-Za-z.-]+))?$`)

type StudioUpdateCheck struct {
	Status           string `json:"status"`
	InstalledVersion string `json:"installedVersion"`
	AvailableVersion string `json:"availableVersion,omitempty"`
	Message          string `json:"message"`
	ReleaseURL       string `json:"releaseUrl,omitempty"`
	downloadURL      string
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

type studioVersion struct {
	parts      [3]int
	prerelease bool
}

func (app *App) CheckStudioUpdate() (StudioUpdateCheck, error) {
	base := app.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithTimeout(base, studioUpdateTimeout)
	defer cancel()
	return checkStudioUpdate(ctx, http.DefaultClient, studioReleaseAPIURL, currentStudioVersion(), runtime.GOOS, runtime.GOARCH)
}

// OpenStudioUpdate only opens an asset discovered by a fresh, fixed-origin
// release check. The browser and Gatekeeper retain control of the download and
// installation; arbitrary URLs from the web view are never accepted here.
func (app *App) OpenStudioUpdate() error {
	check, err := app.CheckStudioUpdate()
	if err != nil {
		return err
	}
	if check.Status != "available" || check.downloadURL == "" {
		return fmt.Errorf("no Studio update is available")
	}
	if app.ctx == nil {
		return fmt.Errorf("application is not ready")
	}
	runtimeURL, err := safeExternalURL(check.downloadURL)
	if err != nil {
		return err
	}
	return app.openExternalURL(runtimeURL)
}

func (app *App) openExternalURL(target string) error {
	if app.ctx == nil {
		return fmt.Errorf("application is not ready")
	}
	// Kept behind this small method so update checks remain unit-testable.
	openBrowserURL(app.ctx, target)
	return nil
}

func checkStudioUpdate(ctx context.Context, client *http.Client, endpoint, installed, goos, goarch string) (StudioUpdateCheck, error) {
	result := StudioUpdateCheck{Status: "unsupported", InstalledVersion: installed, Message: "Studio updates are available from GitHub Releases."}
	if goos != "darwin" || goarch != "arm64" {
		return result, nil
	}
	installedVersion, ok := parseStudioVersion(installed)
	if !ok {
		return result, fmt.Errorf("installed Studio version %q is invalid", installed)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return result, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Hobot-Code-Studio/"+installed)
	response, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("check Studio update: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result, fmt.Errorf("check Studio update: release service returned HTTP %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maximumReleaseBytes+1))
	if err != nil {
		return result, fmt.Errorf("check Studio update: read release metadata: %w", err)
	}
	if len(payload) > maximumReleaseBytes {
		return result, fmt.Errorf("check Studio update: release metadata is too large")
	}
	var release githubRelease
	if err := json.Unmarshal(payload, &release); err != nil {
		return result, fmt.Errorf("check Studio update: invalid release metadata: %w", err)
	}
	if release.Draft || release.Prerelease {
		return result, fmt.Errorf("check Studio update: latest release is not stable")
	}
	available, ok := parseStudioVersion(release.TagName)
	if !ok || available.prerelease {
		return result, fmt.Errorf("check Studio update: release version %q is invalid", release.TagName)
	}
	availableText := strings.TrimPrefix(release.TagName, "v")
	comparison := compareStudioVersions(available, installedVersion)
	if comparison < 0 {
		result.Status = "ahead"
		result.AvailableVersion = availableText
		result.Message = fmt.Sprintf("This Studio is newer than the latest public stable release (v%s).", availableText)
		return result, nil
	}
	if comparison == 0 {
		result.Status = "current"
		result.AvailableVersion = availableText
		result.Message = "Studio matches the latest public stable release."
		return result, nil
	}
	result.AvailableVersion = availableText

	dmgName := fmt.Sprintf("hobot-code-%s-macos-arm64.dmg", availableText)
	checksumName := dmgName + ".sha256"
	hasDMG, hasChecksum := false, false
	downloadURL := ""
	for _, asset := range release.Assets {
		switch asset.Name {
		case dmgName:
			hasDMG = strings.HasPrefix(asset.BrowserDownloadURL, "https://github.com/bryant-w/hobot-code/releases/download/")
			if hasDMG {
				downloadURL = asset.BrowserDownloadURL
			}
		case checksumName:
			hasChecksum = strings.HasPrefix(asset.BrowserDownloadURL, "https://github.com/bryant-w/hobot-code/releases/download/")
		}
	}
	if !hasDMG || !hasChecksum {
		return result, fmt.Errorf("check Studio update: release %s is missing its ARM64 DMG or checksum", availableText)
	}
	releaseURL, err := safeExternalURL(release.HTMLURL)
	if err != nil || releaseURL != fmt.Sprintf("https://github.com/bryant-w/hobot-code/releases/tag/v%s", availableText) {
		return result, fmt.Errorf("check Studio update: release page is invalid")
	}
	result.Status = "available"
	result.Message = "A signed macOS update is ready. Downloading it does not interrupt board tasks."
	result.ReleaseURL = releaseURL
	result.downloadURL = downloadURL
	return result, nil
}

func parseStudioVersion(value string) (studioVersion, bool) {
	match := studioVersionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return studioVersion{}, false
	}
	var result studioVersion
	for index := range result.parts {
		part, err := strconv.Atoi(match[index+1])
		if err != nil {
			return studioVersion{}, false
		}
		result.parts[index] = part
	}
	result.prerelease = match[4] != ""
	return result, true
}

func compareStudioVersions(left, right studioVersion) int {
	for index := range left.parts {
		if left.parts[index] < right.parts[index] {
			return -1
		}
		if left.parts[index] > right.parts[index] {
			return 1
		}
	}
	if left.prerelease == right.prerelease {
		return 0
	}
	if left.prerelease {
		return -1
	}
	return 1
}
