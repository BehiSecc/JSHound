# JSHound

**Understand any JavaScript file in seconds, without running it.**

JSHound is a fast static analyzer for JavaScript and HTML. Point it at a
minified bundle, a single-page app, or a directory of scripts, and it tells
you what the code is, what it does, and how it behaves.

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
| Type | What the file is and what it is built with |
| Features | Product areas in the code, with their actions, API calls, and screens |
| Behavior | What the code does at run time |

Every line comes from evidence in the code.

- **Several files.** Pass several files or a directory and JSHound describes
  them as one application, then lists each file. Imports are never fetched.
- **Source maps.** An inline map, or a map file next to a local input, is
  applied automatically.
- **More detail.** `--verbose` adds evidence locations, bundled modules,
  dispatch points, call flow, imports, exports, and UI routes:

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

RPC methods are found by how the code declares or calls them, and each is
listed once as `Service.Method`. Emails and GraphQL operations are reported
too.

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

## Secret scanning

Secret detection is opt-in and uses
[MongoDB Kingfisher](https://github.com/mongodb/kingfisher) 1.110.0 or newer,
which runs only when you pass `--kingfisher PATH`:

```bash
jshound --kingfisher /path/to/kingfisher app.js
jshound --kingfisher /path/to/kingfisher --validate-secrets app.js
```

Findings include the secret value, so treat the output as sensitive.
`--validate-secrets` checks credentials against provider APIs; use it only
where you are authorized to.

## Usage

JSHound reads JavaScript or HTML (inline scripts included) from:

| Input | Example |
| --- | --- |
| Files | `jshound app.js index.html` |
| Directories, recursively | `jshound ./assets` |
| URLs, repeatable | `jshound -u https://a.example/app.js -u https://a.example/vendor.js` |
| Standard input | `cat app.js \| jshound --overview` |

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
Kingfisher flags). In JSON overview output, each file's summary is under
`purpose`, and a set of files adds an `overall` object:

```bash
jshound --overview --format json ./assets | jq '.files[].purpose'
```

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
