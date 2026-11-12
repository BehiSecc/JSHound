# JSHound

JSHound is a fast, static JavaScript and HTML analyzer for application review.
It helps you understand a file's structure and extract concrete leads without
executing the code.

Use it in two complementary ways:

- `--overview` says what each file is and what it does in a few lines.
  `--verbose` adds components, UI routes, entry paths, browser interactions,
  and Mermaid diagrams of the relationships visible in the code.
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

Use `--overview` for a short answer to what a file is and what it does:

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

| Section | Where it comes from |
| --- | --- |
| What it is | Runtime, kind of file, framework, and build tool; a distributed library is named by its licence banner |
| What it does | Words that recur across names minifiers keep: strings, component selectors, property and method names, style file names, and user-facing messages. Toolkit prefixes, plumbing words, and bundled packages are ignored |
| How it runs | One line per observed behavior: how the UI starts, navigation, server calls and auth headers, sign-in methods, file uploads, storage, background work, and third-party SDKs identified by the hosts they load from |

A feature needs several independent names before it is reported, so small or
heavily minified files may show none. Product names are not guessed.

### Detailed report

Add `--verbose` to understand the structure and where to start reading. The
detailed report highlights the evidence it can recover:

| Section | What it tells you |
| --- | --- |
| What this file does | Runtime, framework, kind of file, and how execution starts |
| Bundled modules | Project areas and third-party packages inside a bundle, from esbuild markers or a source map |
| Dispatch points | Switches, handler maps, and route registrations that select among many handlers, such as RPC methods or message types |
| Main components | Likely responsibilities, supporting evidence, and module locations |
| Architecture | Observed module, loading, route, and request relationships |
| UI routes and navigation | Paths used in route definitions or navigation |
| Browser behavior | Storage, events, messaging, and other browser interactions |
| Where to read first | Source locations for the most useful components |

Minified files retain structural analysis. Readable scripts also show imports,
exports, named functions and classes, startup calls, event registrations, and
direct call links. Locations use one-based lines and byte columns to help find
evidence in bundles that occupy a single long line. Sections without useful
evidence are omitted.

For a file with `DOMContentLoaded → start() → loadUsers() → render()`, the
report shows each observed link and, when the graph is large enough, a Mermaid
flowchart. The diagram describes calls and callbacks visible in the file; it
does not claim a guaranteed execution order through branches or asynchronous
code.

For Webpack bundles, the report identifies components by module ID and shows
startup callbacks, required chunks, and dependencies outside the file. Other
compiled scripts retain visible RPC interfaces and initialization details.
Component roles are inferred from code evidence; request-related code does not
prove that a network request was sent, and arbitrary path literals are not
treated as confirmed UI routes.

Each source is reported separately. Overview does not fetch imports, and it
does not infer links between files. It applies a source map when one is
available without network access: an inline `data:` map, or a regular map file
in a local input's directory or below it. The map restores original function names and source locations
and groups the bundle into project areas and packages. Remote maps are never
fetched in overview mode. Terminals show Unicode
flowcharts, with color when enabled; redirected text keeps copyable Mermaid
blocks in Markdown. Use the standard scan to list API endpoints, URLs, files,
and emails. To inspect original sources from an available source map,
extract them with `jshound -s TARGET --output ./source`, then run
`jshound --overview ./source`.

Overview supports text, JSON, and JSONL. JSON and JSONL always include the
summary under `purpose` and every detailed field:

```bash
jshound --overview --format json -u https://target.example/app.js
jshound --overview --format jsonl --output overview.jsonl app.js
# Save the detailed Markdown report with copyable Mermaid diagrams
jshound --overview --verbose --output overview.md app.js
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
--overview          Describe what each JavaScript file is and what it does
--verbose           With --overview, add structure, entry paths, and call flow
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

When `--overview` is used, parameter, wordlist, and Kingfisher flags are
ignored. Source-map extraction remains a separate mode.

## Safety and limits

- JSHound never executes analyzed JavaScript.
- Overview mode does not follow imports, discovered endpoints, or URLs, and only
  reads inline or local source maps.
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
