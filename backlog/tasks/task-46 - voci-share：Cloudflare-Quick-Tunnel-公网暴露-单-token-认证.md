---
id: TASK-46
title: voci --share：Cloudflare Quick Tunnel 公网暴露 + 单 token 认证
status: 'Basic: Done'
assignee: []
created_date: '2026-06-29 14:39'
updated_date: '2026-06-29 15:01'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
以上方案：
- voci --serve 加 --share flag，fork cloudflared Quick Tunnel 子进程，捕获打印的公网 URL，输出给用户。
- 认证用单一 token（不是 user:pass），通过 --share-auth <token> 提供；若未提供则随机生成并打印。
- voci 服务端加 Bearer token middleware，所有请求须带 Authorization: Bearer <token> header。
- cloudflared 二进制为唯一外部依赖，仅在 --share 时使用；无需 Cloudflare 账号（Quick Tunnel 匿名）。
- voci 进程退出时子进程一起退出，不留残留。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: voci --share：Cloudflare Quick Tunnel 公网暴露 + 单 token 认证

## Background

voci serve 目前只监听本地回环地址（默认 127.0.0.1:9474），无法从其他设备访问。用户有时需要在移动设备（手机、平板）上使用浏览器语音界面控制桌面 Claude Code 会话，或在同一局域网外的远程位置使用语音输入。直接暴露到公网需要公网 IP 或复杂的端口转发配置，门槛过高。Cloudflare Quick Tunnel（`cloudflared tunnel --url`）可以在不注册账号、不配置 DNS 的情况下为本地 HTTP 服务创建临时公网 HTTPS URL，适合这种临时共享场景。但一旦暴露到公网，必须有认证机制阻止未授权请求触发本地 ASR 流水线或向 Claude Code 注入命令。单 Bearer token 方案最简洁：无需会话管理，浏览器端一次存储即可复用，且与 Cloudflare Tunnel 透传 HTTP header 的方式完全兼容。

## Goals

1. `voci serve --share` 启动时，自动调用本地已安装的 `cloudflared` 二进制，通过 Quick Tunnel 将 serve 端口暴露到公网，并将生成的公网 HTTPS URL 打印到 stderr。
2. `--share-auth <token>` 接受用户指定的 Bearer token；若未指定，服务端自动生成一个 32 字节随机 hex token 并打印到 stderr（与公网 URL 同行或相邻）。
3. 当 `--share` 启用时，服务端对所有 `/api/*` 路由强制校验 `Authorization: Bearer <token>` header；token 不匹配返回 HTTP 401；静态文件路由（`/`）不要求认证，以便浏览器能正常加载前端 JS/HTML（前端 JS 负责在发请求时附加 token）。
4. `cloudflared` 不在 PATH 中时，`--share` 启动失败并输出明确错误信息，提示安装路径；不影响无 `--share` 的普通 serve 启动。
5. Cloudflare Tunnel 进程与 serve 进程生命周期绑定：serve 退出时（SIGINT/SIGTERM 或 `Start()` 返回）自动终止 `cloudflared` 子进程。

## Proposed Approach

**flag 层（cmd/voci/main.go）**：新增 `--share`（bool）和 `--share-auth`（string）两个 flag，在 `--serve` 分支中读取。若 `--share` 为 true，在调用 `srv.Start(addr)` 前先启动 `cloudflared tunnel --url http://127.0.0.1:<port>` 子进程（`exec.Command`），从其 stderr 解析出形如 `https://*.trycloudflare.com` 的 URL，打印到 stderr，持有 `*exec.Cmd` 以备清理。用 `defer cmd.Process.Kill()` 或 goroutine 监听 context cancel 实现生命周期绑定。

**认证中间件（internal/daemon/server.go 或新文件 internal/daemon/auth.go）**：在 `Server` struct 上增加 `BearerToken string` 字段。`Handler()` 方法中，若 `BearerToken != ""`，用一个包装函数将 `/api/` 前缀的路由包入认证中间件（校验 `Authorization` header，不通过则 401）。静态文件路由不包装，保持不变。中间件实现为独立函数，便于单元测试。

**token 生成**：在 main.go 的 `--share` 分支中，若 `--share-auth` 为空，用 `crypto/rand` 生成 32 字节随机数并以 hex 编码，赋给 `srv.BearerToken`。

**前端适配**：浏览器端 JS（internal/daemon/web/）在 localStorage 中缓存 token，每次 fetch `/api/*` 时附加 `Authorization: Bearer <token>` header。首次访问若 token 为空，显示输入框让用户粘贴 token。这部分改动局限在前端静态文件，不影响 Go 后端接口契约。

