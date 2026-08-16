package overview

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type TextOptions struct{ Color bool }

func WriteJSON(w io.Writer, result Result) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(result)
}

// WriteJSONL emits semantic overview facts and never emits raw routes or URLs.
func WriteJSONL(w io.Writer, result Result) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	for _, item := range result.Behavior {
		if err := encoder.Encode(struct {
			Kind string `json:"kind"`
			Insight
		}{"application_behavior", item}); err != nil {
			return err
		}
	}
	for _, item := range result.Authentication {
		if err := encoder.Encode(struct {
			Kind string `json:"kind"`
			Insight
		}{"authentication", item}); err != nil {
			return err
		}
	}
	for _, item := range result.Storage {
		if err := encoder.Encode(struct {
			Kind string `json:"kind"`
			StorageOperation
		}{"storage", item}); err != nil {
			return err
		}
	}
	if result.Messaging != nil {
		if err := encoder.Encode(struct {
			Kind string `json:"kind"`
			*MessageBoundary
		}{"cross_window_messaging", result.Messaging}); err != nil {
			return err
		}
	}
	for _, item := range result.ReviewTargets {
		if err := encoder.Encode(struct {
			Kind string `json:"kind"`
			ReviewTarget
		}{"review_target", item}); err != nil {
			return err
		}
	}
	return nil
}

func WriteText(w io.Writer, result Result, opts TextOptions) error {
	style := terminalStyle{enabled: opts.Color}
	if err := writeBanner(w, style); err != nil {
		return err
	}

	profile := make([]string, 0, 5)
	if len(result.Frameworks) > 0 {
		profile = append(profile, "Framework  "+strings.Join(result.Frameworks, ", "))
	}
	if len(result.Bundlers) > 0 {
		profile = append(profile, "Bundler    "+strings.Join(result.Bundlers, ", "))
	}
	profile = append(profile, fmt.Sprintf("Input      %d %s · %s", result.Sources, plural(result.Sources, "source"), humanBytes(result.Bytes)))
	if result.Minified {
		profile = append(profile, "Build      Production/minified bundle")
	}
	if result.SourceMap {
		profile = append(profile, "Source map Reference detected")
	}
	if err := simpleSection(w, style, "APPLICATION PROFILE", profile); err != nil {
		return err
	}
	if err := simpleSection(w, style, "AT A GLANCE", result.AtAGlance); err != nil {
		return err
	}
	if err := insightSection(w, style, "APPLICATION BEHAVIOR", result.Behavior); err != nil {
		return err
	}
	if err := insightSection(w, style, "AUTHENTICATION & SESSION", result.Authentication); err != nil {
		return err
	}

	storage := make([]string, 0, len(result.Storage))
	for _, item := range result.Storage {
		storage = append(storage, fmt.Sprintf("%s[%s]  ·  %s", item.Store, item.Key, item.Action))
	}
	if err := simpleSection(w, style, "CLIENT STORAGE", storage); err != nil {
		return err
	}
	if err := messagingSection(w, style, result.Messaging); err != nil {
		return err
	}
	if err := simpleSection(w, style, "BROWSER TRUST BOUNDARIES", result.Capabilities); err != nil {
		return err
	}
	if err := simpleSection(w, style, "THIRD-PARTY SERVICES", result.ThirdParties); err != nil {
		return err
	}
	return reviewSection(w, style, result.ReviewTargets)
}

