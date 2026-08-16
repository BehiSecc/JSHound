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
	if err := simpleSection(w, style, "BROWSER TRUST BOUNDARIES", result.Capabilities); err != nil {
		return err
	}
	if err := simpleSection(w, style, "THIRD-PARTY SERVICES", result.ThirdParties); err != nil {
		return err
	}
	if err := reviewSection(w, style, result.ReviewTargets); err != nil {
		return err
	}
	return notesSection(w, style, result.Limitations)
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
		meta := fmt.Sprintf("[observed · %s · %d %s]", item.Confidence, item.Evidence, plural(item.Evidence, "signal"))
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
	for _, item := range items {
		badge := style.priority(item.Priority, "["+strings.ToUpper(item.Priority)+"]")
		meta := style.dim(fmt.Sprintf("[%s · %s confidence · %d %s]", item.Status, item.Confidence, item.Evidence, plural(item.Evidence, "signal")))
		if _, err := fmt.Fprintf(w, "  %s %s %s\n", badge, style.bold(item.Title), meta); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "    %s\n", item.Why); err != nil {
			return err
		}
	}
	return nil
}

func notesSection(w io.Writer, style terminalStyle, items []string) error {
	if len(items) == 0 {
		return nil
	}
	if err := sectionTitle(w, style, "ANALYSIS NOTES"); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := fmt.Fprintf(w, "  %s %s\n", style.dim("•"), style.dim(item)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
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
func (s terminalStyle) priority(priority, value string) string {
	if priority == "high" {
		return s.wrap("1;31", value)
	}
	return s.wrap("1;33", value)
}
