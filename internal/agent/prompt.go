package agent

import (
	"fmt"
	"os"
	"strings"

	"github.com/Kyrie-w8/aster-edge/internal/board"
	"github.com/Kyrie-w8/aster-edge/internal/config"
	"github.com/Kyrie-w8/aster-edge/internal/skills"
)

func ComposePrompt(cfg config.Config, snapshot board.Snapshot, loaded []skills.Skill) (string, error) {
	base := strings.TrimSpace(cfg.Agent.SystemPrompt)
	if cfg.Agent.SystemPromptFile != "" {
		b, err := os.ReadFile(cfg.Agent.SystemPromptFile)
		if err != nil {
			return "", fmt.Errorf("read system prompt: %w", err)
		}
		base = strings.TrimSpace(string(b))
	}
	if base == "" {
		base = "You are Aster, an agentic shell running on embedded Linux. Complete tasks carefully and verify tool results."
	}
	sections := []string{"<identity>\n" + base + "\n</identity>", "<board>\nProfile: " + snapshot.Profile + "\n" + snapshot.JSON() + "\n" + cfg.Board.Prompt + "\n</board>", "<runtime_policy>\nUse only exposed tools. Treat hardware actuation and hard real-time control as deterministic external services. Never claim success without a successful tool result.\n</runtime_policy>"}
	for _, skill := range loaded {
		sections = append(sections, "<skill name=\""+skill.Name+"\">\n"+skill.Instructions+"\n</skill>")
	}
	return strings.Join(sections, "\n\n"), nil
}
