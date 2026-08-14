package hobot

import (
	"context"
	"fmt"
	"os/exec"
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

// CheckBoardUpdate executes one fixed, read-only product command. Callers
// receive a strict structure instead of untrusted remote command output.
func (client *Client) CheckBoardUpdate(ctx context.Context) (BoardUpdateCheck, error) {
	output, err := client.runUpdateCommand(ctx, "hobot update --check")
	if err != nil {
		return BoardUpdateCheck{}, err
	}
	raw := strings.TrimSpace(output)
	if raw == "" || strings.ContainsAny(raw, "\r\x00") {
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

// InstallBoardUpdate executes only the stable, non-downgrading product
// updater. Archive verification, active-work checks, transaction and rollback
// remain enforced by the board-side updater.
func (client *Client) InstallBoardUpdate(ctx context.Context, version string) error {
	if !boardUpdateVersionOnlyPattern.MatchString(version) {
		return fmt.Errorf("board update version is invalid")
	}
	_, err := client.runUpdateCommand(ctx, "hobot update --version "+version)
	return err
}

func (client *Client) runUpdateCommand(ctx context.Context, remoteCommand string) (string, error) {
	command := exec.CommandContext(ctx, client.config.SSHBinary, client.sshArgsFor(remoteCommand)...)
	stdout := &boundedBuffer{maximum: maximumErrorBytes}
	stderr := &boundedBuffer{maximum: maximumErrorBytes}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", fmt.Errorf("board update command failed: %s", detail)
		}
		return "", fmt.Errorf("board update command failed: %w", err)
	}
	return stdout.String(), nil
}
