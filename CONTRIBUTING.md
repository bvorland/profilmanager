# Contributing to profilmanager

Short doc, focused on the bits that bite newcomers. For the bigger picture, start with `README.md` and the docs under `docs/`.

---

## Build & test

```sh
go build ./...
go test ./...
```

That's the contract. Both must pass on Windows, macOS, and Linux. CI matrix is `{ubuntu, macos, windows} × {go 1.25.x, stable}` (`go.mod` requires Go 1.25+).

Race detector is on by default:

```sh
go test -race -timeout 5m ./...
```

If you add a test that fundamentally cannot run under `-race` (you should not), document why in the test file and skip with `if testing.Short()` or a build tag, not by disabling race for the whole package.

---

## Golden tests (`-update` flow)

`internal/cli/golden_test.go` and `internal/tui/view_*_test.go` use the standard Go `-update` golden pattern. A flag declared once per package:

```go
var updateGolden = flag.Bool("update", false, "update golden files instead of asserting against them")
```

### When you've intentionally changed CLI `--json` output or TUI rendering

1. Run with `-update`:
   ```sh
   go test ./internal/cli -run TestGolden -update
   go test ./internal/tui -run TestRender -update
   ```
2. Diff the regenerated `testdata/golden/*.json` and `testdata/*.golden` files.
3. **Read the diff.** If anything surprises you, your change has unintended scope — pull it back before committing.
4. Commit golden files alongside the code change in the same PR. Reviewers diff goldens to understand user-visible impact; orphaning them across PRs breaks that signal.

### When `-update` would change goldens but you didn't intend to change anything

That's a bug or a flaky test. Common causes:

- **Embedded timestamp / PID / random ID.** All goldens must be deterministic. The normalization helper in `internal/cli/golden_test.go` masks `path`, `message`, `fix`, and `error` keys, and sorts JSON map keys. If you add a field that varies run-to-run, extend the mask list.
- **TTY assumptions in TUI snapshots.** Tests run under `lipgloss.SetColorProfile(termenv.Ascii)` and `NO_COLOR=1`. If you `os.Getenv("TERM")` from inside a model, expect snapshot drift between local and CI — gate on the env via the model's `colorProfile` field, not direct OS reads.
- **Map iteration order.** `range map` is randomized. Sort keys before rendering.
- **Locale-sensitive formatting** (number separators, date formats). Pin to `"en-US"` or to a culture-free format.

Locally, do the same check before pushing:

```sh
go test -count=1 -run 'TestGolden|TestRender' ./internal/cli/... ./internal/tui/...
git diff --exit-code internal/cli/testdata internal/tui/testdata
```

---

## Integration tests (`-tags integration`)

`internal/cli/integration_test.go` builds the real `pm` binary, drops fake `az`/`azd`/`gh`/`kubectl`/`git` scripts on a shimmed PATH, and runs end-to-end. The pattern mirrors `internal/providers/fakecli_test.go` (the package-level unit-test version).

Run them:

```sh
go test -tags integration -count=1 ./internal/cli/...
```

POSIX-only for v1. On Windows the test file compiles and every test calls `t.Skip` — the fake CLIs are bash scripts and the path-quoting tax on Windows isn't worth it for v1. Integration concerns specific to Windows are covered by:

- the isolation probes in `scripts/isolation/` (run via `pwsh scripts/isolation/run-matrix.ps1`);
- unit tests in `internal/providers/` that fake the CLIs in-process.

### Adding a fake-CLI integration test

1. Pick a verb. Decide what env / args the fake should observe.
2. Extend `writeFakeProviders` in `integration_test.go` with the new behavior (a new subcommand handler in the existing shell script, or a new fake binary).
3. Write the test as `TestIntegration<Name>`, gated with `requireUnix(t)` + `if testing.Short() { t.Skip(...) }`.
4. Assert on the fake's recorded output (each fake writes its observed env + args to a per-call log file).

### Adding integration coverage for not-yet-shipped verbs

If you write a test for a verb that isn't wired yet, call `t.Skip("not yet wired: <issue or todo reference>")` and leave a TODO. Do not delete the test — the skip becomes the to-do list when the verb lands.

---

## Isolation probes (`scripts/isolation/`)

Two naming styles, both first-class:

| Style       | Layout                                                | When to use                                                                 |
| ----------- | ----------------------------------------------------- | --------------------------------------------------------------------------- |
| Flat        | `scripts/isolation/probe-<name>.{ps1,sh}`             | Single-file probe, no fixture data.                                         |
| Package     | `scripts/isolation/<name>/probe.{ps1,sh}` + siblings  | Probe needs `expected.json`, fixtures, or multiple helper scripts.          |

Both are auto-discovered by `run-matrix.ps1` and `run-matrix.sh`. Probes must:

- Source `_common.ps1` or `_common.sh` for the shared envelope helpers.
- Emit exactly one JSON object on stdout matching `isolation-matrix/v1` schema.
- Exit 0 (the matrix runner aggregates `isolated` from the JSON; exit code is reserved for harness errors).
- Skip cleanly if the tool isn't on PATH (`Skipped = $true`, `SkipReason = '<tool> not found on PATH'`).

See `scripts/isolation/windows-azd-shellout/` for a worked example with a sibling `expected.json` JSON Schema doc.

---

## Style

- `gofmt -l .` must be empty. CI enforces this — run `gofmt -w .` before committing.
- `go vet ./...` must be clean.
- Linter beyond `gofmt` + `vet` is deliberately deferred; `golangci-lint` has not been adopted for v1.
- Commits: imperative mood (`Add x`, `Fix y`, not `Added x` / `Fixes y`). Reference issues with `#NN` in the body, not the subject.

