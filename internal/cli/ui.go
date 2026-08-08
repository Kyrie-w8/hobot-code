package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Kyrie-w8/aster-edge/internal/app"
)

type terminalTheme struct {
	color, interactive bool
	width              int
	accent, brand, dim string
	green, yellow, red string
	bold, reset        string
}

func newTerminalTheme() terminalTheme {
	theme := terminalTheme{width: terminalWidth()}
	stdout, err := os.Stdout.Stat()
	theme.interactive = err == nil && stdout.Mode()&os.ModeCharDevice != 0 && os.Getenv("TERM") != "dumb"
	theme.color = theme.interactive && os.Getenv("NO_COLOR") == ""
	if theme.color {
		theme.accent = "\033[38;5;44m"
		theme.brand = "\033[38;5;81m"
		theme.dim = "\033[38;5;245m"
		theme.green = "\033[38;5;78m"
		theme.yellow = "\033[38;5;221m"
		theme.red = "\033[38;5;203m"
		theme.bold = "\033[1m"
		theme.reset = "\033[0m"
	}
	return theme
}

func terminalWidth() int {
	width, _ := strconv.Atoi(os.Getenv("COLUMNS"))
	if width < 56 {
		width = 72
	}
	if width > 100 {
		width = 100
	}
	return width
}

func renderBanner(runtime *app.Runtime, theme terminalTheme) {
	inner := theme.width - 2
	title := " ASTER"
	version := "v" + Version + " "
	model := runtime.Config.Provider.Model + " · " + runtime.Config.Provider.Type
	board := strings.ToUpper(runtime.Board.Profile)
	details := fmt.Sprintf("%s  |  %s  |  %d tools", model, board, len(runtime.Registry.Definitions()))
	fmt.Println()
	fmt.Printf("%s╭%s╮%s\n", theme.dim, strings.Repeat("─", inner), theme.reset)
	fmt.Printf("%s│%s%s%s%s│%s\n", theme.dim, theme.reset, theme.brand+theme.bold, padSides(title, version, inner), theme.reset+theme.dim, theme.reset)
	fmt.Printf("%s│%s%s%s%s│%s\n", theme.dim, theme.reset, theme.dim, padRight("  "+fitText(details, inner-2), inner), theme.dim, theme.reset)
	fmt.Printf("%s╰%s╯%s\n", theme.dim, strings.Repeat("─", inner), theme.reset)
}

func renderPrompt(sessionID string, options chatOptions, theme terminalTheme) {
	session := "new session"
	if sessionID != "" {
		session = "session " + shortID(sessionID)
	}
	thinking := "reasoning off"
	if options.showThinking {
		thinking = "reasoning on"
	}
	fmt.Printf("%s%s · %s%s\n%s%s›%s ", theme.dim, session, thinking, theme.reset, theme.accent, theme.bold, theme.reset)
}

func shortID(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[len(value)-8:]
}

func padSides(left, right string, width int) string {
	left = fitText(left, width)
	right = fitText(right, width)
	spaces := width - utf8.RuneCountInString(left) - utf8.RuneCountInString(right)
	if spaces < 1 {
		return fitText(left+" "+right, width)
	}
	return left + strings.Repeat(" ", spaces) + right
}

func padRight(value string, width int) string {
	value = fitText(value, width)
	return value + strings.Repeat(" ", width-utf8.RuneCountInString(value))
}

func fitText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}
