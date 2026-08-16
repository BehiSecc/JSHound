# JSHound

JSHound is a fast, static JavaScript and HTML analyzer built for bug-bounty and
application-recon workflows. It turns frontend code into useful security
evidence without executing untrusted JavaScript.

Use it in two complementary ways:

- `--overview` produces a concise attack-surface story: application behavior,
  authentication, storage, browser trust boundaries, cross-window messaging,
  and prioritized review targets.
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

## Application overview

Overview mode is designed to answer the questions that matter during initial
recon:

- What framework, bundler, and build profile are present?
- What application workflows and privileged actions are exposed?
- How are credentials, tokens, cookies, and browser storage used?
- Are `postMessage` senders or receivers present?
- Which calls belong to Window, Worker, MessagePort, or BroadcastChannel?
- Are fixed, wildcard, or dynamic Window `targetOrigin` values used?
- Which Window handlers validate `event.origin` and/or `event.source`?
- Which storage keys are security state, application state, debug controls, or
  SDK capability probes?
- Which authorization and trust boundaries deserve manual review?

Example structure:

```text
AT A GLANCE
  ◆ OAuth redirect data written to sessionStorage
  ◆ 5 message receivers · 1 without origin validation · 2 unresolved
  ◆ Production debug flag referenced

CLIENT STORAGE
  SECURITY / AUTH
    sessionStorage[account_auth_code]  ·  write
    sessionStorage[oauth_redirect_uri]  ·  write
  SDK / CAPABILITY PROBES
    3 store/key pairs · 8 operations collapsed

CROSS-WINDOW MESSAGING
  ◆ 4 confirmed Window receivers
    origin + source validation    1
    origin validation only        1
    no recognizable validation    1
    validation unresolved         1
  ◆ 9 parsed postMessage calls
    Window               3
    Worker               2
    MessagePort          3
    unresolved/local     1
    Window wildcard targetOrigin 1

PRIORITIZED REVIEW TARGETS
  ╭─ 01  OAuth redirect integrity
  │    OBSERVED · 2 signals
  │
  │  Review redirect allowlisting, state and authorization-code binding,
  │  replay resistance, and cleanup after the flow completes.
  ╰────────────────────────────────────────────────────────
```

Raw endpoint and URL inventories remain in the standard scanner and are not
repeated in overview output. The report uses colors when writing directly to a
terminal and stays ANSI-free when redirected or when `NO_COLOR` is set.

Evidence counts represent distinct behaviors or operation types where
possible, rather than repeated keyword occurrences. Messaging classifications
are conservative: ambiguous listeners and local `postMessage` implementations
are reported as unresolved instead of being treated as Window boundaries.

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
--overview          Write a static application recon brief
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
- Overview mode does not follow discovered endpoints, URLs, or source maps.
- Review targets are static-analysis leads, not vulnerability verdicts.
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
