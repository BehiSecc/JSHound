# JSHound

**Understand any JavaScript file in seconds, without running it.**

JSHound is a fast, static analyzer for JavaScript and HTML. Point it at a
minified bundle, a compiled single-page app, or a whole directory of scripts,
and it tells you what each file is, what it does, and how it behaves at run
time. It also extracts endpoints, RPC methods, URLs, source maps, and secrets
for security review.

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![Go 1.24+](https://img.shields.io/badge/go-1.24%2B-00ADD8.svg)

- **Plain answers.** `--overview` summarizes a file, or a set of files as
  one application, in a few lines: its type (framework and build tool), its
  features, and its run-time behavior.
- **Works on minified code.** It reads the names and strings that survive
  minification, and applies a local or inline source map when one exists.
- **Leads for review.** The scanner lists endpoints, RPC methods, URLs,
  parameters, GraphQL operations, and source maps, and can hand files to
  Kingfisher for secret detection.
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
go install github.com/BehiSecc/JSHound/cmd/jshound@latest
```

This puts `jshound` in your Go bin directory (usually `~/go/bin`). To build
from source instead:

```bash
git clone https://github.com/BehiSecc/JSHound.git
cd JSHound
go build -o jshound ./cmd/jshound
```

Secret scanning additionally needs
[Kingfisher](https://github.com/mongodb/kingfisher) 1.110.0 or newer.

## Quick start

```bash
# What is this bundle and what does it do?
jshound --overview -u https://target.example/static/js/main.js

# What is this set of chunks as one application?
jshound --overview ./assets

# Which endpoints and URLs does it contain?
jshound -u https://target.example/static/js/main.js

# Recover the original sources from its source map
jshound -s https://target.example/static/js/main.js --output ./source
```

## Overview: what a file is and does

`--overview` prints three sections for each file: its type, its features, and
its behavior. Output for a sample project-management app:

```text
# JavaScript overview

1 source · 9.6 KiB

## app.js

ES module · 9.6 KiB

### Type

Browser app using React.

### Features

- sign-in (password, email): register, reset, sign in, verify
    4 API calls (/api/v1/email/login, /api/v1/email/register, /api/v1/password/reset)
- project: archive, copy, create, rename, share
    6 API calls (ProjectService) · 2 screens (/projects, /projects/:id)
- invoice: create, export, pay, preview, refund
    6 API calls (/api/v1/invoices/create, /api/v1/invoices/export, /api/v1/invoices/refund) · 2 screens (/invoices, /invoices/:id)
- timesheet: assign, close, create, delete, move
    5 API calls (TimesheetService)

### Behavior

- Starts: mounts a React UI into #root
- Screens: 4 routes (/invoices, /projects, /invoices/:id, +1 more)
- Server calls: fetch; sends Authorization and X-CSRF-Token headers
- API: 21 calls in 5 groups (/invoices 6, ProjectService 6, TimesheetService 5, +2 more)
- Storage: localStorage
- Third parties: Sentry (error reporting)

Use --verbose for evidence, structure, and call flow. Run without --overview to list every endpoint and RPC method.
```

| Section | How JSHound decides |
| --- | --- |
| Type | Runtime, kind of file, framework, and build tool. A distributed library, such as jQuery or Vue, is named by its licence banner. Obsidian plugins, VS Code extensions, and Electron apps (main, preload, or renderer) are named from what the file imports; browser extensions from the `chrome.*` or `browser.*` members an extension uses about itself, so a page that only messages an extension is not one. "App" requires a real UI mount, the same test as the `Starts` line. Bundle chunks (Next.js, esbuild, Module Federation containers), Closure-compiled output, and scripts that only define custom elements are named as such. |
| Features | Words that recur across names minifiers keep (strings, component selectors, method and property names, style file names, user-facing messages), joined to the API calls (RPC methods found by how they are declared or called, REST paths, endpoint tables, GraphQL operations) and UI routes that name the same area. UI-kit prefixes, naming conventions, hash fragments, programming words, bundled packages, and third-party services are ignored. When names give no area, a first path segment shared by several screens or API calls can name one. Areas about signing in, judged by their actions and API paths and not by their names, are shown as one `sign-in` area that lists what it gathers. |
| Behavior | One line per behavior observed in the code: how the code starts (a framework mount call such as `createRoot` or `createApp` counts as a UI mount, but a file that only defines such an entry point is a runtime, not an app; otherwise the calls it makes at load), navigation and the screens it counts, server calls and auth headers, a summary of the API calls, sign-in methods, uploads, storage, extension APIs (most sensitive first, with a short gloss), background work, and third-party SDKs identified by the hosts they load from. |

An area with API or route evidence gets an indented line with counts and up to
three examples. The overview is a summary and not an endpoint list: the
`API` line in Behavior gives the total and the largest groups, and the scanner
prints every endpoint and RPC method. Calls with an RPC service are grouped by
service, other paths by their first segment. Every RPC method counts once,
whether it is also an endpoint or not, and endpoint tables count as API calls
too. A request counts by its literal path even when a base variable is
concatenated in front, and client methods such as `get`, `post`, `request`, and
`ajax` count as request calls. Screens come from route tables, `route` and
`routerLink` properties, and tables whose names or keys say view, route, page,
or screen.

A feature area needs more than names: user-facing text, a screen, or an API
call must back them up, so a small file may show no areas, and Features says
so. In "Behavior", a claim that rests on a specific call, constructor, or
vendor constant stands on one use, while a claim inferred from an ordinary
word needs two. Facts that
cannot apply where the file runs are left out: a service worker, web worker,
or Node.js program has no navigation or UI mount. JSHound reports what the
code can do; it does not guess product names or claim the order in which
things happen.

### Several files

Give `--overview` several files or a directory and it first prints the same
three sections for the whole set. Features and behavior are merged across
files, so the API and Screens lines cover the set, and a file that names
another input file in a string, such as `import("./projects.js")`, is recorded
as loading it, which is how the entry file is found. Imports are never
fetched; only the files you pass are read.

```text
# JavaScript overview

8 sources · 10.0 KiB

## All 8 files

### Type

Browser app.

- 8 files (10.0 KiB): 5 scripts, 3 libraries
- Entry file: main.js (loads 7 files and no other file loads it; mounts a UI into #root)

### Features

- sign-in (password, email): register, reset, sign in, verify
    4 API calls (/api/v1/email/login, /api/v1/email/register, /api/v1/password/reset)
- project: archive, copy, create, rename, share
    6 API calls (ProjectService) · 2 screens (/projects, /projects/:id)
- invoice: create, export, pay, preview, refund
    6 API calls (/api/v1/invoices/create, /api/v1/invoices/export, /api/v1/invoices/refund) · 2 screens (/invoices, /invoices/:id)
- timesheet: assign, close, create, delete, move
    5 API calls (TimesheetService)

### Behavior

- Starts: mounts its UI into #root
- Screens: 4 routes (/invoices, /projects, /invoices/:id, +1 more)
- Server calls: fetch; sends Authorization header
- API: 21 calls in 5 groups (/invoices 6, ProjectService 6, TimesheetService 5, +2 more)
- Storage: localStorage

## Files

- main.js · 709 B · Browser app · behavior: starts, screens, server calls, storage · entry file
- account.js · 2.9 KiB · Browser script · sign-in
- timesheets.js · 2.1 KiB · Browser script · timesheet
- projects.js · 2.0 KiB · Browser script · project
- invoices.js · 1.9 KiB · Browser script · invoice
- vendor.js · 271 B · The react 18.2.0 library for the browser, named by its licence banner · behavior: storage

Nothing to report in 2 files:

- 2 libraries · 211 B

Run --overview on a single file, or add --verbose, for the full section of each file. Run without --overview to list every endpoint and RPC method.
```

Up to five files get a full section each. Larger sets print a compact index:
one line per file with something to report, entry files first, then files that
share an area with the set, with quiet files grouped by kind. `--verbose`
prints every full section. Files that have nothing to do with each other print
one line saying so (`3 separate files, not one application.`) instead of a
merged summary, followed by a section per file.

### Detailed report

Add `--verbose` to print the same three sections followed by the structure
behind them and where to start reading:

- **Bundled modules:** project areas and third-party packages, from esbuild
  markers or a source map.
- **Dispatch points:** switches, handler maps, and route registrations that
  select among many handlers, such as RPC methods or message types.
- **Main components:** likely responsibilities, with evidence and locations.
- **Entry paths and call flow:** startup calls, event registrations, and
  observed call links, drawn as a flowchart when the graph is large enough.
- **Imports, exports, UI routes, and where to read first.**

For each run fact under "Behavior" that rests on signals in the code, the
verbose report also shows how often each signal was used and where it was first
seen.

In a minified file with no applied source map, units, call links, exports,
entry paths, and diagram edges that carry minified names are not listed; one
closing line gives their counts. Names restored by a source map are listed.

Locations are one-based lines and byte columns, so you can find evidence in a
bundle that is a single long line. In a terminal, flowcharts render as Unicode
graphs; redirected output keeps copyable Mermaid blocks:

```bash
jshound --overview --verbose --output overview.md app.js
```

Call links are evidence visible in the code, not a runtime trace. Imports are
never fetched; files given together are linked only when one names another by
file name.

## Scan: endpoints, URLs, and parameters

Without `--overview`, JSHound extracts concrete leads:

```text
$ jshound app.js

JSHound dev  │  7 findings  │  2 endpoints  │  2 URLs  │  1 RPC methods  │  1 files  │  1 maps
────────────────────────────────────────────────────────────

◆ ENDPOINTS (2)
  ─────────────────
  • /api/orders
  • /users?id=

◇ URLS (2)
  ────────────
  • https://api.example.com/v2
  • wss://rt.example.com/socket

⇄ RPC METHODS (1)
  ───────────────────
  • OrderService.CreateOrder  (/shop.OrderService/CreateOrder, id aB3xYz)

▧ FILES (1)
  ─────────────
  • ./chunk-4F2A9C1D.js

⌁ SOURCE MAPS (1)
  ───────────────────
  • [UNREACHABLE] app.js.map
```

The scanner finds endpoints, RPC methods, URLs, emails, files, GraphQL
operations, and source-map references. RPC methods are written `Service.Method`
and are found by how the code declares or calls them, not by what the service
is named: RPC paths (`/pkg.Service/Method`, `/Service.Method`), service stubs
that register methods through a helper, Connect-ES service descriptors,
JSON-RPC messages, and dotted names backed by other evidence. When the code
registers a method under a wire id, the id is shown next to it (`wire_id` in
JSON and JSONL). Each RPC method is listed once, here, with the path it was
found through when that says more than its name (`path` in JSON and JSONL), so
it is not repeated under endpoints. Filtering is conservative to keep noise low: module specifiers,
`node_modules` paths, and relative source or chunk files are not reported as
endpoints, while a relative path passed to a request call is.

```bash
# Add parameters and a wordlist built from the target's own routes and RPC methods
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
packages. When a file references a map that was not applied, a line under its
heading says how to load it for that kind of input.

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
Under `purpose`, `platform` names the host (Obsidian plugin, browser
extension, and so on), `features` carry `api_calls` and `screens` with
examples (the `sign-in` feature adds a `folded` list of the areas it gathers), `evidence` holds the totals, and each `runs` entry has an `evidence`
array of `signal`, `uses`, `line`, and `column`; the `API` and `Screens` entries,
and a `Starts` entry taken from the load flow, are counted or read from other
facts and have none. With several files, JSON adds a
top-level `overall` object for the whole set and a `loads` list on each file
that names others; JSONL adds a final record with `"kind": "overall"`.

```bash
jshound --overview --format json -u https://target.example/app.js | jq '.files[].purpose'
jshound --format jsonl --output findings.jsonl ./assets
```

## Flags

| Flag | Description |
| --- | --- |
| `--overview` | Describe each file's type, features, and behavior |
| `--verbose` | With `--overview`, print the summary plus structure, entry paths, and call flow |
| `-u URL` | Analyze an HTTP(S) URL; repeatable |
| `-p` | Include parameters |
| `--wordlist` | Include a wordlist built from routes, RPC methods, and parameters |
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
  does not follow imports, URLs, or remote source maps. Files are linked only
  by names found in the inputs you give.
- Each input checks at most 64 unique source-map references.
- If brackets do not pair up or a literal is left open, the file's notes (in
  `--verbose` and JSON, not the default report) say the overview may be
  incomplete.
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
