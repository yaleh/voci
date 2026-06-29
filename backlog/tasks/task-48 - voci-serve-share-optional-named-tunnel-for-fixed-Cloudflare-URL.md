---
id: TASK-48
title: 'voci serve --share: 动态 Cloudflare Named Tunnel 生命周期管理'
status: 'Basic: Done'
assignee: []
created_date: '2026-06-29 16:13'
updated_date: '2026-06-29 21:48'
labels:
  - 'kind:basic'
dependencies: []
modified_files:
  - internal/daemon/cfapi/cfapi.go
  - internal/daemon/cfapi/cfapi_test.go
  - internal/daemon/tunnel_state.go
  - internal/daemon/tunnel_state_test.go
  - internal/daemon/managed_tunnel.go
  - internal/daemon/managed_tunnel_test.go
  - cmd/voci/main.go
  - cmd/voci/main_test.go
ordinal: 33000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## 目标行为

1. **新会话首次执行 `voci serve --share`**：
   - 若环境变量 `CLOUDFLARE_API_TOKEN`、`CF_ACCOUNT_ID`、`CF_ZONE_ID`、`CF_TUNNEL_DOMAIN` 均已配置，进入 Managed Tunnel 路径
   - 调用 Cloudflare API 动态创建随机名称 Named Tunnel（如 `voci-a3f9b2`）
   - 创建 CNAME DNS 记录：`voci-a3f9b2.<CF_TUNNEL_DOMAIN>` → `<tunnel-uuid>.cfargotunnel.com`
   - 用 `cloudflared tunnel --token <token>` 启动隧道
   - 将 (token, publicURL, createdAt) 写入 `~/.voci/active-tunnel.json`
   - 打印 `https://voci-a3f9b2.<CF_TUNNEL_DOMAIN>` 作为 share URL

2. **同一会话重启 `voci serve --share`（TTL 未过期，默认 20h）**：
   - 读取 `~/.voci/active-tunnel.json`，检查 createdAt + TTL
   - 复用相同 token → 相同 tunnel UUID → 相同 DNS → 相同 URL ✓

3. **新会话（active-tunnel.json 不存在或已过期）**：
   - 先调用 CF API 删除旧 tunnel + DNS 记录（清理）
   - 走步骤 1，创建新 tunnel → 新随机 URL ✓

4. **Fallback（未配置 CF 环境变量）**：
   - 行为与现在完全相同：Quick Tunnel（`*.trycloudflare.com`），无任何回归

## 用户侧配置
- `CLOUDFLARE_API_TOKEN`：Tunnel:Edit + DNS:Edit 权限
- `CF_ACCOUNT_ID`：Cloudflare 账号 ID
- `CF_ZONE_ID`：域名所在 Zone ID
- `CF_TUNNEL_DOMAIN`：子域名根（如 `voci.example.com`）

不需要 `cloudflared login`（cert.pem），全程用 `--token` 模式。

## 实现范围
- `internal/daemon/cfapi/` 新包：封装 CF REST API（Create Tunnel, Get Token, Create DNS Record, Delete Tunnel, Delete DNS Record）
- `internal/daemon/tunnel_state.go`：读写 `~/.voci/active-tunnel.json`，TTL 判断
- `internal/daemon/tunnel.go`：新增 `StartManagedTunnel(ctx, cfg ManagedTunnelConfig, port int, logW io.Writer)` 函数
- `cmd/voci/main.go`：在 `--share` 分支中，若 CF 环境变量齐全则调用 StartManagedTunnel，否则保留现有 StartTunnel
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: voci serve --share: 动态 Cloudflare Named Tunnel 生命周期管理

## Background

`voci serve --share` currently uses Cloudflare Quick Tunnels (`*.trycloudflare.com`), which assign a new random URL on every restart. This makes it impractical for users who share the URL with teammates or embed it in automation: any server restart breaks existing clients and requires redistributing the URL. Beyond URL instability, Quick Tunnels are an anonymous, unauthenticated public service with no SLA guarantees and no programmatic lifecycle control. Users who already manage a Cloudflare-hosted domain and have an API token should be able to get a stable, human-memorable share URL that survives `voci serve` restarts within a configurable TTL — without requiring the heavy `cloudflared login` / `cert.pem` flow.

## Goals