## Trade-offs and Risks

**不做的事**：不实现多 token / 用户权限分层；不提供 token 轮换 API；不持久化 token（重启即失效，这是临时共享场景的合理行为）；不在服务端渲染 token 输入 UI（由前端 JS 处理）。

**已知风险**：
- Cloudflare Quick Tunnel URL 是随机子域名，每次重启都会变化，用户需重新分享 URL。这是 Quick Tunnel 的设计限制，属于已知约束，不在本任务范围内解决。
- `cloudflared` 需用户自行安装；若未安装，失败信息需足够清晰（建议打印官方安装文档链接）。
- 静态文件不鉴权意味着攻击者可以看到前端 JS 源码，但前端 JS 本身不包含 token，风险可接受。
- Quick Tunnel 流量经过 Cloudflare 节点，语音音频和转录文本会经由第三方基础设施，用户需知晓此隐私含义（在 stderr 输出中加注提示）。
- Bearer token 以明文在 HTTP header 传输，但 Cloudflare Tunnel 提供 HTTPS 端对端加密，风险可控。

---

# Plan: voci --share：Cloudflare Quick Tunnel 公网暴露 + 单 token 认证

Proposal: docs/proposals/proposal-voci-share-cloudflare-quick-tunnel.md

## Phase A: Bearer token auth middleware（后端）

### Tests (write first)

File: `internal/daemon/auth_test.go` (new, package `daemon`)

Test cases — all must FAIL before implementation:

- `TestBearerMiddleware_AllowsWhenTokenEmpty` — `Server{BearerToken: ""}` wrapping `/api/voice/emit`; any request passes through (200/204).
- `TestBearerMiddleware_AllowsCorrectToken` — POST with `Authorization: Bearer secret42`; handler reached, returns 204.
- `TestBearerMiddleware_Rejects401WhenTokenWrong` — POST with `Authorization: Bearer wrong`; middleware returns 401, body contains "Unauthorized".
- `TestBearerMiddleware_Rejects401WhenHeaderMissing` — POST without Authorization header; returns 401.
- `TestBearerMiddleware_Rejects401WhenSchemeMismatch` — POST with `Authorization: Basic secret42`; returns 401.
- `TestHandler_StaticFilesUnprotected` — `Server{BearerToken: "tok"}` handler; GET `/` (no auth header) returns 200 with `text/html` content type.
- `TestHandler_APIRequiresTokenWhenSet` — `Server{BearerToken: "tok"}` handler; POST `/api/voice/emit` without token returns 401.
- `TestHandler_APIAllowedWithCorrectToken` — same server; POST with correct token returns 204.
- `TestGenerateToken_Is64HexChars` — `GenerateToken()` returns 64-character lowercase hex string with no error.
- `TestGenerateToken_UniqueEachCall` — two calls return different strings.

### Implementation

Files to create/modify:

- **`internal/daemon/auth.go`** (new): export `GenerateToken() (string, error)` using `crypto/rand` (32 bytes → hex); export `BearerMiddleware(token string, next http.Handler) http.Handler` — checks `Authorization: Bearer <token>` header, returns 401 JSON on mismatch; no-op when token is empty.
- **`internal/daemon/server.go`**: add `BearerToken string` field to `Server` struct; in `Handler()`, wrap the three `/api/` routes with `BearerMiddleware(s.BearerToken, ...)` — static file route (`/`) stays unwrapped.

### DoD
- [ ] `go test ./...`
- [ ] `grep -q 'BearerToken' /home/yale/work/voci/internal/daemon/server.go`
- [ ] `grep -q 'BearerMiddleware' /home/yale/work/voci/internal/daemon/auth.go`
- [ ] `! grep -q 'BearerToken' /home/yale/work/voci/internal/daemon/auth.go`

---

## Phase B: --share flag + cloudflared subprocess（cmd/main.go）

### Tests (write first)

File: `cmd/voci/main_test.go` (additions, package `main`)

Test cases — all must FAIL before implementation:

- `TestServeFlag_ShareAuthSetsBearerToken` — `run(["--serve","--share-auth","mytoken"], ...)` with a `startServeFn` stub that captures the server; asserts `BearerToken == "mytoken"` on the `daemon.Server`.
- `TestServeFlag_ShareAutoGeneratesToken` — `run(["--serve","--share"], ...)` with a `startServeFn` stub; asserts `BearerToken` is non-empty 64-char hex string.
- `TestServeFlag_ShareAuthEmptyNoToken` — `run(["--serve"], ...)` without `--share`; asserts `BearerToken == ""`.
- `TestShare_CloudflaredNotFoundReturnsError` — `run(["--serve","--share"], ...)` with a `findCloudflaredFn` stub returning `("", ErrNotFound)` and a `startServeFn` that would succeed; asserts returned error contains "cloudflared".

