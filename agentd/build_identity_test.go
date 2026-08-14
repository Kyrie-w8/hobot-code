package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func validBuildInfo(t *testing.T, version, binarySHA256, compatibilitySHA256 string) []byte {
	t.Helper()
	dirty := false
	metadata := packagedBuildInfo{
		SchemaVersion: buildInfoSchema, Version: version, Commit: strings.Repeat("a", 40), Dirty: &dirty,
		BuiltAt: time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC), Target: runtime.GOOS + "-" + runtime.GOARCH,
		AgentdSHA256: binarySHA256,
	}
	metadata.Pi.Version = "0.84.1"
	metadata.Pi.Commit = strings.Repeat("b", 40)
	metadata.Pi.ArchiveSHA256 = strings.Repeat("c", 64)
	metadata.Pi.CompatibilitySHA256 = compatibilitySHA256
	metadata.Tools.FD = "10.4.2"
	metadata.Tools.Ripgrep = "15.2.0"
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestReadBuildIdentityBindsMetadataToBinary(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "agentd")
	metadata := filepath.Join(dir, "BUILD_INFO.json")
	compatibility := filepath.Join(dir, "PI_COMPATIBILITY.json")
	if err := os.WriteFile(binary, []byte("agentd fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	binarySHA256, err := digestRegularFile(binary, maximumAgentdBinaryBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(compatibility, []byte(`{"apiVersion":"hobot.pi-compatibility/v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	compatibilitySHA256, err := digestRegularFile(compatibility, maximumPiContractBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadata, validBuildInfo(t, "0.26.0", binarySHA256, compatibilitySHA256), 0o644); err != nil {
		t.Fatal(err)
	}
	identity := readBuildIdentity(binary, metadata, "0.26.0")
	if identity.Status != "verified" || identity.Commit != strings.Repeat("a", 40) || identity.BinarySHA256 == "" || identity.Dirty == nil || *identity.Dirty || identity.PiVersion != "0.84.1" || identity.PiCompatibilitySHA256 != compatibilitySHA256 {
		t.Fatalf("valid build identity was rejected: %+v", identity)
	}
	if err := os.Remove(compatibility); err != nil {
		t.Fatal(err)
	}
	missingContract := readBuildIdentity(binary, metadata, "0.26.0")
	if missingContract.Status != "invalid" || missingContract.Reason != "pi-compatibility-missing" {
		t.Fatalf("missing Pi compatibility contract was accepted: %+v", missingContract)
	}
	if err := os.WriteFile(compatibility, []byte(`{"apiVersion":"hobot.pi-compatibility/v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("changed binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	changed := readBuildIdentity(binary, metadata, "0.26.0")
	if changed.Status != "invalid" || changed.Reason != "metadata-mismatch" || changed.BinarySHA256 == identity.BinarySHA256 {
		t.Fatalf("metadata was not bound to the executable: before=%+v after=%+v", identity, changed)
	}
}

func TestReadBuildIdentityBindsMetadataToPiContract(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "agentd")
	metadata := filepath.Join(dir, "BUILD_INFO.json")
	compatibility := filepath.Join(dir, "PI_COMPATIBILITY.json")
	if err := os.WriteFile(binary, []byte("agentd fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(compatibility, []byte(`{"apiVersion":"hobot.pi-compatibility/v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	binarySHA256, err := digestRegularFile(binary, maximumAgentdBinaryBytes)
	if err != nil {
		t.Fatal(err)
	}
	compatibilitySHA256, err := digestRegularFile(compatibility, maximumPiContractBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadata, validBuildInfo(t, "0.26.0", binarySHA256, compatibilitySHA256), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(compatibility, []byte(`{"apiVersion":"tampered"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	identity := readBuildIdentity(binary, metadata, "0.26.0")
	if identity.Status != "invalid" || identity.Reason != "metadata-mismatch" {
		t.Fatalf("mutated Pi compatibility contract was accepted: %+v", identity)
	}
	if err := os.Chmod(compatibility, 0o666); err != nil {
		t.Fatal(err)
	}
	identity = readBuildIdentity(binary, metadata, "0.26.0")
	if identity.Status != "invalid" || identity.Reason != "pi-compatibility-invalid" {
		t.Fatalf("writable Pi compatibility contract was accepted: %+v", identity)
	}
}

func TestReadBuildIdentityRejectsUntrustedMetadata(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "agentd")
	metadata := filepath.Join(dir, "BUILD_INFO.json")
	compatibility := filepath.Join(dir, "PI_COMPATIBILITY.json")
	if err := os.WriteFile(binary, []byte("agentd fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if identity := readBuildIdentity(binary, metadata, "0.26.0"); identity.Status != "unavailable" || identity.Reason != "metadata-missing" {
		t.Fatalf("missing metadata was not reported: %+v", identity)
	}
	binarySHA256, err := digestRegularFile(binary, maximumAgentdBinaryBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(compatibility, []byte(`{"apiVersion":"hobot.pi-compatibility/v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	compatibilitySHA256, err := digestRegularFile(compatibility, maximumPiContractBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadata, validBuildInfo(t, "0.25.0", binarySHA256, compatibilitySHA256), 0o644); err != nil {
		t.Fatal(err)
	}
	if identity := readBuildIdentity(binary, metadata, "0.26.0"); identity.Status != "invalid" || identity.Reason != "metadata-mismatch" {
		t.Fatalf("mismatched metadata was accepted: %+v", identity)
	}
	if err := os.Chmod(metadata, 0o666); err != nil {
		t.Fatal(err)
	}
	if identity := readBuildIdentity(binary, metadata, "0.25.0"); identity.Status != "invalid" || identity.Reason != "metadata-invalid" {
		t.Fatalf("writable metadata was accepted: %+v", identity)
	}
}