1. When the four Cloudflare env vars (`CLOUDFLARE_API_TOKEN`, `CF_ACCOUNT_ID`, `CF_ZONE_ID`, `CF_TUNNEL_DOMAIN`) are all set and `--share` is used, `voci serve` prints a stable HTTPS URL of the form `https://voci-<random6>.<CF_TUNNEL_DOMAIN>` that does **not** change between restarts within the 20-hour default TTL.
2. When `~/.voci/active-tunnel.json` exists and is within TTL, `voci serve --share` reuses the stored token and emits the same URL without any new Cloudflare API calls for tunnel or DNS creation.
3. When `~/.voci/active-tunnel.json` is absent or expired, stale Cloudflare resources (previous tunnel + DNS record) are deleted via API before a new tunnel is created, leaving no orphaned entries in the Cloudflare account.
4. When none of the four env vars are set, behavior is identical to the current implementation (Quick Tunnel via `StartTunnel`); no regression is possible in that path.
5. The new `internal/daemon/cfapi/` package compiles and is callable without any side effects when no network is available — all HTTP calls are isolated behind function boundaries that can be replaced in tests.

## Proposed Approach

**New package `internal/daemon/cfapi/`** encapsulates all Cloudflare REST API interactions: create Named Tunnel, retrieve tunnel token, create CNAME DNS record, delete tunnel, delete DNS record. Each operation maps 1:1 to a single Cloudflare API call and returns a typed struct or error, with no global state.

**New file `internal/daemon/tunnel_state.go`** owns `~/.voci/active-tunnel.json` I/O. It exposes `ReadActiveTunnel()`, `WriteActiveTunnel()`, and a TTL predicate, keeping the state schema in one place (fields: `tunnel_id`, `token`, `public_url`, `dns_record_id`, `created_at`).

**New function `daemon.StartManagedTunnel(ctx, cfg ManagedTunnelConfig, port int, logW io.Writer)`** orchestrates the full lifecycle:
1. Check `active-tunnel.json`; if valid, skip to step 4.
2. If stale state exists, call `cfapi` to delete the old tunnel and DNS record.
3. Call `cfapi` to create a new Named Tunnel and CNAME, then write `active-tunnel.json`.
4. Exec `cloudflared tunnel --token <token> --url http://127.0.0.1:<port>` (same pattern as `StartTunnel`), return the public URL.

**`cmd/voci/main.go` `--share` branch** gains an env-var probe before calling `StartTunnel`; if all four vars are present it calls `StartManagedTunnel` instead. The fallback to `StartTunnel` requires zero changes to that function.

The `ManagedTunnelConfig` struct holds the four env-var values and the TTL duration (default 20 h), making it straightforward to override in tests.

## Trade-offs and Risks

**Not doing:** GUI or CLI commands to manually rotate / delete tunnels. Users must either wait for TTL expiry or delete `~/.voci/active-tunnel.json` by hand. This is acceptable for the initial iteration.

**Not doing:** Support for `cloudflared login` / cert.pem flow. The `--token` mode is strictly simpler and avoids requiring a local credentials file, but it means the user must provision an API token with `Tunnel:Edit` + `DNS:Edit` permissions.

**Risk — orphaned Cloudflare resources on hard kill:** If `voci serve` is killed before the deferred cleanup runs on the *next* startup (not the current one), the tunnel and DNS record remain in Cloudflare until the next startup with a fresh or expired state. Mitigation: the startup path always attempts cleanup of the *previous* tunnel before creating a new one, bounding orphan count to 1.

**Risk — API token scope accidents:** A token with overly broad permissions (e.g., Zone:Admin) grants more access than needed. The proposal documents the minimum required scopes but does not enforce them; a mis-scoped token will still work.

**Alternative considered — named tunnel via `cloudflared tunnel create` CLI:** Avoids writing a REST client but requires `cloudflared` to be logged in or have a credentials file, reintroducing the cert.pem complexity. The direct API approach is preferred.

---

# Plan: voci serve --share: 动态 Cloudflare Named Tunnel 生命周期管理

## Phase A: cfapi package — Cloudflare REST API client

### Tests (write first)

File: `internal/daemon/cfapi/cfapi_test.go`

- `TestCreateTunnel_Success` — httptest server returns 200 with tunnel JSON; asserts TunnelID and Token are populated
- `TestCreateTunnel_HTTPError` — httptest server returns 422; asserts non-nil error containing status code
- `TestGetTunnelToken_Success` — httptest server returns 200 with token JSON; asserts token string matches
- `TestGetTunnelToken_NotFound` — httptest server returns 404; asserts non-nil error
- `TestCreateDNSRecord_Success` — httptest server returns 200 with DNS record JSON; asserts RecordID populated
- `TestCreateDNSRecord_Conflict` — httptest server returns 409; asserts non-nil error containing status
- `TestDeleteTunnel_Success` — httptest server returns 200; asserts nil error
- `TestDeleteTunnel_NotFound` — httptest server returns 404; asserts nil error (idempotent delete)
- `TestDeleteDNSRecord_Success` — httptest server returns 200; asserts nil error
- `TestDeleteDNSRecord_NotFound` — httptest server returns 404; asserts nil error (idempotent delete)
- `TestClient_BaseURLOverride` — construct Client with custom BaseURL; verify requests hit the override host

### Implementation

