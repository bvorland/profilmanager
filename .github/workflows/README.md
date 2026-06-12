# `.github/workflows/`

Index of GitHub Actions workflows in this repository, what they do, and how to run their equivalents locally.

## Workflows

| File                       | Triggers                                                 | Purpose                                                                                                                |
| -------------------------- | -------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `ci.yml`                   | `push` to any branch, `pull_request`                     | Lint, test, and build the Go module on the OS × Go matrix. Uploads `pm` / `pm.exe` as build artifacts.                 |
| `release.yml`              | tag push matching `v*`                                   | Tag-driven release pipeline. Runs `go test ./...` then `goreleaser --clean` to produce archives, checksums, and a GitHub Release. |

## `ci.yml` — at a glance

- **Matrix:** `{ubuntu-latest, macos-latest, windows-latest}` × `{Go 1.25.x, Go stable}` (`go.mod` requires Go 1.25+).
- **Jobs (in order):**
  1. `lint`   — `gofmt -l .` (fails on any output) + `go vet ./...`
  2. `test`   — `go test -race -timeout 5m ./...`
  3. `build`  — `go build -o pm[.exe] ./cmd/pm`, upload artifact `pm-<os>-go<ver>`.
- **Action pins:** `actions/checkout@v4`, `actions/setup-go@v5`, `actions/upload-artifact@v4`. Pinned to major versions, never `@latest`.
- **Concurrency:** older runs on the same ref are cancelled when a newer commit lands.

`golangci-lint` is intentionally NOT in this workflow yet. `gofmt` + `go vet` are the v1 baseline.

## `release.yml` — at a glance

- **Trigger:** any `vX.Y.Z` tag push.
- **Permissions:** `contents: write` only (no `packages:` or `id-token:` until cosign is wired).
- **Concurrency:** `release-<ref>` group with `cancel-in-progress: false` so a half-published release is never abandoned.
- **Not yet wired:** cosign / sigstore signing, Homebrew/Scoop tap auto-PRs. See the top-of-file comment in `release.yml` for the contract.

## Local equivalents

Run these on your laptop to mirror what CI will run. Everything below should pass before pushing.

### From the repo root

```bash
# Same as the lint job:
gofmt -l .              # must produce NO output
go vet ./...

# Same as the test job:
go test -race -timeout 5m ./...

# Same as the build job (Linux/macOS):
go build -o pm ./cmd/pm

# Windows (PowerShell):
go build -o pm.exe ./cmd/pm
```

### Quick "did I break anything" pass

```bash
gofmt -l . && go vet ./... && go test -race -timeout 5m ./... && go build ./cmd/pm
```

If that command exits non-zero, CI will fail too.

### Isolation matrix (diagnostic harness, not part of CI v1)

```pwsh
# PowerShell — runs all probes
pwsh -NoProfile -File scripts/isolation/run-matrix.ps1 -OutputFile isolation-report.json
```

```bash
# bash — runs the cross-platform subset
bash scripts/isolation/run-matrix.sh --output isolation-report.json
```

See [`docs/isolation-matrix.md`](../../docs/isolation-matrix.md) for what each probe verifies and how to interpret the report.

## Adding a new workflow

1. Drop the YAML in this directory.
2. Update the table above.
3. If it triggers on anything other than `push` / `pull_request` / `workflow_dispatch`, document why in the workflow's top-of-file comment.
4. Pin every `uses:` to a major version (`@v4`), never `@latest` or floating `main`.
