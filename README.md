# JSHound

**Understand any JavaScript file in seconds, without running it.**

JSHound is a fast static analyzer for JavaScript and HTML. Point it at a
minified bundle, a single-page app, or a directory of scripts, and it tells
you what the code is, what it does, and how it behaves. It also extracts
endpoints, RPC methods, URLs, source maps, and secrets for security review.

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![Go 1.24+](https://img.shields.io/badge/go-1.24%2B-00ADD8.svg)

- **Plain answers.** `--overview` describes a file, or a set of files as one
  application, in three short sections: type, features, and behavior.
- **Works on minified code.** It reads the names and strings that survive
  minification, and applies a local or inline source map when one exists.
- **Leads for review.** The scanner lists endpoints, RPC methods, URLs,
  parameters, GraphQL operations, and source maps, and can hand files to
  Kingfisher for secret detection.
- **Safe by design.** Analyzed code is never executed.

## Install

JSHound needs Go 1.24 or newer.

```bash
go install github.com/BehiSecc/JSHound/cmd/jshound@latest
```

Or build from source:

```bash
git clone https://github.com/BehiSecc/JSHound.git
cd JSHound
go build -o jshound ./cmd/jshound
```

Secret scanning also needs
[Kingfisher](https://github.com/mongodb/kingfisher) 1.110.0 or newer.

## Quick start

```bash
# What is this bundle and what does it do?
jshound --overview -u https://target.example/static/js/main.js

# What is this set of chunks as one application?
jshound --overview ./assets

# Which endpoints, RPC methods, and URLs does it contain?
jshound -u https://target.example/static/js/main.js

# Recover the original sources from its source map
jshound -s https://target.example/static/js/main.js --output ./source
```

## Overview

`--overview` prints three sections for each file. Output for a sample
project-management app:

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

| Section | What it shows |
| --- | --- |
| Type | What kind of file it is and what it is built with: runtime, framework, build tool, or host such as a browser extension, Obsidian plugin, VS Code extension, or Electron app. |
| Features | Product areas named in the code, the actions found next to them, and the API calls and screens that back each one. Sign-in flows are grouped into one `sign-in` area. |
| Behavior | What the code does at run time: how it starts, screens, server calls and auth headers, the API surface, sign-in methods, uploads, storage, extension APIs, background work, and third-party SDKs. |

- **Evidence only.** Every line comes from the code. A feature area needs more
  than matching names, and a claim resting on one ordinary word is left out.
- **A summary, not an inventory.** The `API` line gives the total and the
  largest groups; scan mode lists every endpoint and RPC method.

### Several files

Pass several files or a directory and JSHound describes the set as one
application, then lists the files. Imports are never fetched; only the files
you pass are read.

```text
# JavaScript overview

8 sources · 10.0 KiB

## All 8 files

### Type

Browser app.

- 8 files (10.0 KiB): 5 scripts, 3 libraries
- Entry file: main.js (loads 7 files and no other file loads it; mounts a UI into #root)

### Features
...
### Behavior
...

## Files

- main.js · 709 B · Browser app · behavior: starts, screens, server calls, storage · entry file
- account.js · 2.9 KiB · Browser script · sign-in
- timesheets.js · 2.1 KiB · Browser script · timesheet
- projects.js · 2.0 KiB · Browser script · project
- invoices.js · 1.9 KiB · Browser script · invoice
- vendor.js · 271 B · The react 18.2.0 library for the browser, named by its licence banner · behavior: storage

Nothing to report in 2 files:

- 2 libraries · 211 B
```

Features and Behavior (shortened above) cover the whole set. Up to five files
get a full section each; larger sets get the compact index shown here. Files
that have nothing to do with each other are reported as separate, not merged.

### Detailed report

`--verbose` prints the same three sections, then the detail behind them:

- where each behavior was first seen, and how often;
- bundled modules, from esbuild markers or a source map;
- dispatch points: switches, handler maps, and route registrations;
- entry paths and call flow, drawn as a flowchart when large enough;
- imports, exports, and UI routes.

Minified names are counted rather than listed, unless a source map restores
them. Locations are one-based lines and byte columns.

```bash
jshound --overview --verbose --output overview.md app.js
```

## Scan

Without `--overview`, JSHound lists concrete leads:

```text
$ jshound app.js

JSHound dev  │  7 findings  │  2 endpoints  │  1 RPC methods  │  2 URLs  │  1 files  │  1 maps
────────────────────────────────────────────────────────────

◆ ENDPOINTS (2)
  ─────────────────
  • /api/orders
  • /users?id=

⇄ RPC METHODS (1)
  ───────────────────
  • OrderService.CreateOrder  (/shop.OrderService/CreateOrder, id aB3xYz)

◇ URLS (2)
  ────────────
  • https://api.example.com/v2
  • wss://rt.example.com/socket

▧ FILES (1)
  ─────────────
  • ./chunk-4F2A9C1D.js

⌁ SOURCE MAPS (1)
  ───────────────────
  • [UNREACHABLE] app.js.map
```

- **RPC methods** are found by how the code declares or calls them, whatever
  the service is called. Each is listed once as `Service.Method`, with its
  path and wire id when the code has them.
- **Endpoints** leave out module specifiers, `node_modules` paths, and source
  or chunk files.
- **Also reported:** emails, files, GraphQL operations, and source-map
  references.

```bash
# Add parameters and a wordlist built from the target's own routes and RPC methods
jshound -p --wordlist app.js

# Scan a directory recursively (skips .git and node_modules)
jshound ./assets
```

## Source maps

Scans report every source-map reference as `valid`, `invalid`, or
`unreachable`. To recover the original sources from a JavaScript file or a
map:

```bash
jshound -s https://target.example/app.js --output ./source
jshound -s https://target.example/app.js.map --output ./source
jshound --overview ./source
```

The output directory must be new or empty. JSHound writes the sources
embedded in the map and never fetches the paths the map points to.

`--overview` applies a map by itself when that needs no network access: an
inline map, or a map file next to a local input. Otherwise it says how to
load it.

## Secret scanning

Secret detection is opt-in and uses
[MongoDB Kingfisher](https://github.com/mongodb/kingfisher), which runs only
when you pass `--kingfisher PATH`:

```bash
jshound --kingfisher /path/to/kingfisher app.js
jshound --kingfisher /path/to/kingfisher --validate-secrets app.js
```

Findings include the secret value, so treat the output as sensitive.
`--validate-secrets` checks credentials against provider APIs; use it only
where you are authorized to. Kingfisher's Base64 scanning is disabled.

## Input and output

JSHound reads JavaScript or HTML (inline scripts included) from:

| Input | Example |
| --- | --- |
| Files | `jshound app.js index.html` |
| Directories, recursively | `jshound ./assets` |
| URLs, repeatable | `jshound -u https://a.example/app.js -u https://a.example/vendor.js` |
| Standard input | `cat app.js \| jshound --overview` |

Results print as text. Use `--format json` or `--format jsonl` for scripts,
and `--output FILE` to write to a file. In overview mode each file's summary
is under `purpose`, and a set of files adds an `overall` object.

```bash
jshound --overview --format json -u https://target.example/app.js | jq '.files[].purpose'
jshound --format jsonl --output findings.jsonl ./assets
```

## Flags

| Flag | Description |
| --- | --- |
| `--overview` | Describe each file's type, features, and behavior |
| `--verbose` | With `--overview`, add evidence, structure, and call flow |
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

`--overview` ignores the scan-only flags (`-p`, `--wordlist`, and the
Kingfisher flags).

## Safety and limits

- Analyzed JavaScript is never executed.
- Overview mode makes no network requests beyond fetching the `-u` inputs.
- Input and source-map sizes are capped by `--max-size`, and requests by
  `--timeout`.
- Each input checks at most 64 source-map references.

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
program licensed under Apache-2.0.