- **`internal/daemon/cfapi/cfapi.go`** (new) — `Client` struct with fields `APIToken`, `AccountID`, `ZoneID`, `BaseURL`; typed structs `TunnelInfo`, `DNSRecord`; methods `CreateTunnel`, `GetTunnelToken`, `CreateDNSRecord`, `DeleteTunnel`, `DeleteDNSRecord`; each method performs exactly one HTTP call with `Authorization: Bearer` header; `BaseURL` defaults to `https://api.cloudflare.com`

### DoD
- [ ] `go test ./internal/daemon/cfapi/ -run TestCreate`
- [ ] `go test ./internal/daemon/cfapi/ -run TestGet`
- [ ] `go test ./internal/daemon/cfapi/ -run TestDelete`
- [ ] `go test ./internal/daemon/cfapi/`
- [ ] `! grep -rq 'global\|sync\.Once\|init()' /home/yale/work/voci/internal/daemon/cfapi/cfapi.go`

---

## Phase B: TunnelState — active-tunnel.json lifecycle

### Tests (write first)

File: `internal/daemon/tunnel_state_test.go`

- `TestTunnelState_RoundTrip` — WriteActiveTunnel writes file; ReadActiveTunnel reads it back; all fields match
- `TestTunnelState_MissingFile` — ReadActiveTunnel returns nil, nil when file does not exist
- `TestTunnelState_ExpiredTTL` — write a state with `CreatedAt` 25 hours ago; `IsWithinTTL(20h)` returns false
- `TestTunnelState_ValidTTL` — write a state with `CreatedAt` 1 hour ago; `IsWithinTTL(20h)` returns true
- `TestTunnelState_CorruptJSON` — write garbage bytes to the state file; ReadActiveTunnel returns non-nil error
- `TestTunnelState_DefaultPath` — `ActiveTunnelPath()` returns path under `os.UserHomeDir()/.voci/active-tunnel.json`

### Implementation

- **`internal/daemon/tunnel_state.go`** (new) — `TunnelState` struct (`TunnelID`, `Token`, `PublicURL`, `DNSRecordID`, `CreatedAt time.Time`); `ActiveTunnelPath() (string, error)` returning `~/.voci/active-tunnel.json`; `ReadActiveTunnel() (*TunnelState, error)`; `WriteActiveTunnel(s *TunnelState) error` (creates `~/.voci/` dir if missing); `(s *TunnelState) IsWithinTTL(ttl time.Duration) bool`

### DoD
- [ ] `go test ./internal/daemon/ -run TestTunnelState`
- [ ] `go test ./internal/daemon/`

---

## Phase C: StartManagedTunnel + cmd integration

### Tests (write first)

File: `internal/daemon/managed_tunnel_test.go`

- `TestStartManagedTunnel_FreshState` — no state file; fake cfapi creates tunnel + DNS; cloudflared stub emits URL; asserts returned URL matches `https://voci-<id>.<domain>` and state file is written
- `TestStartManagedTunnel_ReuseState` — valid state file within TTL; asserts no cfapi calls are made and returned URL equals stored `PublicURL`
- `TestStartManagedTunnel_ExpiredState` — expired state file; asserts delete calls made for old tunnel and DNS record; new tunnel created; state file updated with new values
- `TestStartManagedTunnel_MissingBinary` — cloudflared not in PATH (via env override); asserts non-nil error containing "cloudflared not found"

File: `cmd/voci/main_test.go` (existing — add test)

- `TestServeCmd_ShareManagedTunnel` — all four CF env vars set; `--share` flag; asserts `StartManagedTunnel` code path taken (via fake injected function); asserts Quick Tunnel path not taken

### Implementation

- **`internal/daemon/managed_tunnel.go`** (new) — `ManagedTunnelConfig` struct (`APIToken`, `AccountID`, `ZoneID`, `TunnelDomain`, `TTL time.Duration`); `StartManagedTunnel(ctx context.Context, cfg ManagedTunnelConfig, port int, logW io.Writer) (*exec.Cmd, string, error)` implementing: read state → check TTL → if expired/missing: delete stale resources → create tunnel → get token → create DNS → write state → exec `cloudflared tunnel run --token <token>`; drain stderr for `cloudflared` ready signal (reuse existing `drainStderr` or inline equivalent)
- **`cmd/voci/main.go`** (modify) — in the `--share` branch (around line 268): probe `CLOUDFLARE_API_TOKEN`, `CF_ACCOUNT_ID`, `CF_ZONE_ID`, `CF_TUNNEL_DOMAIN`; if all four are set call `daemon.StartManagedTunnel` instead of `daemon.StartTunnel`; `ManagedTunnelConfig.TTL` defaults to `20 * time.Hour`