File: `internal/daemon/tunnel_test.go` (new, package `daemon`)

- `TestParseTunnelURL_ExtractsHTTPS` — `ParseTunnelURL("...https://abc.trycloudflare.com....\n")` returns `"https://abc.trycloudflare.com"`.
- `TestParseTunnelURL_ReturnsEmptyWhenNoMatch` — `ParseTunnelURL("no url here")` returns `""`.

### Implementation

Files to create/modify:

- **`internal/daemon/tunnel.go`** (new): export `ParseTunnelURL(line string) string` using `regexp` to match `https://[^\s]+\.trycloudflare\.com`; export `StartTunnel(ctx context.Context, port int, stderr io.Writer) (*exec.Cmd, string, error)` — looks up `cloudflared` in PATH (`exec.LookPath`), starts `cloudflared tunnel --url http://127.0.0.1:<port>`, reads stderr lines until URL found or timeout (10 s), returns cmd + URL.
- **`cmd/voci/main.go`**: add `--share` (bool) and `--share-auth` (string) flags in `--serve` branch; after flags parsed and before `srv.Start(addr)`, if `--share`: call `GenerateToken` (or use `--share-auth`), assign `srv.BearerToken`; call `daemon.StartTunnel`; print URL + token to `os.Stderr`; `defer cmd.Process.Kill()`. If `cloudflared` not found, return descriptive error immediately. Extract port from `addr` string.

### DoD
- [ ] `go test ./...`
- [ ] `grep -q 'shareFlag' /home/yale/work/voci/cmd/voci/main.go`
- [ ] `grep -q 'shareAuthFlag' /home/yale/work/voci/cmd/voci/main.go`
- [ ] `grep -q 'ParseTunnelURL' /home/yale/work/voci/internal/daemon/tunnel.go`

---

## Phase C: 前端 token 存储与 API 请求附加 header

### Tests (write first)

File: `internal/daemon/static_test.go` (additions to existing file, package `daemon`)

Test cases — all must FAIL before implementation:

- `TestEmbeddedRecorder_HasAuthHeader` — `embeddedFS.ReadFile("web/recorder.js")`; body contains `"Authorization"`.
- `TestEmbeddedRecorder_HasLocalStorageToken` — body contains `"localStorage"` and `"voci_token"`.
- `TestEmbeddedIndex_HasTokenInputUI` — `embeddedFS.ReadFile("web/index.html")`; body contains `"voci-token"` (the token input element id/class).
- `TestHandler_StaticUnprotectedEvenWithToken` — `Server{BearerToken: "tok"}` handler with a live `httptest.NewServer`; GET `/recorder.js` without Authorization header returns 200 (static files are never gated).

### Implementation

Files to modify:

- **`internal/daemon/web/recorder.js`**: add token-loading logic — on first fetch check `localStorage.getItem("voci_token")`; if empty, prompt user for input and save; attach `Authorization: Bearer <token>` header to all `fetch("/api/voice/transcribe", ...)` and `fetch("/api/voice/emit", ...)` calls; also add a helper `apiFetch(url, opts)` wrapper to avoid header repetition.
- **`internal/daemon/web/index.html`**: add a `<div id="voci-token-setup">` section (hidden by default) with a text `<input id="voci-token">` and a save button; toggled visible by recorder.js when token is absent from localStorage.

### DoD
- [ ] `go test ./...`
- [ ] `grep -q 'Authorization' /home/yale/work/voci/internal/daemon/web/recorder.js`
- [ ] `grep -q 'localStorage' /home/yale/work/voci/internal/daemon/web/recorder.js`
- [ ] `grep -q 'voci-token' /home/yale/work/voci/internal/daemon/web/index.html`

---

## Constraints

