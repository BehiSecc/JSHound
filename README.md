# JSHound

**Understand any JavaScript file in seconds, without running it.**

JSHound is a fast, static analyzer for JavaScript and HTML. Point it at a
minified bundle, a compiled single-page app, or a whole directory of scripts,
and it tells you what each file is, what it does, and how it behaves at run
time. It also extracts endpoints, URLs, source maps, and secrets for security
review.

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![Go 1.24+](https://img.shields.io/badge/go-1.24%2B-00ADD8.svg)

- **Plain answers.** `--overview` summarizes a file in a few lines: its
  framework and build tool, its feature areas, and how it runs.
- **Works on minified code.** It reads the names and strings that survive
  minification, and applies a local or inline source map when one exists.
- **Leads for review.** The scanner lists endpoints, URLs, parameters,
  GraphQL operations, and source maps, and can hand files to Kingfisher for
  secret detection.
- **Safe by design.** Analyzed code is never executed.

## Contents

- [Install](#install)
- [Quick start](#quick-start)
- [Overview: what a file is and does](#overview-what-a-file-is-and-does)
- [Scan: endpoints, URLs, and parameters](#scan-endpoints-urls-and-parameters)
- [Source maps](#source-maps)
- [Secret scanning](#secret-scanning)
- [Input and output](#input-and-output)
- [Flags](#flags)
- [Safety and limits](#safety-and-limits)
- [Development](#development)

## Install

JSHound needs Go 1.24 or newer.

```bash
git clone https://github.com/BehiSecc/JSHound.git
cd JSHound
go install ./cmd/jshound
```

This puts `jshound` in your Go bin directory (usually `~/go/bin`). Secret
scanning additionally needs [Kingfisher](https://github.com/mongodb/kingfisher)
1.110.0 or newer.

## Quick start

```bash
# What is this bundle and what does it do?
jshound --overview -u https://target.example/static/js/main.js

# Which endpoints and URLs does it contain?
jshound -u https://target.example/static/js/main.js

# Recover the original sources from its source map
jshound -s https://target.example/static/js/main.js --output ./source
```

## Overview: what a file is and does

`--overview` answers three questions for each file. Abridged output for a
3 MiB Closure-compiled Angular app in which every function name is minified:

```text
## studio_compiled.js

### What it is

Browser app using Angular, compiled by Closure Compiler.

### What it does

Feature areas named throughout the code, with the actions next to them:

- creative: preview, copy, create, reject, submit
- feed: search, delete, edit, preview, publish
- asset: preview, transcode, add, delete, create

### How it runs

- Navigation: Angular router switches views without page reloads
- Server calls: XMLHttpRequest; sends X-Framework-Xsrf-Token, X-XSRF-TOKEN, and x-sfdc-authorization headers
- Files: resumable uploads with the X-Goog-Upload protocol, reads files in the browser
- Storage: localStorage
- Background: messages with other windows or workers
- Third parties: Google Ads (ads)
```

| Section | How JSHound decides |
| --- | --- |
| What it is | Runtime, kind of file, framework, and build tool. A distributed library, such as jQuery or Vue, is named by its licence banner. |
| What it does | Words that recur across names minifiers keep: strings, component selectors, method and property names, style file names, and user-facing messages. UI-kit prefixes, programming words, and bundled packages are ignored. |
| How it runs | One line per behavior observed in the code: how the UI starts, navigation, server calls and auth headers, sign-in methods, uploads, storage, background work, and third-party SDKs identified by the hosts they load from. |

A feature area needs several independent names before it is reported, so a
small file may show none. JSHound reports what the code can do; it does not
guess product names or claim the order in which things happen.

### Detailed report

Add `--verbose` to see the structure behind the summary and where to start
reading:

- **Bundled modules:** project areas and third-party packages, from esbuild
  markers or a source map.
- **Dispatch points:** switches, handler maps, and route registrations that
  select among many handlers, such as RPC methods or message types.
- **Main components:** likely responsibilities, with evidence and locations.
- **Entry paths and call flow:** startup calls, event registrations, and
  observed call links, drawn as a flowchart when the graph is large enough.
- **UI routes, browser behavior, and where to read first.**

Locations are one-based lines and byte columns, so you can find evidence in a
bundle that is a single long line. In a terminal, flowcharts render as Unicode
graphs; redirected output keeps copyable Mermaid blocks:

```bash
jshound --overview --verbose --output overview.md app.js
```

Call links are evidence visible in the code, not a runtime trace, and each
file is analyzed on its own: imports are not fetched and no links are inferred
between files.

## Scan: endpoints, URLs, and parameters

Without `--overview`, JSHound extracts concrete leads:

```text
$ jshound app.js
JSHound dev  │  5 findings  │  2 endpoints  │  2 URLs  │  1 maps
────────────────────────────────────────────────────────────

◆ ENDPOINTS (2)
  ─────────────────
  • /api/orders
  • /users?id=

◇ URLS (2)
  ────────────
  • https://api.example.com/v2
  • wss://rt.example.com/socket

⌁ SOURCE MAPS (1)
  ───────────────────
  • [UNREACHABLE] app.js.map
```

The scanner finds endpoints, URLs, emails, files, GraphQL operations, and
source-map references. Filtering is conservative to keep noise low.

```bash
# Add parameters and a wordlist built from the target's own routes
jshound -p --wordlist app.js

# Scan a directory recursively (skips .git and node_modules)
jshound ./assets
```

## Source maps

Scans check every source-map reference and report it as:

- `valid`: a reachable, readable source map v3
- `invalid`: reachable, but not a valid source map
- `unreachable`: missing or unreadable

To recover original sources, extract them from a JavaScript file or a map:

```bash
jshound -s https://target.example/app.js --output ./source
jshound -s https://target.example/app.js.map --output ./source
jshound --overview ./source
```

The output directory must be new or empty. Extraction follows relative and
absolute references, inline and indexed maps, and `SourceMap` or `X-SourceMap`
headers. It writes the `sourcesContent` embedded in the map and never fetches
source paths that the map points to.

`--overview` applies a map automatically when it needs no network access: an
inline `data:` map, or a map file next to or below a local input. It restores
original names and locations and groups the bundle into project areas and
packages.

## Secret scanning

Secret detection is opt-in and delegates to
[MongoDB Kingfisher](https://github.com/mongodb/kingfisher):

```bash
jshound --kingfisher /path/to/kingfisher app.js
jshound --kingfisher /path/to/kingfisher --validate-secrets app.js
```

Kingfisher is never searched for or started unless you pass
`--kingfisher PATH`. Findings include the secret value, its type, and the rule
ID, so treat output as sensitive. `--validate-secrets` checks credentials
against provider APIs; use it only where you are authorized to.

Kingfisher's Base64 scanning is disabled; its rules, filters, and validation
otherwise work as usual.

## Input and output

JSHound reads JavaScript or HTML (inline scripts included) from:

| Input | Example |
| --- | --- |
| Files | `jshound app.js index.html` |
| Directories, recursively | `jshound ./assets` |
| URLs, repeatable | `jshound -u https://a.example/app.js -u https://a.example/vendor.js` |
| Standard input | `cat app.js \| jshound --overview` |

Results print as text by default. Use `--format json` or `--format jsonl` for
scripts, and `--output FILE` to write to a file. In overview mode, JSON and
JSONL always contain the summary (under `purpose`) and every detailed field.

```bash
jshound --overview --format json -u https://target.example/app.js | jq '.files[].purpose'
jshound --format jsonl --output findings.jsonl ./assets
```

## Flags

| Flag | Description |
| --- | --- |
| `--overview` | Describe what each file is, what it does, and how it runs |
| `--verbose` | With `--overview`, add structure, entry paths, and call flow |
| `-u URL` | Analyze an HTTP(S) URL; repeatable |
| `-p` | Include parameters |
| `--wordlist` | Include a wordlist built from routes and parameters |
| `-s TARGET`, `--sourcemap TARGET` | Extract sources from a JavaScript file or map into `--output DIR` |
| `--kingfisher PATH` | Scan for secrets with this Kingfisher executable |
| `--validate-secrets` | Let Kingfisher validate secrets with provider APIs |
| `--format FORMAT` | `text` (default), `json`, or `jsonl` |
| `--output FILE` | Write results to a file instead of standard output |
| `--max-size MB` | Size limit per input or source map; default 32 |
| `--timeout DURATION` | HTTP request and Kingfisher startup timeout; default 15s |
| `--version` | Print the version and exit |

`--overview` ignores scan-only flags (`-p`, `--wordlist`, and the Kingfisher
flags). Source-map extraction is a separate mode.

## Safety and limits

- Analyzed JavaScript is never executed.
- Overview mode makes no network requests beyond fetching the `-u` inputs: it
  does not follow imports, URLs, or remote source maps.
- Each input checks at most 64 unique source-map references.
- Input and source-map sizes are capped by `--max-size`; network and
  Kingfisher startup by `--timeout`.

## Development

```bash
go test -race ./...
go vet ./...
go build ./cmd/jshound
```

Issues and pull requests are welcome. Please include a failing test or a
sample file with any bug report.

## License

JSHound is released under the [MIT License](LICENSE). Kingfisher is a separate
program licensed under Apache-2.0; see its repository for details.
