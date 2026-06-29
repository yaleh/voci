---
id: TASK-48
title: 'voci serve --share: optional named tunnel for fixed Cloudflare URL'
status: 'Basic: Backlog'
assignee: []
created_date: '2026-06-29 16:13'
updated_date: '2026-06-29 16:15'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 33000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
以上建议的机制：voci serve --share 增加可选的 --tunnel <name> 参数。提供时使用 cloudflared named tunnel（固定 URL，重连后恢复同一 hostname）；不提供时 fallback 到 quick tunnel（当前行为，每次重连得到新随机 URL）。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: voci serve --share: optional named tunnel for fixed Cloudflare URL

## Background
`voci serve --share` currently always starts a Cloudflare Quick Tunnel
(`cloudflared tunnel --url`). Quick tunnels assign a random `trycloudflare.com`
subdomain on every connection and re-assign a new one on every reconnect, because
the subdomain is tied to the tunnel connection's session — not to any persistent
identity. When the voci-listen monitor restarts (e.g. after a crash or `voci serve`
port conflict), a new URL is generated and must be re-shared with the mobile device.
A Cloudflare Named Tunnel has a credentials file at `~/.cloudflared/<id>.json` and a
user-configured DNS hostname; on reconnect, cloudflared re-establishes the same
hostname. Adding `--tunnel <name> --tunnel-host <hostname>` lets users who have
already set up a named tunnel get a fixed, stable URL.

## Goals
1. `voci serve --share --tunnel <name> --tunnel-host <host>` starts `cloudflared tunnel run`
   instead of `cloudflared tunnel --url`, and prints `https://<host>` as the share URL.
2. Passing `--tunnel` without `--tunnel-host` (or vice versa) returns a clear error
   and exits non-zero before starting the server.
3. Omitting both `--tunnel` and `--tunnel-host` preserves current quick-tunnel behavior
   exactly (no regression).
4. `internal/daemon.StartNamedTunnel()` is unit-testable: command-line construction
   and ready-detection logic are independently verifiable without spawning a real
   cloudflared process.

## Proposed Approach
Add `StartNamedTunnel(ctx, tunnelName, port, logW)` to `internal/daemon/tunnel.go`
alongside the existing `StartTunnel`. It runs
`cloudflared tunnel run --url http://127.0.0.1:<port> <name>` and scans stderr for
"Connection established" to signal readiness (15 s timeout, same as quick tunnel).
Add `--tunnel` and `--tunnel-host` string flags to `cmd/voci/main.go`; validate
mutual presence; branch on whether they are set to call `StartNamedTunnel` vs
`StartTunnel`; print `https://<tunnel-host>` instead of the scraped URL.
`ParseNamedTunnelReady(line string) bool` is extracted for unit-testability.

## Trade-offs and Risks
- We do NOT look up the hostname automatically from `cloudflared tunnel info` — user
  must supply `--tunnel-host`. Simpler implementation; avoids a subprocess round-trip.
- We do NOT manage cloudflared config files — the user must have already run
  `cloudflared tunnel route dns <name> <host>` once. Out of scope.
- Named tunnel reconnect restores the same hostname because cloudflared uses the
  stored credentials, not the session; this is the core value of the feature.
- Quick tunnel behavior is entirely unchanged; no existing tests are affected.

---

# Plan: voci serve --share: optional named tunnel for fixed Cloudflare URL

## Phase A: StartNamedTunnel and ready-detection in tunnel.go

### Tests (write first)
File: `internal/daemon/tunnel_test.go`
- `TestParseNamedTunnelReady_DetectsConnection` — `ParseNamedTunnelReady("INF Connection established connIndex=0 location=SJC")` returns true
- `TestParseNamedTunnelReady_IgnoresOtherLines` — returns false for unrelated log lines
- `TestNamedTunnelCmd_Args` — `namedTunnelCmd("cloudflared", "baime", 9474)` returns a command whose `Args` slice contains `"tunnel"`, `"run"`, `"--url"`, `"http://127.0.0.1:9474"`, `"baime"` (test the constructor, not the live process)

### Implementation
File: `internal/daemon/tunnel.go`
- Add `ParseNamedTunnelReady(line string) bool` — returns true when line contains "Connection established"
- Add unexported `namedTunnelCmd(bin, name string, port int) *exec.Cmd` — constructs `cloudflared tunnel run --url http://127.0.0.1:<port> <name>`
- Add `StartNamedTunnel(ctx context.Context, tunnelName string, port int, logW io.Writer) (*exec.Cmd, error)` — looks up cloudflared binary, calls `namedTunnelCmd`, starts process, scans stderr for `ParseNamedTunnelReady`, 15 s timeout

### DoD
- [ ] `go test ./internal/daemon/ -run TestParseNamedTunnelReady`
- [ ] `go test ./internal/daemon/ -run TestNamedTunnelCmd`

## Phase B: --tunnel / --tunnel-host flags in cmd/voci/main.go

### Tests (write first)
File: `cmd/voci/main_test.go`
- `TestServeCmd_TunnelWithoutHost_ReturnsError` — parse flags with `--tunnel=baime` but no `--tunnel-host`; `runServe()` returns a non-nil error containing "tunnel-host"
- `TestServeCmd_TunnelHostWithoutTunnel_ReturnsError` — symmetric: `--tunnel-host` without `--tunnel` returns error containing "tunnel"
- `TestServeCmd_NoTunnelFlags_NoError` — neither flag set; no validation error (quick-tunnel path unchanged)

### Implementation
File: `cmd/voci/main.go`
- Add `tunnelFlag := fs.String("tunnel", "", "cloudflared named tunnel name (requires --tunnel-host)")` and `tunnelHostFlag := fs.String("tunnel-host", "", "public hostname for --tunnel (e.g. voci.example.com)")`
- After flag parse: if exactly one of the two is non-empty, return error
- In `--share` branch: if both set → call `daemon.StartNamedTunnel(tunnelCtx, *tunnelFlag, port, os.Stderr)`, set `publicURL = "https://"+*tunnelHostFlag`; otherwise keep existing `daemon.StartTunnel()` call

### DoD
- [ ] `go test ./cmd/voci/ -run TestServeCmd_Tunnel`
- [ ] `go test ./...`

## Constraints
- Named tunnel requires `cloudflared` already configured with `tunnel route dns`; this skill does not configure cloudflared
- No changes to `StartTunnel`, `ParseTunnelURL`, or existing quick-tunnel tests
- `StartNamedTunnel` is not called in any existing test; new tests use `namedTunnelCmd` (not the live function) to avoid requiring cloudflared in CI

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `voci serve --share --tunnel=x 2>&1 | grep -q "tunnel-host"`
<!-- SECTION:PLAN:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/daemon/ -run TestParseNamedTunnelReady
- [ ] #2 go test ./internal/daemon/ -run TestNamedTunnelCmd
- [ ] #3 go test ./cmd/voci/ -run TestServeCmd_Tunnel
- [ ] #4 go test ./...
- [ ] #5 voci serve --share --tunnel=x 2>&1 | grep -q tunnel-host
<!-- DOD:END -->
