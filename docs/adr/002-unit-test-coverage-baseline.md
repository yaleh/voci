# ADR-002: Unit Test Coverage Baseline Policy

**Status**: Accepted
**Date**: 2026-06-30
**Tasks**: TASK-65

---

## Context

The voci codebase had no formal coverage standard. Unit test coverage across four key
packages was below 80%: `internal/asr` (65.7%), `internal/daemon/tunnel` (58.8%),
`internal/wire` (62.2%), `internal/daemon/session` (75.0%), with overall coverage at
74.9%.

Coverage gaps concentrated in error paths:
- `TranscribeMerged` HTTP non-200 responses and JSON parse failures (asr)
- `StartTunnel` (0%) and `StartManagedTunnel` (16.7%) cloudflared startup failures (tunnel)
- `defaultCmdRunner` (0%) and Named Tunnel wiring paths (wire)
- `WriteStatus`/`WriteLock`/`SweepStaleStatuses` I/O error branches (session)

These uncovered paths correspond to production-fault modes — API timeouts, filesystem
permission errors, and external process failures. Without a coverage baseline, regressions
in these error paths could be introduced silently, with no automated signal.

---

## Decision

**Adopt 80% per-package coverage as the baseline threshold** for the four packages
listed above, measured via `go test -coverprofile`.

### Rationale for 80%

- The pre-TASK-65 baseline of 74.9% overall (56–76% per package) left critical error
  paths uncovered.
- 80% is achievable within a single task without adding integration-heavy tests
  (real cloudflared binary, inject delivery, kernel feature detection).
- 100% coverage would require testing paths that are either unreachable in unit tests
  (`crypto/rand` panic, `json.Marshal` failures on simple structs) or require
  subprocess/network mocking frameworks that add maintenance burden.
- 80% is a commonly accepted industry threshold that balances defect detection with
  test maintenance cost.

### Error-path-first principle

Coverage is necessary but not sufficient. The policy prioritizes:
1. Error-path coverage: every `if err != nil` branch must be tested
2. Happy-path coverage: verify expected behavior under normal conditions
3. Integration-path coverage: only where unit-testable without external binaries

---

## Implementation

- **Measurement**: `go test -coverprofile=/tmp/cover.out ./... && go tool cover -func=/tmp/cover.out | tail -1`
- **Per-package checks**: `go test -coverprofile=/tmp/cover-{pkg}.out ./internal/{pkg}/...`
- **Enforcement**: DoD check items in task templates; CI gate (future TASK)

### Exclusions from hard thresholds

| Path | Reason |
|---|---|
| `cmd/voci/main.go` | 3-line entry point, no test files |
| Real `cloudflared` binary calls (`StartTunnel`, `StartManagedTunnel` success paths) | Requires external binary; tested via fake `StartManagedTunnelFn` injection in wire tests |
| `inject/` package delivery paths | Integration-only: requires tmux session or X11 display |
| `pdeathsig_linux.go` | Kernel feature detection; unit-testable but covered separately |
| `scripts/check-siliconflow` | Shell script, not Go |

### Test constraints

- **No external test frameworks**: Use only `testing`, `net/http/httptest`, `os.TempDir()`, and shell-script fakes for subprocess testing.
- **chmod-based tests**: Must call `t.Skip("chmod ineffective as root")` when `os.Getuid() == 0`. CI environments running as root will silently pass chmod-based error-path tests — a known limitation.
- **No testify/mock**: External assertion or mocking libraries add maintenance burden without providing capabilities the standard library lacks for this codebase's testing needs.

---

## Consequences

- **Regression safety net**: Error-path coverage ensures that changes to API error
  handling, tunnel startup, file I/O, and lock management are caught by automated tests.
- **Team alignment**: CLAUDE.md now documents the ≥80% threshold, establishing shared
  expectations for code review and task completion.
- **Maintenance cost**: Each new error branch requires a corresponding test. This is
  intentional — the cost of writing a test is lower than the cost of debugging a
  silent regression.
- **Root-environment gap**: chmod-based I/O error tests are ineffective when tests run
  as root. This is a known limitation documented here; it can be addressed in the
  future by running CI as a non-root user or using user namespaces.
