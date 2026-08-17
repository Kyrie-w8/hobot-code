package hobot

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type BoardUpdateCheck struct {
	Status           string `json:"status"`
	InstalledVersion string `json:"installedVersion,omitempty"`
	AvailableVersion string `json:"availableVersion"`
	Message          string `json:"message"`
}

var boardUpdateVersionPattern = `[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?`
var boardUpdateVersionOnlyPattern = regexp.MustCompile(`^` + boardUpdateVersionPattern + `$`)
var boardUpdateCurrentPattern = regexp.MustCompile(`^Hobot Code (` + boardUpdateVersionPattern + `) is current\.$`)
var boardUpdateAvailablePattern = regexp.MustCompile(`^Hobot Code (` + boardUpdateVersionPattern + `) is available; installed version is (` + boardUpdateVersionPattern + `)\.$`)
var boardUpdateNotInstalledPattern = regexp.MustCompile(`^Hobot Code (` + boardUpdateVersionPattern + `) is available and is not installed\.$`)
var boardUpdateOlderPattern = regexp.MustCompile(`^Hobot Code release metadata reports (` + boardUpdateVersionPattern + `), older than installed version (` + boardUpdateVersionPattern + `); no update will be applied\.$`)

// CheckBoardUpdate executes one fixed, read-only product command. The caller
// receives a strict structure rather than remote command output.
func (client *Client) CheckBoardUpdate(ctx context.Context) (BoardUpdateCheck, error) {
	output, err := client.runBoardCommand(ctx, "hobot update --check", nil)
	if err != nil {
		return BoardUpdateCheck{}, err
	}
	raw := strings.TrimSpace(string(output))
	if strings.ContainsAny(raw, "\r\x00") {
		return BoardUpdateCheck{}, fmt.Errorf("board returned an unrecognized update status")
	}
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 || len(lines) > 2 {
		return BoardUpdateCheck{}, fmt.Errorf("board returned an unrecognized update status")
	}
	line := lines[0]
	if match := boardUpdateCurrentPattern.FindStringSubmatch(line); match != nil {
		if len(lines) != 1 {
			return BoardUpdateCheck{}, fmt.Errorf("board returned an unrecognized update status")
		}
		return BoardUpdateCheck{Status: "current", InstalledVersion: match[1], AvailableVersion: match[1], Message: "This board is up to date."}, nil
	}
	if match := boardUpdateAvailablePattern.FindStringSubmatch(line); match != nil {
		if len(lines) != 1 {
			return BoardUpdateCheck{}, fmt.Errorf("board returned an unrecognized update status")
		}
		return BoardUpdateCheck{Status: "available", InstalledVersion: match[2], AvailableVersion: match[1], Message: "A verified stable update is available."}, nil
	}
	if match := boardUpdateNotInstalledPattern.FindStringSubmatch(line); match != nil {
		if len(lines) != 1 {
			return BoardUpdateCheck{}, fmt.Errorf("board returned an unrecognized update status")
		}
		return BoardUpdateCheck{Status: "available", AvailableVersion: match[1], Message: "A verified stable release is available."}, nil
	}
	if match := boardUpdateOlderPattern.FindStringSubmatch(line); match != nil {
		if len(lines) == 2 && lines[1] != "The release source may be stale. Use an explicit --version only after verifying the release." {
			return BoardUpdateCheck{}, fmt.Errorf("board returned an unrecognized update status")
		}
		return BoardUpdateCheck{Status: "source-older", InstalledVersion: match[2], AvailableVersion: match[1], Message: "The installed version is newer than the release channel."}, nil
	}
	return BoardUpdateCheck{}, fmt.Errorf("board returned an unrecognized update status")
}

// InstallBoardUpdate executes only the stable, non-downgrading product updater.
// Version selection, archive verification, active-work checks, transaction and
// rollback remain enforced by the board-side updater.
func (client *Client) InstallBoardUpdate(ctx context.Context, version string) error {
	if !boardUpdateVersionOnlyPattern.MatchString(version) {
		return fmt.Errorf("board update version is invalid")
	}
	_, err := client.runBoardCommand(ctx, "hobot update --version "+version, nil)
	return err
}

// InstallBoardService executes the remote installation script on a target board.
// If version is empty or "latest", it downloads and installs the latest verified stable release.
func (client *Client) InstallBoardService(ctx context.Context, version string) error {
	version = strings.TrimSpace(version)
	var command string
	if version != "" && version != "latest" {
		if !boardUpdateVersionOnlyPattern.MatchString(version) {
			return fmt.Errorf("board install version is invalid")
		}
		command = fmt.Sprintf("curl -fsSL https://github.com/bryant-w/hobot-code/releases/latest/download/hobot-install.sh | sh -s -- --version %s", version)
	} else {
		command = "curl -fsSL https://github.com/bryant-w/hobot-code/releases/latest/download/hobot-install.sh | sh"
	}
	_, err := client.runBoardCommand(ctx, command, nil)
	return err
}

// InstallBoardServiceFromArchive streams a local Linux ARM64 release tarball to the board
// and executes its self-contained install script. This works in air-gapped/offline networks.
func (client *Client) InstallBoardServiceFromArchive(ctx context.Context, archive io.Reader) error {
	installCmd := `tmp_dir="/tmp/hobot-install-$$" && mkdir -p "$tmp_dir" && (tar --warning=no-unknown-keyword -xzf - -C "$tmp_dir" 2>/dev/null || tar -xzf - -C "$tmp_dir" 2>/dev/null) && (if [ -f "$tmp_dir/install.sh" ]; then cd "$tmp_dir"; else cd "$tmp_dir"/*; fi && HOBOT_CODE_FORCE_INSTALL=1 ./install.sh) && rm -rf "$tmp_dir"`
	_, err := client.runBoardCommandWithReader(ctx, installCmd, archive)
	return err
}