- `cloudflared` Quick Tunnel URLs change on every restart; users must re-share the URL each time — this is accepted behaviour, not a bug.
- Token is not persisted across server restarts; this is intentional for the temporary-sharing use case.
- Static files (`/`) are deliberately unprotected so the browser can load JS/HTML before the user supplies a token.
- Bearer token is transmitted in plain HTTP header; Cloudflare Tunnel's HTTPS termination provides transport-level protection.
- Cloudflare Quick Tunnel routes traffic through Cloudflare infrastructure; users are informed of this privacy implication via the stderr startup message.
- `--share` does not change default bind host (`127.0.0.1`); `cloudflared` connects to the loopback, and Cloudflare terminates TLS externally.
- Multi-token, token rotation, and permission tiers are out of scope.

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `grep -q 'BearerToken' /home/yale/work/voci/internal/daemon/server.go`
- [ ] `grep -q 'BearerMiddleware' /home/yale/work/voci/internal/daemon/auth.go`
- [ ] `grep -q 'ParseTunnelURL' /home/yale/work/voci/internal/daemon/tunnel.go`
- [ ] `grep -q 'shareFlag\|share-flag\|shareAuth' /home/yale/work/voci/cmd/voci/main.go`
- [ ] `grep -q 'Authorization' /home/yale/work/voci/internal/daemon/web/recorder.js`
- [ ] `grep -q 'localStorage' /home/yale/work/voci/internal/daemon/web/recorder.js`
- [ ] `grep -q 'voci-token' /home/yale/work/voci/internal/daemon/web/index.html`
- [ ] `! grep -q 'BearerToken' /home/yale/work/voci/internal/daemon/web/recorder.js`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Motivation (3-8 lines, explains WHY): Background is 8 sentences explaining mobile/remote use case and why Bearer token fits; not just WHAT.
[E] Goals (numbered, concrete, verifiable): 5 goals with measurable outcomes (HTTP 401, stderr output, process kill, clear error message).
[E] Feasibility (approach aligns with codebase): Server struct in internal/daemon/server.go is directly extensible; Handler() uses plain http.NewServeMux; --serve branch in main.go calls srv.Start(addr) — cloudflared launch slots in cleanly before that call.
[C] Completeness (trade-offs and risks identified): No-multi-token, no persistence, URL rotation, cloudflared install dep, privacy (Cloudflare node), static-file auth gap — all documented.
[H] Consistency (no contradictions): Goal 3 (static unprotected) consistent with Approach (static route not wrapped); Goal 2 (auto-generate) consistent with Approach (crypto/rand hex).
GCL-self-report: E=3 C=1 H=1

Proposal approved. Starting plan draft.

Plan review iteration 1: APPROVED
premise-ledger:
E Goal coverage: all 5 Goals addressed by at least one Phase or Acceptance Gate item
E TDD structure: every Phase has Tests then Implementation sections in correct order
E TDD order: first DoD item in every phase is exactly 'go test ./...'
E Acceptance gate: first item is exactly 'go test ./...'
E DoD executability: all DoD and Acceptance Gate items are shell commands; natural-language items are in Constraints
E Absence checks: '! grep -q' pattern used correctly (not 'grep -qv')
E Phase ordering: A (auth middleware) -> B (CLI+tunnel) -> C (frontend); no circular deps
E Scope discipline: all phases backed by Goals
E File paths: internal/daemon/server.go, cmd/voci/main.go, internal/daemon/web/recorder.js, internal/daemon/web/index.html verified; new files listed as 'create' in Implementation sections
GCL-self-report: E=9 C=0 H=0

claimed: 2026-06-29T14:50:43Z
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
voci --share 功能完成。新增：internal/daemon/auth.go (GenerateToken + BearerMiddleware)，internal/daemon/tunnel.go (ParseTunnelURL + StartTunnel)，--share/--share-auth flags in cmd/voci/main.go，frontend apiFetch() + localStorage token caching in recorder.js，#voci-token-setup overlay in index.html。/api/* routes 受 Bearer token 保护，static / 不受保护。与 master anti-flicker 变更合并解决冲突后，全部测试通过。
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./...
- [ ] #2 grep -q 'BearerToken' /home/yale/work/voci/internal/daemon/server.go
- [ ] #3 grep -q 'BearerMiddleware' /home/yale/work/voci/internal/daemon/auth.go
- [ ] #4 ! grep -q 'BearerToken' /home/yale/work/voci/internal/daemon/auth.go
- [ ] #5 grep -q 'shareFlag' /home/yale/work/voci/cmd/voci/main.go
- [ ] #6 grep -q 'shareAuthFlag' /home/yale/work/voci/cmd/voci/main.go
- [ ] #7 grep -q 'ParseTunnelURL' /home/yale/work/voci/internal/daemon/tunnel.go
- [ ] #8 grep -q 'Authorization' /home/yale/work/voci/internal/daemon/web/recorder.js
- [ ] #9 grep -q 'localStorage' /home/yale/work/voci/internal/daemon/web/recorder.js
- [ ] #10 grep -q 'voci-token' /home/yale/work/voci/internal/daemon/web/index.html
- [ ] #11 grep -q 'shareFlag\|share-flag\|shareAuth' /home/yale/work/voci/cmd/voci/main.go
- [ ] #12 ! grep -q 'BearerToken' /home/yale/work/voci/internal/daemon/web/recorder.js
<!-- DOD:END -->
