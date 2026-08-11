package hobot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fakeSSH(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	starts := filepath.Join(dir, "starts.log")
	script := filepath.Join(dir, "ssh")
	content := fmt.Sprintf(`#!/bin/sh
printf 'start\n' >> %q
while IFS= read -r line; do
  id=$(printf '%%s\n' "$line" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
  case "$line" in
    *'"method":"capabilities"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"protocolMin":1,"protocolMax":1,"eventSchema":2,"capabilities":["bridge.stdio"],"maximumRequestBytes":2097152,"maximumResponseBytes":8388608}}\n' "$id"
      ;;
    *'"method":"task.page"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"tasks":[{"id":"00112233445566778899aabb","name":"build","cwd":"/root","status":"idle","createdAt":"2026-08-11T00:00:00Z","updatedAt":"2026-08-11T00:00:01Z","lastSequence":7}]}}\n' "$id"
      ;;
    *'"method":"task.restart"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"id":"00112233445566778899aabb","name":"build","cwd":"/root","status":"starting","createdAt":"2026-08-11T00:00:00Z","updatedAt":"2026-08-11T00:00:02Z","lastSequence":7,"restartCount":1}}\n' "$id"
      ;;
    *'"method":"task.subscribe"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"replayed":0,"following":true}}\n' "$id"
      printf '%%s\n' '{"protocol":1,"kind":"event","taskId":"00112233445566778899aabb","sequence":8,"time":"2026-08-11T00:00:02Z","event":{"type":"agent_settled"},"normalized":{"schema":2,"type":"task.idle"}}'
      exit 0
      ;;
    *)
      printf '{"protocol":1,"id":"%%s","ok":false,"error":{"code":"method_not_found","message":"unsupported"}}\n' "$id"
      ;;
  esac
done
`, starts)
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return script, starts
}

func TestClientReusesControlBridgeAndDecodesTasks(t *testing.T) {
	ssh, starts := fakeSSH(t)
	client, err := NewClient(Config{Host: "10.0.0.2", User: "root", SSHBinary: ssh})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	capabilities, err := client.GetCapabilities(ctx)
	if err != nil || capabilities.EventSchema != 2 {
		t.Fatalf("capabilities=%+v err=%v", capabilities, err)
	}
	page, err := client.Tasks(ctx, false, "", 50)
	if err != nil || len(page.Tasks) != 1 || page.Tasks[0].Name != "build" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	restarted, err := client.RestartTask(ctx, page.Tasks[0].ID, "start fresh")
	if err != nil || restarted.RestartCount != 1 || restarted.Status != "starting" {
		t.Fatalf("restarted=%+v err=%v", restarted, err)
	}
	content, err := os.ReadFile(starts)
	if err != nil || string(content) != "start\n" {
		t.Fatalf("control bridge was not reused: %q err=%v", content, err)
	}
}

func TestSubscriptionUsesDedicatedBridge(t *testing.T) {
	ssh, _ := fakeSSH(t)
	client, err := NewClient(Config{Host: "rdk.local", SSHBinary: ssh})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var received Event
	err = client.Subscribe(ctx, "00112233445566778899aabb", 7, func(event Event) error {
		received = event
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if received.Sequence != 8 || received.Normalized == nil || received.Normalized.Type != "task.idle" {
		t.Fatalf("unexpected event: %+v", received)
	}
}

func TestConnectionValidationFailsClosed(t *testing.T) {
	ssh, _ := fakeSSH(t)
	for _, config := range []Config{
		{Host: "-oProxyCommand=bad", SSHBinary: ssh},
		{Host: "host name", SSHBinary: ssh},
		{Host: "board", User: "root@other", SSHBinary: ssh},
		{Host: "board", Port: 70000, SSHBinary: ssh},
		{Host: "board", HostKeyPolicy: "off", SSHBinary: ssh},
	} {
		if _, err := NewClient(config); err == nil {
			t.Fatalf("unsafe config was accepted: %+v", config)
		}
	}
}
