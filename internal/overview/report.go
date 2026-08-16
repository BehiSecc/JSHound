package overview

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func WriteJSON(w io.Writer, result Result) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(result)
}

// WriteJSONL writes one stable record for each observed overview fact.
func WriteJSONL(w io.Writer, result Result) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	for _, item := range result.Requests {
		if err := encoder.Encode(struct {
			Kind string `json:"kind"`
			Request
		}{"request", item}); err != nil {
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

func WriteText(w io.Writer, result Result) error {
	if _, err := fmt.Fprint(w, "\nAPPLICATION OVERVIEW\n────────────────────────────────────────────────────────────\n"); err != nil {
		return err
	}
	profile := []string{fmt.Sprintf("%d input %s", result.Sources, plural(result.Sources, "source")), fmt.Sprintf("%d bytes", result.Bytes)}
	if result.Minified {
		profile = append(profile, "minified input detected")
	}
	if result.SourceMap {
		profile = append(profile, "source-map reference detected")
	}
	if err := section(w, "PROFILE", append(append([]string{}, prefixed("Framework", result.Frameworks)...), append(prefixed("Bundler", result.Bundlers), profile...)...)); err != nil {
		return err
	}

	requests := make([]string, 0, len(result.Requests))
	for _, item := range result.Requests {
		value := item.Target
		if item.Method != "" {
			value = item.Method + " " + value
		}
		requests = append(requests, value)
	}
	if err := section(w, "OBSERVED API SURFACE", requests); err != nil {
		return err
	}
	if err := section(w, "AUTHENTICATION", result.Authentication); err != nil {
		return err
	}
	storage := make([]string, 0, len(result.Storage))
	for _, item := range result.Storage {
		storage = append(storage, item.Store+"["+item.Key+"] ("+item.Action+")")
	}
	if err := section(w, "STORAGE", storage); err != nil {
		return err
	}
	if err := section(w, "ROUTES & FEATURES", result.Routes); err != nil {
		return err
	}
	if err := section(w, "GRAPHQL", result.GraphQLOperations); err != nil {
		return err
	}
	if err := section(w, "THIRD PARTIES", result.ThirdParties); err != nil {
		return err
	}
	if err := section(w, "BROWSER CAPABILITIES", result.Capabilities); err != nil {
		return err
	}
	reviews := make([]string, 0, len(result.ReviewTargets))
	for _, item := range result.ReviewTargets {
		reviews = append(reviews, strings.ToUpper(item.Confidence)+"  "+item.Value)
	}
	if err := section(w, "REVIEW TARGETS", reviews); err != nil {
		return err
	}
	return section(w, "LIMITATIONS", result.Limitations)
}

func section(w io.Writer, title string, items []string) error {
	if len(items) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "\n%s\n", title); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := fmt.Fprintf(w, "  • %s\n", item); err != nil {
			return err
		}
	}
	return nil
}

func prefixed(label string, values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, label+": "+value)
	}
	return result
}
func plural(count int, word string) string {
	if count == 1 {
		return word
	}
	return word + "s"
}