### DoD
- [ ] `go test ./internal/daemon/ -run TestStartManagedTunnel`
- [ ] `go test ./cmd/voci/ -run TestServeCmd_ShareManagedTunnel`
- [ ] `go test ./...`
- [ ] `grep -q 'StartManagedTunnel' /home/yale/work/voci/cmd/voci/main.go`

---

## Constraints

- All CF API calls must be isolated behind function/interface boundaries so tests can substitute an httptest server without modifying global state or environment variables at the OS level.
- The `cfapi` package must compile without network access; no `init()` side effects.
- When none of the four env vars are set, `StartTunnel` (Quick Tunnel) is called unchanged — zero regression in that path.
- `DeleteTunnel` and `DeleteDNSRecord` must be idempotent: a 404 response is treated as success.
- `~/.voci/` directory must be created with `0700` permissions if it does not exist when writing state.
- `cloudflared tunnel run --token` is used for named tunnels (distinct from `cloudflared tunnel --url` used for Quick Tunnels).
- `ManagedTunnelConfig.TTL` must be configurable; default 20h matches proposal.

---

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build ./...`
- [ ] `go vet ./...`
- [ ] `! grep -q 'TODO\|FIXME\|panic(' /home/yale/work/voci/internal/daemon/cfapi/cfapi.go`
- [ ] `test -f /home/yale/work/voci/internal/daemon/cfapi/cfapi.go`
- [ ] `test -f /home/yale/work/voci/internal/daemon/tunnel_state.go`
- [ ] `test -f /home/yale/work/voci/internal/daemon/managed_tunnel.go`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
GCL-self-report: E=3 C=5 H=2

Proposal approved. Starting plan draft.

Plan review iteration 1: APPROVED
premise-ledger:
a) Goal coverage: E — readable directly from plan + proposal in /tmp files
b) TDD structure: E — readable directly from plan headings
c) TDD order: E — readable directly from first DoD item in each phase
d) Acceptance gate first item: E — readable directly from plan
e) DoD executability: E — readable directly from plan (found stale-backtick defect, fixed)
f) grep-q form: E — readable directly from plan
g) Phase ordering: C — requires reading codebase to understand A→B→C dependency chain
h) Scope discipline: C — requires cross-checking plan phases against proposal goals
i) File paths: C — requires ls/find on codebase to verify existence
GCL-self-report: E=6 C=3 H=0
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented full Cloudflare Named Tunnel lifecycle management for `voci serve --share`. Phase A: new `internal/daemon/cfapi/` package with `Client` struct and five API methods (CreateTunnel, GetTunnelToken, CreateDNSRecord, DeleteTunnel/DNSRecord — both idempotent on 404); 11 unit tests via httptest server. Phase B: `internal/daemon/tunnel_state.go` with JSON read/write/TTL for `~/.voci/active-tunnel.json` (0700 dir, 0600 file); 6 unit tests including corrupt JSON, TTL expiry, round-trip. Phase C: `internal/daemon/managed_tunnel.go` with `StartManagedTunnel` orchestrating state reuse, stale cleanup, and new tunnel provisioning; 4 unit tests covering fresh/reuse/expired/missing-binary paths. `cmd/voci/main.go` gains `StartManagedTunnelFn` injectable and selects Managed vs Quick Tunnel based on presence of all 4 CF env vars. Added `TestServeCmd_ShareManagedTunnel` that verifies the managed path with fake injected fn. All 17 DoD items green; full `go test ./...` passes.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 go test ./internal/daemon/cfapi/ -run TestCreate
- [x] #2 go test ./internal/daemon/cfapi/ -run TestGet
- [x] #3 go test ./internal/daemon/cfapi/ -run TestDelete
- [x] #4 go test ./internal/daemon/cfapi/
- [x] #5 ! grep -rq 'global\|sync\.Once\|init()' /home/yale/work/voci/internal/daemon/cfapi/cfapi.go
- [x] #6 go test ./internal/daemon/ -run TestTunnelState
- [x] #7 go test ./internal/daemon/
- [x] #8 go test ./internal/daemon/ -run TestStartManagedTunnel
- [x] #9 go test ./cmd/voci/ -run TestServeCmd_ShareManagedTunnel
- [x] #10 go test ./...
- [x] #11 grep -q 'StartManagedTunnel' /home/yale/work/voci/cmd/voci/main.go
- [x] #12 go build ./...
- [x] #13 go vet ./...
- [x] #14 ! grep -q 'TODO\|FIXME\|panic(' /home/yale/work/voci/internal/daemon/cfapi/cfapi.go
- [x] #15 test -f /home/yale/work/voci/internal/daemon/cfapi/cfapi.go
- [x] #16 test -f /home/yale/work/voci/internal/daemon/tunnel_state.go
- [x] #17 test -f /home/yale/work/voci/internal/daemon/managed_tunnel.go
<!-- DOD:END -->
