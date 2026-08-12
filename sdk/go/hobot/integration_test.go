package hobot

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestSSHIntegration(t *testing.T) {
	host := os.Getenv("HOBOT_TEST_SSH_HOST")
	if host == "" {
		t.Skip("set HOBOT_TEST_SSH_HOST to run against a board")
	}
	client, err := NewClient(Config{
		Host: host,
		User: valueOrDefault(os.Getenv("HOBOT_TEST_SSH_USER"), "root"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	info, err := client.Ping(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Protocol != ProtocolVersion || info.Capabilities.EventSchema < 2 {
		t.Fatalf("incompatible daemon: %+v", info)
	}
	if _, err := client.Tasks(ctx, false, "", 10); err != nil {
		t.Fatal(err)
	}
	if containsCapability(info.Capabilities.Capabilities, "system.snapshot") {
		snapshot, err := client.SystemSnapshot(ctx)
		if err != nil || snapshot.Board == "" || snapshot.CPUCores < 1 || len(snapshot.BPUCores) == 0 {
			t.Fatalf("invalid system snapshot: %+v err=%v", snapshot, err)
		}
	}
}

func containsCapability(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func valueOrDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
