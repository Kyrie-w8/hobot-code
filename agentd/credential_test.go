package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestEnvironmentWithoutGatewayCredential(t *testing.T) {
	result := environmentWithoutGatewayCredential([]string{
		"PATH=/usr/bin", gatewayTokenEnvironment + "=secret", providerTokenPrefix + "ACME=provider-secret", gatewayTokenFDEnvironment + "=3", gatewayTokenFileEnvironment + "=/private/token", modelEgressSocketEnv + "=/spoofed.sock", modelEgressProvidersEnv + "=drobotics", "LANG=C.UTF-8",
	})
	joined := strings.Join(result, "\n")
	if strings.Contains(joined, "secret") || strings.Contains(joined, gatewayTokenEnvironment) || strings.Contains(joined, providerTokenPrefix) || strings.Contains(joined, gatewayTokenFDEnvironment) || strings.Contains(joined, gatewayTokenFileEnvironment) || strings.Contains(joined, modelEgressSocketEnv) || strings.Contains(joined, modelEgressProvidersEnv) {
		t.Fatalf("gateway credential remained in child environment: %v", result)
	}
	if !strings.Contains(joined, "PATH=/usr/bin") || !strings.Contains(joined, "LANG=C.UTF-8") {
		t.Fatalf("non-secret environment was removed: %v", result)
	}
}

func TestManagedProviderCredentialBundleRoundTrip(t *testing.T) {
	bundle, err := ambientGatewayCredentials([]string{
		gatewayTokenEnvironment + "=drobotics-secret",
		providerTokenPrefix + "ACME=acme-secret",
		providerTokenPrefix + "OPENAI_LAB=lab-secret",
		"UNRELATED=visible",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := encodeGatewayCredentialBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "UNRELATED") {
		t.Fatalf("unrelated environment entered credential bundle: %s", payload)
	}
	decoded, err := decodeGatewayCredentialBundle([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.DRobotics != "drobotics-secret" || decoded.ProviderKeys[providerTokenPrefix+"ACME"] != "acme-secret" || decoded.ProviderKeys[providerTokenPrefix+"OPENAI_LAB"] != "lab-secret" {
		t.Fatalf("credential bundle changed: %+v", decoded)
	}
}

func TestManagedProviderCredentialBundleRejectsUnsafeInput(t *testing.T) {
	for _, environment := range [][]string{
		{providerTokenPrefix + "bad=secret"},
		{providerTokenPrefix + "=secret"},
		{providerTokenPrefix + "ACME=" + strings.Repeat("x", maximumGatewayTokenBytes+1)},
	} {
		if _, err := ambientGatewayCredentials(environment); err == nil {
			t.Fatalf("unsafe credential environment accepted: %q", environment[0])
		}
	}
	for _, payload := range []string{
		`{"schemaVersion":1,"schemaVersion":1}`,
		`{"schemaVersion":2}`,
		`{"schemaVersion":1,"unknown":"secret"}`,
		`{"schemaVersion":1,"providerKeys":{"HOBOT_CODE_PROVIDER_KEY_bad":"secret"}}`,
	} {
		if _, err := decodeGatewayCredentialBundle([]byte(payload)); err == nil {
			t.Fatalf("unsafe credential bundle accepted: %s", payload)
		}
	}
}

func TestLegacyGatewayCredentialPayloadRemainsCompatible(t *testing.T) {
	bundle, err := decodeGatewayCredentialBundle([]byte("legacy-secret"))
	if err != nil || bundle.DRobotics != "legacy-secret" || len(bundle.ProviderKeys) != 0 {
		t.Fatalf("legacy payload = %+v, %v", bundle, err)
	}
}

func TestLoadGatewayCredentialsCapturesAndClearsManagedEnvironment(t *testing.T) {
	t.Setenv(gatewayTokenEnvironment, "drobotics-secret")
	t.Setenv(providerTokenPrefix+"ACME", "acme-secret")
	bundle, err := loadGatewayCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if bundle.DRobotics != "drobotics-secret" || bundle.ProviderKeys[providerTokenPrefix+"ACME"] != "acme-secret" {
		t.Fatalf("credentials were not captured: %+v", bundle)
	}
	if os.Getenv(gatewayTokenEnvironment) != "" || os.Getenv(providerTokenPrefix+"ACME") != "" {
		t.Fatal("managed credentials remained in the process environment")
	}
}

func TestGatewayCredentialPipeTransport(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", `value=$(cat <&"$HOBOT_CODE_GATEWAY_TOKEN_FD"); printf 'value=%s\n' "$value"; env`)
	command.Env = []string{"PATH=/usr/bin:/bin", gatewayTokenEnvironment + "=must-not-leak"}
	closeCredential, err := attachGatewayCredential(command, "pipe-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer closeCredential()
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	if !strings.Contains(text, "value=pipe-secret") || strings.Contains(text, "must-not-leak") || strings.Contains(text, gatewayTokenEnvironment+"=") {
		t.Fatalf("unexpected credential transport output: %q", text)
	}
}

func TestLoadGatewayCredentialPrefersFDAndClearsEnvironment(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString("fd-secret"); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	t.Setenv(gatewayTokenEnvironment, "ambient-secret")
	t.Setenv(gatewayTokenFDEnvironment, fmt.Sprintf("%d", reader.Fd()))
	value, err := loadGatewayCredential()
	// loadGatewayCredential owns and closes an inherited descriptor. Mark the
	// test-side wrapper closed as well so its finalizer cannot close a reused FD.
	_ = reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if value != "fd-secret" || os.Getenv(gatewayTokenEnvironment) != "" || os.Getenv(gatewayTokenFDEnvironment) != "" {
		t.Fatalf("credential was not isolated: value=%q ambient=%q fd=%q", value, os.Getenv(gatewayTokenEnvironment), os.Getenv(gatewayTokenFDEnvironment))
	}
}
