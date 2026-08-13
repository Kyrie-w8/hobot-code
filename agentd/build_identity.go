package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

const (
	buildInfoSchema          = 2
	maximumBuildInfoBytes    = int64(64 * 1024)
	maximumAgentdBinaryBytes = int64(128 * 1024 * 1024)
)

type buildIdentity struct {
	Status       string    `json:"status"`
	Reason       string    `json:"reason,omitempty"`
	Commit       string    `json:"commit,omitempty"`
	Dirty        *bool     `json:"dirty,omitempty"`
	BuiltAt      time.Time `json:"builtAt,omitempty"`
	Target       string    `json:"target,omitempty"`
	BinarySHA256 string    `json:"binarySha256,omitempty"`
	PiVersion    string    `json:"piVersion,omitempty"`
	PiCommit     string    `json:"piCommit,omitempty"`
}

type packagedBuildInfo struct {
	SchemaVersion int       `json:"schemaVersion"`
	Version       string    `json:"version"`
	Commit        string    `json:"commit"`
	Dirty         *bool     `json:"dirty"`
	BuiltAt       time.Time `json:"builtAt"`
	Target        string    `json:"target"`
	AgentdSHA256  string    `json:"agentdSha256"`
	Pi            struct {
		Version       string `json:"version"`
		Commit        string `json:"commit"`
		ArchiveSHA256 string `json:"archiveSha256"`
	} `json:"pi"`
	Tools struct {
		FD      string `json:"fd"`
		Ripgrep string `json:"ripgrep"`
	} `json:"tools"`
}

func currentBuildIdentity() buildIdentity {
	executable, err := os.Executable()
	if err != nil {
		return buildIdentity{Status: "unavailable", Reason: "executable-unavailable"}
	}
	physical, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return buildIdentity{Status: "unavailable", Reason: "executable-unavailable"}
	}
	return readBuildIdentity(physical, filepath.Join(filepath.Dir(physical), "BUILD_INFO.json"), version)
}

func readBuildIdentity(executable, metadataPath, expectedVersion string) buildIdentity {
	result := buildIdentity{Status: "unavailable"}
	if digest, err := digestRegularFile(executable, maximumAgentdBinaryBytes); err == nil {
		result.BinarySHA256 = digest
	} else {
		result.Reason = "binary-unavailable"
		return result
	}
	content, err := readPublicBuildInfo(metadataPath)
	if errors.Is(err, os.ErrNotExist) {
		result.Reason = "metadata-missing"
		return result
	}
	if err != nil {
		result.Status = "invalid"
		result.Reason = "metadata-invalid"
		return result
	}
	var metadata packagedBuildInfo
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&metadata) != nil || decoder.Decode(&struct{}{}) != io.EOF || metadata.Dirty == nil {
		result.Status = "invalid"
		result.Reason = "metadata-invalid"
		return result
	}
	if metadata.SchemaVersion != buildInfoSchema || metadata.Version != expectedVersion || !isLowerHex(metadata.Commit, 40) ||
		metadata.BuiltAt.IsZero() || metadata.Target != runtime.GOOS+"-"+runtime.GOARCH || metadata.AgentdSHA256 != result.BinarySHA256 || metadata.Pi.Version == "" || !isLowerHex(metadata.Pi.Commit, 40) ||
		!isLowerHex(metadata.Pi.ArchiveSHA256, 64) || metadata.Tools.FD == "" || metadata.Tools.Ripgrep == "" {
		result.Status = "invalid"
		result.Reason = "metadata-mismatch"
		return result
	}
	result.Status = "verified"
	result.Reason = ""
	result.Commit = metadata.Commit
	dirty := *metadata.Dirty
	result.Dirty = &dirty
	result.BuiltAt = metadata.BuiltAt.UTC()
	result.Target = metadata.Target
	result.PiVersion = metadata.Pi.Version
	result.PiCommit = metadata.Pi.Commit
	return result
}

func readPublicBuildInfo(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumBuildInfoBytes || info.Mode().Perm()&0o022 != 0 {
		return nil, errors.New("build metadata is not a trusted regular file")
	}
	if owner, ok := fileOwner(info); ok && owner != 0 && owner != os.Getuid() {
		return nil, errors.New("build metadata has an unexpected owner")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("build metadata changed while it was opened")
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumBuildInfoBytes+1))
	if err != nil || int64(len(content)) != info.Size() {
		return nil, errors.New("build metadata changed while it was read")
	}
	after, err := file.Stat()
	if err != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return nil, errors.New("build metadata changed while it was read")
	}
	return content, nil
}

func digestRegularFile(path string, maximum int64) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum || info.Mode().Perm()&0o022 != 0 {
		return "", errors.New("binary is not a bounded regular file")
	}
	if owner, ok := fileOwner(info); ok && owner != 0 && owner != os.Getuid() {
		return "", errors.New("binary has an unexpected owner")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", errors.New("binary changed while it was opened")
	}
	digest := sha256.New()
	if copied, err := io.Copy(digest, io.LimitReader(file, maximum+1)); err != nil || copied != info.Size() {
		return "", errors.New("binary digest failed")
	}
	after, err := file.Stat()
	if err != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return "", errors.New("binary changed while it was read")
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func isLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
