package cli

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Kyrie-w8/aster-edge/internal/core"
)

func TestSplitGlobalArgsAllowsFlagsAfterCommand(t *testing.T) {
	globals, command := splitGlobalArgs([]string{"run", "--message", "hello", "--json", "--config", "base.json", "--board=s600.json"})
	wantGlobals := []string{"--json", "--config", "base.json", "--board=s600.json"}
	wantCommand := []string{"run", "--message", "hello"}
	if !reflect.DeepEqual(globals, wantGlobals) || !reflect.DeepEqual(command, wantCommand) {
		t.Fatalf("globals=%v command=%v", globals, command)
	}
}

func TestChatAliasesAndToggles(t *testing.T) {
	if fields := strings.Fields("/q"); fields[0] != "/q" {
		t.Fatal("/q alias was not parsed")
	}
	if !toggleOption([]string{"/thinking", "on"}, false) {
		t.Fatal("expected explicit on")
	}
	if toggleOption([]string{"/details", "off"}, true) {
		t.Fatal("expected explicit off")
	}
}

func TestApprovalBrokerHonorsCancellation(t *testing.T) {
	broker := newApprovalBroker(false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		done <- broker.Approve(ctx, core.ToolCall{Name: "shell_exec"}, core.ToolDefinition{Risk: "write"})
	}()
	select {
	case <-broker.requests:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("approval request was not delivered")
	}
	select {
	case approved := <-done:
		if approved {
			t.Fatal("cancelled approval was accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("approval did not unblock after cancellation")
	}
}

func TestSessionLocksSerializeOnlyMatchingSessions(t *testing.T) {
	locks := newSessionLocks()
	releaseFirst := locks.acquire("same")
	acquiredSame := make(chan func(), 1)
	go func() { acquiredSame <- locks.acquire("same") }()
	select {
	case release := <-acquiredSame:
		release()
		t.Fatal("matching session was not serialized")
	case <-time.After(25 * time.Millisecond):
	}
	releaseOther := locks.acquire("other")
	releaseOther()
	releaseFirst()
	select {
	case release := <-acquiredSame:
		release()
	case <-time.After(time.Second):
		t.Fatal("matching session did not resume")
	}
}

func TestActiveTurnCancellation(t *testing.T) {
	active := newActiveTurns()
	ctx, cancel := context.WithCancel(context.Background())
	active.set("session", cancel)
	if !active.cancel("session") {
		t.Fatal("active session was not found")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("active session was not cancelled")
	}
	active.clear("session", cancel)
	if active.cancel("session") {
		t.Fatal("cleared session remained active")
	}
}
