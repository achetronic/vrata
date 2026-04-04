# Vrata Codebase Audit Plan

This document tracks the massive code audits performed on the Vrata codebase to ensure robustness, concurrency safety, error handling, and strict adherence to project conventions.

## Audit 1: Core Engine & Controller Architecture
*Status: Completed*
*Date: 2026-04-03*
*Auditor: Gemini 3.1 Pro*

This first comprehensive audit was divided into four targeted iterations to cover all critical areas of the proxy and controller:

### Iteration 1: Concurrency, Goroutine Leaks, and Context Management
**Objective**: Identify memory leaks, deadlocks, and orphaned contexts.
**Focus Areas**:
- Traced all `go func()` spawns (especially in the proxy, `mirrorRequest`, `ExtProc`, and `Raft`).
- Reviewed all uses of `context.Background()` and `context.TODO()`.
- Verified that all `context.WithTimeout` and `context.WithCancel` calls have their respective `defer cancel()`.
- Searched for potential deadlocks in `sync.Mutex` and `sync.RWMutex` usage (e.g., double locks, missing unlocks on early returns).

### Iteration 2: Error Handling, Logging (slog), and API Conventions
**Objective**: Eliminate silent errors, ensure exclusive use of `slog`, and structure API errors.
**Focus Areas**:
- Searched for `_ = err` or unchecked error assignments.
- Tracked functions returning errors where the caller ignores them (e.g., in cleanup routines or `defer r.Body.Close()`).
- Checked for illegal uses of `fmt.Println`, `fmt.Printf`, `log.Print` (ensuring exclusive use of `slog`).
- Validated that API errors use `respond.Error` and do not expose raw Go errors to the client, strictly adhering to `CONVENTIONS.md`.
- Verified proper error wrapping (`fmt.Errorf("...: %w", err)`).

### Iteration 3: Data Atomicity, Race Conditions, and Global State
**Objective**: Guarantee data integrity in memory and on disk.
**Focus Areas**:
- Reviewed `bbolt` transactions in `server/internal/store/bolt`. Checks (e.g., duplicate routes) and writes must occur within the *same* transaction (`Update`).
- Searched for global variables with mutable state that break the "No global state" rule (validated the append-only `regexCache` justification).
- Reviewed atomic loading of the routing table (`atomic.Pointer[RoutingTable]`) to ensure no dirty reads occur.
- Validated load balancer state consistency (healthy vs. unhealthy endpoints under high concurrency).

### Iteration 4: Edge Cases, HTTP Protocols, and Middlewares
**Objective**: Ensure strict compliance with HTTP/gRPC protocols and middleware robustness.
**Focus Areas**:
- Memory leaks in buffers: Reviewed `celBodyMaxSize` and full read operations (`io.ReadAll`) in middlewares like `ExtAuthz` and CEL validations to prevent OOM (Out Of Memory) attacks.
- Header rewrite validation (append vs. replace behavior).
- Proxy behavior for WebSockets and Server-Sent Events (SSE).
- Strict timeouts on external calls (JWKS, ExtAuthz, ExtProc) to prevent proxy hangs when upstream dependencies are unresponsive.

---
*Future audits will be appended to this document as new architectural phases are completed.*
