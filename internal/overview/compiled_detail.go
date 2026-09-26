package overview

import (
	"sort"
	"strings"
)

// Compiled records named interfaces still visible after minification. A path
// literal is a definition or reference, not proof that a request is sent.
type Compiled struct {
	Services []ServiceGroup `json:"rpc_services,omitempty"`
	Paths    []PathGroup    `json:"-"`
	Boot     string         `json:"boot_call,omitempty"`
}

type ServiceGroup struct {
	Name    string   `json:"name"`
	Count   int      `json:"method_count"`
	Methods []string `json:"example_methods"`
}

type PathGroup struct {
	Prefix   string   `json:"prefix"`
	Count    int      `json:"path_count"`
	Examples []string `json:"examples"`
}

func analyzeCompiledDetail(tokens []token) *Compiled {
	services := make(map[string]map[string]struct{})
	pathValues := make(map[string]struct{})
	boot := ""
	for i, tok := range tokens {
		if tok.kind == 'i' && tok.text == "_ModuleManager_initialize" && at(tokens, i+1) == "(" &&
			at(tokens, i-1) != "=" && loadTimeCall(tokens, i) {
			boot = "The load-time wrapper contains a _ModuleManager_initialize() call."
		}
		if tok.kind != 's' {
			continue
		}
		value := stringValue(tok.text)
		if !isRoutePath(value) {
			continue
		}
		if service, method, ok := rpcName(value); ok {
			if services[service] == nil {
				services[service] = make(map[string]struct{})
			}
			services[service][method] = struct{}{}
			continue
		}
		if !usefulPath(value) {
			continue
		}
		pathValues[value] = struct{}{}
	}
	if len(services) == 0 && len(pathValues) == 0 && boot == "" {
		return nil
	}
	result := &Compiled{Boot: boot}
	for name, methods := range services {
		list := sortedKeys(methods)
		result.Services = append(result.Services, ServiceGroup{Name: name, Count: len(list), Methods: sampleMethods(list)})
	}
	sort.Slice(result.Services, func(i, j int) bool {
		if result.Services[i].Count != result.Services[j].Count {
			return result.Services[i].Count > result.Services[j].Count
		}
		return result.Services[i].Name < result.Services[j].Name
	})
	if len(result.Services) > 6 {
		result.Services = result.Services[:6]
	}
	for prefix, family := range groupedPaths(pathValues) {
		if len(family) < 2 {
			continue
		}
		list := sortedKeys(family)
		result.Paths = append(result.Paths, PathGroup{Prefix: prefix, Count: len(list), Examples: list[:min(3, len(list))]})
	}
	sort.Slice(result.Paths, func(i, j int) bool {
		if result.Paths[i].Count != result.Paths[j].Count {
			return result.Paths[i].Count > result.Paths[j].Count
		}
		return result.Paths[i].Prefix < result.Paths[j].Prefix
	})
	if len(result.Paths) > 5 {
		result.Paths = result.Paths[:5]
	}
	if len(result.Services) == 0 && len(result.Paths) == 0 && result.Boot == "" {
		return nil
	}
	return result
}

func loadTimeCall(tokens []token, call int) bool {
	if tokens[call].depth == 0 {
		return true
	}
	for i := call - 1; i >= 0; i-- {
		if at(tokens, i) != "{" || tokens[i].match <= call {
			continue
		}
		function := -1
		if at(tokens, i-1) == ")" {
			params := tokens[i-1].match
			for j := params - 1; j >= max(0, params-3); j-- {
				if at(tokens, j) == "function" {
					function = j
					break
				}
			}
		} else if at(tokens, i-1) == "=>" {
			return false // a nested arrow's execution is not established
		}
		if function < 0 {
			continue
		}
		close := tokens[i].match
		if at(tokens, function-1) == "(" && at(tokens, close+1) == ")" &&
			(at(tokens, close+2) == "(" || at(tokens, close+2) == "." && at(tokens, close+3) == "call" && at(tokens, close+4) == "(") {
			return true
		}
		if at(tokens, function-1) == "!" && at(tokens, close+1) == "(" {
			return true
		}
		return false // the nearest containing function is not invoked here
	}
	return false
}

