// Package wordlist derives concise, target-specific words from discovered
// routes and parameter names.
package wordlist

import (
	"net/url"
	"path"
	"sort"
	"strings"
	"unicode"
)

// Build returns sorted unique words derived from endpoints, URLs, files, and
// parameter names. Common scheme/host noise and numeric-only tokens are omitted.
func Build(endpoints, urls, files, parameters []string) []string {
	words := make(map[string]struct{})
	for _, parameter := range parameters {
		add(words, parameter)
	}
	for _, value := range append(append(append([]string{}, endpoints...), urls...), files...) {
		pathname := value
		if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
			pathname = parsed.Path
		}
		pathname = strings.TrimSuffix(pathname, path.Ext(pathname))
		for _, token := range strings.FieldsFunc(pathname, func(r rune) bool {
			return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-')
		}) {
			add(words, token)
		}
	}
	result := make([]string, 0, len(words))
	for word := range words {
		result = append(result, word)
	}
	sort.Strings(result)
	return result
}

func add(words map[string]struct{}, value string) {
	value = strings.Trim(strings.ToLower(value), "_-.:{}")
	if len(value) < 2 || len(value) > 64 {
		return
	}
	allDigits := true
	for _, r := range value {
		if !unicode.IsDigit(r) {
			allDigits = false
			break
		}
	}
	if !allDigits {
		words[value] = struct{}{}
	}
}