func writeBanner(w io.Writer, style terminalStyle) error {
	lines := []string{
		"╭──────────────────────────────────────────────────────────╮",
		"│  J S H O U N D                                           │",
		"│  JAVASCRIPT ATTACK-SURFACE OVERVIEW                       │",
		"╰──────────────────────────────────────────────────────────╯",
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	for i, line := range lines {
		if i == 1 {
			line = style.boldCyan(line)
		} else {
			line = style.cyan(line)
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func simpleSection(w io.Writer, style terminalStyle, title string, items []string) error {
	if len(items) == 0 {
		return nil
	}
	if err := sectionTitle(w, style, title); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := fmt.Fprintf(w, "  %s %s\n", style.cyan("◆"), item); err != nil {
			return err
		}
	}
	return nil
}

func insightSection(w io.Writer, style terminalStyle, title string, items []Insight) error {
	if len(items) == 0 {
		return nil
	}
	if err := sectionTitle(w, style, title); err != nil {
		return err
	}
	for _, item := range items {
		meta := fmt.Sprintf("[observed · %d %s]", item.Evidence, plural(item.Evidence, "signal"))
		if _, err := fmt.Fprintf(w, "  %s %s %s\n", style.cyan("◆"), style.bold(item.Title), style.dim(meta)); err != nil {
			return err
		}
		if item.Detail != "" {
			if _, err := fmt.Fprintf(w, "    %s\n", style.dim(item.Detail)); err != nil {
				return err
			}
		}
	}
	return nil
}

func reviewSection(w io.Writer, style terminalStyle, items []ReviewTarget) error {
	if len(items) == 0 {
		return nil
	}
	if err := sectionTitle(w, style, "PRIORITIZED REVIEW TARGETS"); err != nil {
		return err
	}
	for index, item := range items {
		top := style.cyan("╭─")
		rail := style.cyan("│")
		bottom := style.cyan("╰" + strings.Repeat("─", 56))
		if _, err := fmt.Fprintf(w, "  %s %s  %s\n", top, style.boldCyan(fmt.Sprintf("%02d", index+1)), style.bold(item.Title)); err != nil {
			return err
		}
		meta := fmt.Sprintf("%s · %d %s", strings.ToUpper(item.Status), item.Evidence, plural(item.Evidence, "signal"))
		if _, err := fmt.Fprintf(w, "  %s    %s\n  %s\n", rail, style.status(item.Status, meta), rail); err != nil {
			return err
		}
		for _, line := range wrapText(item.Why, 72) {
			if _, err := fmt.Fprintf(w, "  %s  %s\n", rail, line); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "  %s\n", bottom); err != nil {
			return err
		}
	}
	return nil
}

func messagingSection(w io.Writer, style terminalStyle, item *MessageBoundary) error {
	if item == nil {
		return nil
	}
	if err := sectionTitle(w, style, "CROSS-WINDOW MESSAGING"); err != nil {
		return err
	}
	if item.Receivers > 0 {
		if _, err := fmt.Fprintf(w, "  %s %s\n", style.cyan("◆"), style.bold(fmt.Sprintf("%d receiver %s", item.Receivers, plural(item.Receivers, "handler")))); err != nil {
			return err
		}
		origin := style.status("potential", "not detected")
		if item.OriginChecks > 0 {
			origin = style.status("observed", fmt.Sprintf("detected · %d %s", item.OriginChecks, plural(item.OriginChecks, "check")))
		}
		source := style.dim("not detected")
		if item.SourceChecks > 0 {
			source = style.status("observed", fmt.Sprintf("detected · %d %s", item.SourceChecks, plural(item.SourceChecks, "check")))
		}
		if _, err := fmt.Fprintf(w, "    Origin validation  %s\n    Source validation  %s\n", origin, source); err != nil {
			return err
		}
		for _, validatedOrigin := range item.ValidatedOrigins {
			if _, err := fmt.Fprintf(w, "      %s compared with %s\n", style.cyan("↳"), validatedOrigin); err != nil {
				return err
			}
		}
	}
	if item.Senders > 0 {
		if _, err := fmt.Fprintf(w, "  %s %s\n", style.cyan("◆"), style.bold(fmt.Sprintf("%d sender %s", item.Senders, plural(item.Senders, "call")))); err != nil {
			return err
		}
		if item.FixedTargets > 0 {
			if _, err := fmt.Fprintf(w, "    Fixed targetOrigin  %d\n", item.FixedTargets); err != nil {
				return err
			}
		}
		for _, origin := range item.FixedTargetOrigins {
			if _, err := fmt.Fprintf(w, "      %s %s\n", style.cyan("↳"), origin); err != nil {
				return err
			}
		}
		if item.WildcardTargets > 0 {
			if _, err := fmt.Fprintf(w, "    Wildcard targetOrigin  %s\n", style.status("potential", fmt.Sprintf("%d", item.WildcardTargets))); err != nil {
				return err
			}
		}
		if item.DynamicTargets > 0 {
			if _, err := fmt.Fprintf(w, "    Dynamic targetOrigin   %d\n", item.DynamicTargets); err != nil {
				return err
			}
		}
	}
	return nil
}

func sectionTitle(w io.Writer, style terminalStyle, title string) error {
	_, err := fmt.Fprintf(w, "\n%s\n%s\n", style.boldCyan(title), style.dim("  "+strings.Repeat("─", min(54, len(title)+12))))
	return err
}

func humanBytes(value int) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	if value < 1024*1024 {
		return fmt.Sprintf("%.1f KiB", float64(value)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(value)/(1024*1024))
}

func wrapText(value string, width int) []string {
	words := strings.Fields(value)
	if len(words) == 0 {
		return nil
	}
	lines := make([]string, 0, len(words)/8+1)
	line := words[0]
	for _, word := range words[1:] {
		if len(line)+1+len(word) > width {
			lines = append(lines, line)
			line = word
			continue
		}
		line += " " + word
	}
	return append(lines, line)
}

type terminalStyle struct{ enabled bool }

func (s terminalStyle) wrap(code, value string) string {
	if !s.enabled {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}
func (s terminalStyle) bold(value string) string     { return s.wrap("1", value) }
func (s terminalStyle) dim(value string) string      { return s.wrap("2", value) }
func (s terminalStyle) cyan(value string) string     { return s.wrap("36", value) }
func (s terminalStyle) boldCyan(value string) string { return s.wrap("1;36", value) }
func (s terminalStyle) status(status, value string) string {
	if status == "observed" {
		return s.wrap("32", value)
	}
	return s.wrap("33", value)
}
