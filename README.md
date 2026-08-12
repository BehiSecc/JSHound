# JSHound

JSHound is a fast, static JavaScript and HTML analyzer for bug-bounty workflows.
It finds endpoints, URLs, emails, referenced files, GraphQL operations,
parameters, source maps, and secrets without executing untrusted JavaScript.

Endpoint, URL, and email filtering follows HuntProxy's conservative
PageAnalyzer behavior. Secret finding and optional live validation delegate to
[MongoDB Kingfisher](https://github.com/mongodb/kingfisher), preserving its
full rule and validation coverage while keeping JSHound focused and small.

## Install

Requirements:

- Go 1.24 or newer
- Kingfisher 1.110.0 or newer on `PATH` for secret scanning

```bash
go install github.com/jshound/jshound/cmd/jshound@latest
```

To scan without Kingfisher, pass `--no-secrets`.

## Usage

```bash
# Local JavaScript or HTML
jshound app.js
jshound index.html

# Remote page or bundle
jshound https://target.example/app.js

# Recursive directory scan; skips .git and node_modules
jshound ./assets

# Pipeline input
cat app.js | jshound --no-secrets

# Parameters and target-specific words
jshound -p --wordlist app.js

# JSON/JSONL and output files
jshound --format json --output findings.json app.js
jshound --format jsonl app.js

# Explicitly make Kingfisher's provider validation requests
jshound --validate-secrets app.js
```

Secret validation is off by default. `--validate-secrets` can send discovered
credentials to their provider APIs, so use it only where you are authorized.
Secret values are always hash-redacted before JSHound receives them.

Source-map references are resolved against their containing file or URL. A
map is reported as:

- `valid`: reachable/readable and valid source-map v3 JSON
- `invalid`: reachable/readable but not structurally valid
- `unreachable`: unavailable or unreadable

## Flags

```text
--validate-secrets  Run Kingfisher live validation
--no-secrets        Skip secret scanning
-p                  Include parameters
--wordlist          Include route/parameter-derived words
--output FILE       Write output to FILE
--format FORMAT     text, json, or jsonl
--max-size MB       Per-input/source-map limit (default 32)
--timeout DURATION  HTTP request and Kingfisher startup timeout (default 15s)
--kingfisher PATH   Use a specific Kingfisher executable
```

Kingfisher Base64 scanning is deliberately disabled. All built-in rules,
dependent matches, entropy/checksum filters, parser context, and validation
mechanisms otherwise remain enabled. JSHound requires Kingfisher at runtime
rather than shipping a stale copy of its evolving rules.

JSHound checks at most 64 unique source-map references per input to keep
network work bounded. Kingfisher applies its own validation request timeout,
retry, and rate-limit policies during `--validate-secrets` scans.

## Development

```bash
go test -race ./...
go vet ./...
go build ./cmd/jshound
```

## License

JSHound is MIT licensed. Kingfisher is a separate Apache-2.0-licensed program;
see its repository for its license and notices.
