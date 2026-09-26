# JSHound

JSHound is a fast, static JavaScript and HTML analyzer for application review.
It helps you understand a file's structure and extract concrete leads without
executing the code.

Use it in two complementary ways:

- `--overview` shows the file's module shape, entry paths, main functions,
  observed calls, and browser interactions. It adds a Mermaid flowchart when
  enough direct relationships are visible.
- The standard scanner extracts endpoints, URLs, parameters, files, emails,
  GraphQL operations, source maps, and optional Kingfisher findings.

## Install

Requirements:

- Go 1.24 or newer
- Kingfisher 1.110.0 or newer only when secret scanning is wanted

```bash
go install github.com/jshound/jshound/cmd/jshound@latest
```

## Quick start

Analyze a remote bundle:

```bash
jshound --overview -u https://target.example/app.js
```

Analyze a local file or directory:

```bash
jshound --overview app.js
jshound --overview ./assets
```

Use pipeline input:

```bash
cat app.js | jshound --overview
```

## JavaScript overview

Use `--overview` to get oriented before reading a file. For readable modules,
the report lists imports and exports, named functions and classes, startup
calls, event registrations, direct call links, and code interactions such as
`fetch()`, browser storage, workers, and DOM updates. Line numbers point back
to the input.

For a file with `DOMContentLoaded → start() → loadUsers() → render()`, the
report shows each observed link and, when the graph is large enough, a Mermaid
flowchart. The diagram describes calls and callbacks visible in the file; it
does not claim a guaranteed execution order through branches or asynchronous
code.

Compiled bundles get a shorter report. JSHound recognizes Webpack-style chunk
registration and UMD wrappers, shows any visible module factory count or
global namespace, and avoids listing thousands of generated function names.
When compilation hides the original module graph, the report says so.

Each source is reported separately. Overview does not fetch imports or follow
source maps, and it does not infer links between files. Use the standard scan
for the full endpoint, URL, file, and email inventory.

Overview supports text, JSON, and JSONL:

```bash
jshound --overview --format json -u https://target.example/app.js
jshound --overview --format jsonl --output overview.jsonl app.js
```

## Standard extraction

```bash
# Local JavaScript or HTML
jshound app.js
jshound index.html

# Remote page or bundle
jshound -u https://target.example/app.js

# Recursive scan; skips .git and node_modules
jshound ./assets

# Parameters and a target-specific wordlist
jshound -p --wordlist app.js

# Machine-readable output
jshound --format json --output findings.json app.js
jshound --format jsonl app.js
```

Endpoint, URL, and email filtering follows HuntProxy's conservative
PageAnalyzer behavior.

## Source maps

Normal scans discover and classify source-map references as:

- `valid`: reachable/readable source-map v3 JSON
- `invalid`: reachable/readable but structurally invalid
- `unreachable`: unavailable or unreadable

Extract embedded source content from a JavaScript file or a direct map:

```bash
jshound -s https://target.example/app.js --output ./source
jshound --sourcemap https://target.example/app.js.map --output ./source
```

The output must be a new or empty directory. Extraction supports local and
remote inputs, relative and absolute references, inline and indexed maps, and
`SourceMap`/`X-SourceMap` headers. Missing `sourcesContent` entries are skipped;
JSHound does not fetch source paths controlled by a map.

## Secret scanning

Secret detection is opt-in and delegates to
[MongoDB Kingfisher](https://github.com/mongodb/kingfisher):

```bash
jshound --kingfisher /path/to/kingfisher app.js
jshound --kingfisher /path/to/kingfisher --validate-secrets app.js
```

Kingfisher is never searched for or started unless `--kingfisher PATH` is
provided. Live validation is off by default; `--validate-secrets` may send
credentials to provider APIs, so use it only where authorized.

Secret values are emitted with their Kingfisher type and rule ID. Treat text
and machine-readable results as sensitive. Kingfisher Base64 scanning is
disabled; its built-in rules, dependent matches, entropy/checksum filters,
parser context, and validation mechanisms otherwise remain enabled.

## Flags

```text
--overview          Describe JavaScript structure and observed flow
-u URL              Analyze an HTTP(S) URL (repeatable)
-p                  Include parameters
--wordlist          Include route/parameter-derived words
--kingfisher PATH   Enable secrets with this Kingfisher executable
--validate-secrets  Run Kingfisher live validation
-s TARGET, --sourcemap TARGET
                    Extract a JavaScript/source-map target into --output DIR
--output FILE       Write output to FILE
--format FORMAT     text, json, or jsonl
--max-size MB       Per-input/source-map limit (default 32)
--timeout DURATION  HTTP request and Kingfisher startup timeout (default 15s)
--version           Print the version and exit
```

`--overview` cannot be combined with source-map extraction,
parameter/wordlist flags, or Kingfisher flags.

## Safety and limits

- JSHound never executes analyzed JavaScript.
- Overview mode does not follow imports, discovered endpoints, URLs, or source maps.
- Observed call links are lexical evidence, not a full runtime trace.
- Source-map checks are bounded to 64 unique references per input.
- Input and source-map sizes are bounded by `--max-size`.
- Network and Kingfisher startup work are bounded by `--timeout`.

## Development

```bash
go test -race ./...
go vet ./...
go build ./cmd/jshound
```

## License

JSHound is MIT licensed. Kingfisher is a separate Apache-2.0-licensed program;
see its repository for its license and notices.