func rpcName(path string) (string, string, bool) {
	trimmed := strings.TrimPrefix(path, "/")
	var service, method string
	qualified := false
	if slash := strings.LastIndexByte(trimmed, '/'); slash >= 0 {
		service, method = trimmed[:slash], trimmed[slash+1:]
		if dot := strings.LastIndexByte(service, '.'); dot >= 0 {
			qualified = true
			service = service[dot+1:]
		}
	} else if dot := strings.LastIndexByte(trimmed, '.'); dot >= 0 {
		service, method = trimmed[:dot], trimmed[dot+1:]
	}
	if !strings.HasSuffix(service, "Service") && service != "Operations" && !(qualified && len(service) > 2 && service[0] >= 'A' && service[0] <= 'Z') {
		return "", "", false
	}
	if method == "" || method[0] < 'A' || method[0] > 'Z' {
		return "", "", false
	}
	for _, ch := range method {
		if ch < 'A' || ch > 'Z' {
			if ch < 'a' || ch > 'z' {
				if ch < '0' || ch > '9' {
					return "", "", false
				}
			}
		}
	}
	return service, method, true
}

func usefulPath(value string) bool {
	if len(value) < 5 || strings.Contains(value, "..") || strings.Contains(value, "?") || strings.Contains(value, ".js") || strings.Contains(value, ".css") {
		return false
	}
	for _, extension := range []string{".svg", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".woff", ".woff2", ".pdf"} {
		if strings.HasSuffix(value, extension) {
			return false
		}
	}
	segment := strings.Trim(strings.SplitN(value[1:], "/", 2)[0], "/")
	switch segment {
	case "div", "span", "strong", "table", "script", "style", "body", "head", "html", "title", "label", "input", "button":
		return false
	}
	return len(segment) >= 3
}

func groupedPaths(values map[string]struct{}) map[string]map[string]struct{} {
	byRoot := make(map[string][]string)
	for path := range values {
		root := strings.SplitN(path[1:], "/", 2)[0]
		byRoot[root] = append(byRoot[root], path)
	}
	groups := make(map[string]map[string]struct{}, len(byRoot))
	for root, paths := range byRoot {
		prefix := "/" + root
		for depth := 1; depth < 4; depth++ {
			children := make(map[string][]string)
			for _, path := range paths {
				parts := strings.Split(strings.Trim(path, "/"), "/")
				if len(parts) > depth {
					children[parts[depth]] = append(children[parts[depth]], path)
				}
			}
			next, largest := "", []string(nil)
			for name, child := range children {
				if len(child) > len(largest) {
					next, largest = name, child
				}
			}
			if len(largest) < 2 || len(largest)*5 < len(paths)*4 {
				break
			}
			if len(largest) < len(paths) {
				major := make(map[string]struct{}, len(largest))
				for _, path := range largest {
					major[path] = struct{}{}
				}
				rest := make(map[string]struct{})
				for _, path := range paths {
					if _, ok := major[path]; !ok {
						rest[path] = struct{}{}
					}
				}
				if len(rest) >= 2 {
					groups[prefix] = rest
				}
			}
			paths = largest
			prefix += "/" + next
		}
		groups[prefix] = make(map[string]struct{}, len(paths))
		for _, path := range paths {
			groups[prefix][path] = struct{}{}
		}
	}
	return groups
}

func sampleMethods(methods []string) []string {
	if len(methods) <= 4 {
		return methods
	}
	// Use a few distinct verbs to show the service's actual operations.
	chosen := make([]string, 0, 4)
	verbs := []string{"Create", "Get", "List", "Update", "Delete", "Submit", "Approve", "Download"}
	for _, verb := range verbs {
		for _, method := range methods {
			if strings.HasPrefix(method, verb) {
				chosen = append(chosen, method)
				break
			}
		}
		if len(chosen) == 4 {
			return chosen
		}
	}
	for _, method := range methods {
		if len(chosen) == 4 {
			break
		}
		if !containsString(chosen, method) {
			chosen = append(chosen, method)
		}
	}
	return chosen
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
