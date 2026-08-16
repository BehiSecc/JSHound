package overview

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
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

	if err := storageSection(w, style, result.Storage); err != nil {
		return err
	}
	if err := messagingSection(w, style, result.Messaging); err != nil {
		return err
	}
	if err := simpleSection(w, style, "BROWSER TRUST BOUNDARIES", result.Capabilities); err != nil {
		return err
	}
	if err := simpleSection(w, style, "INTEGRATIONS & SERVICES", result.ThirdParties); err != nil {
		return err
	}
	return reviewSection(w, style, result.ReviewTargets)
}

func writeBanner(w io.Writer, style terminalStyle) error {
	lines := []string{
		`       _ ____  _   _  ___  _   _ _   _ ____`,
		`      | / ___|| | | |/ _ \| | | | \ | |  _ \`,
		`   _  | \___ \| |_| | | | | | | |  \| | | | |`,
		`  | |_| |___) |  _  | |_| | |_| | |\  | |_| |`,
		`   \___/|____/|_| |_|\___/ \___/|_| \_|____/`,
		``,
		`  JSHOUND · JAVASCRIPT ATTACK-SURFACE RECON`,
		`  STATIC ANALYSIS · NO CODE EXECUTION`,
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	for i, line := range lines {
		if i <= 4 || i == 6 {
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
		if _, err := fmt.Fprintf(w, "  %s %s\n", style.cyan("◆"), style.bold(fmt.Sprintf("%d confirmed Window %s", item.Receivers, plural(item.Receivers, "receiver")))); err != nil {
			return err
		}
		coverage := []struct {
			label  string
			count  int
			status string
		}{
			{"origin + source validation", item.OriginAndSource, "observed"}, {"origin validation only", item.OriginOnly, "observed"},
			{"source validation only", item.SourceOnly, "potential"}, {"weak origin matching", item.WeakOrigin, "potential"},
			{"no recognizable validation", item.Unvalidated, "potential"}, {"validation unresolved", item.ValidationUnknown, ""},
		}
		for _, row := range coverage {
			if row.count == 0 {
				continue
			}
			value := fmt.Sprintf("%d", row.count)
			if row.status != "" {
				value = style.status(row.status, value)
			} else {
				value = style.dim(value)
			}
			if _, err := fmt.Fprintf(w, "    %-29s %s\n", row.label, value); err != nil {
				return err
			}
		}
		for _, validatedOrigin := range item.ValidatedOrigins {
			if _, err := fmt.Fprintf(w, "      %s compared with %s\n", style.cyan("↳"), validatedOrigin); err != nil {
				return err
			}
		}
	}
	if item.UnknownReceivers > 0 {
		if _, err := fmt.Fprintf(w, "  %s %d non-Window or unresolved message %s excluded\n", style.dim("◇"), item.UnknownReceivers, plural(item.UnknownReceivers, "listener")); err != nil {
			return err
		}
	}
	if item.Senders > 0 {
		if _, err := fmt.Fprintf(w, "  %s %s\n", style.cyan("◆"), style.bold(fmt.Sprintf("%d parsed postMessage %s", item.Senders, plural(item.Senders, "call")))); err != nil {
			return err
		}
		for _, row := range []struct {
			label string
			count int
		}{{"Window", item.WindowSenders}, {"Worker", item.WorkerSenders}, {"MessagePort", item.PortSenders}, {"BroadcastChannel", item.BroadcastSenders}, {"unresolved/local", item.UnknownSenders}} {
			if row.count > 0 {
				if _, err := fmt.Fprintf(w, "    %-20s %d\n", row.label, row.count); err != nil {
					return err
				}
			}
		}
		if item.FixedTargets > 0 {
			if _, err := fmt.Fprintf(w, "    Window fixed targetOrigin    %d\n", item.FixedTargets); err != nil {
				return err
			}
		}
		for _, origin := range item.FixedTargetOrigins {
			if _, err := fmt.Fprintf(w, "      %s %s\n", style.cyan("↳"), origin); err != nil {
				return err
			}
		}
		if item.WildcardTargets > 0 {
			if _, err := fmt.Fprintf(w, "    Window wildcard targetOrigin %s\n", style.status("potential", fmt.Sprintf("%d", item.WildcardTargets))); err != nil {
				return err
			}
		}
		if item.DynamicTargets > 0 {
			if _, err := fmt.Fprintf(w, "    Window dynamic targetOrigin  %d\n", item.DynamicTargets); err != nil {
				return err
			}
		}
	}
	return nil
}

type groupedStorage struct {
	store   string
	key     string
	actions map[string]struct{}
}

func storageSection(w io.Writer, style terminalStyle, items []StorageOperation) error {
	if len(items) == 0 {
		return nil
	}
	groups := make(map[string]*groupedStorage)
	for _, item := range items {
		key := item.Store + "\x00" + item.Key
		group := groups[key]
		if group == nil {
			group = &groupedStorage{store: item.Store, key: item.Key, actions: stringSet()}
			groups[key] = group
		}
		if item.Action == "read/write" {
			group.actions["read"] = struct{}{}
			group.actions["write"] = struct{}{}
		} else {
			group.actions[item.Action] = struct{}{}
		}
	}
	categories := map[string][]*groupedStorage{"SECURITY / AUTH": {}, "DEBUG / FEATURE FLAGS": {}, "APPLICATION STATE": {}}
	probeEntries, probeOperations := 0, 0
	for _, group := range groups {
		switch storageCategory(group.key) {
		case "probe":
			probeEntries++
			probeOperations += len(group.actions)
		case "security":
			categories["SECURITY / AUTH"] = append(categories["SECURITY / AUTH"], group)
		case "debug":
			categories["DEBUG / FEATURE FLAGS"] = append(categories["DEBUG / FEATURE FLAGS"], group)
		default:
			categories["APPLICATION STATE"] = append(categories["APPLICATION STATE"], group)
		}
	}
	if err := sectionTitle(w, style, "CLIENT STORAGE"); err != nil {
		return err
	}
	for _, category := range []string{"SECURITY / AUTH", "DEBUG / FEATURE FLAGS", "APPLICATION STATE"} {
		values := categories[category]
		if len(values) == 0 {
			continue
		}
		sort.Slice(values, func(i, j int) bool {
			if values[i].store != values[j].store {
				return values[i].store < values[j].store
			}
			return values[i].key < values[j].key
		})
		if _, err := fmt.Fprintf(w, "  %s\n", style.bold(category)); err != nil {
			return err
		}
		for _, value := range values {
			if _, err := fmt.Fprintf(w, "    %s[%s]  ·  %s\n", value.store, value.key, orderedActions(value.actions)); err != nil {
				return err
			}
		}
	}
	if probeEntries > 0 {
		if _, err := fmt.Fprintf(w, "  %s\n    %d store/key %s · %d %s collapsed\n", style.bold("SDK / CAPABILITY PROBES"), probeEntries, plural(probeEntries, "pair"), probeOperations, plural(probeOperations, "operation")); err != nil {
			return err
		}
	}
	return nil
}

func storageCategory(key string) string {
	lower := strings.ToLower(key)
	if lower == "_applogtest-test-key" || lower == "_localforage_support_test" || lower == "_ranger-test-key" {
		return "probe"
	}
	if strings.Contains(lower, "debug") {
		return "debug"
	}
	if containsAny(lower, "token", "auth", "oauth", "auth_code", "authorization_code", "redirect", "pkce", "verifier", "challenge", "csrf") {
		return "security"
	}
	return "application"
}

func orderedActions(actions map[string]struct{}) string {
	ordered := make([]string, 0, len(actions))
	for _, action := range []string{"read", "write", "remove"} {
		if _, ok := actions[action]; ok {
			ordered = append(ordered, action)
		}
	}
	return strings.Join(ordered, "/")
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
