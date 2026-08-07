package cli

import (
	"reflect"
	"testing"
)

func TestSplitGlobalArgsAllowsFlagsAfterCommand(t *testing.T) {
	globals, command := splitGlobalArgs([]string{"run", "--message", "hello", "--json", "--config", "base.json", "--board=s600.json"})
	wantGlobals := []string{"--json", "--config", "base.json", "--board=s600.json"}
	wantCommand := []string{"run", "--message", "hello"}
	if !reflect.DeepEqual(globals, wantGlobals) || !reflect.DeepEqual(command, wantCommand) {
		t.Fatalf("globals=%v command=%v", globals, command)
	}
}
