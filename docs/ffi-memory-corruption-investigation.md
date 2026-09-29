# Investigation: ksqueegee agent memory corruption via the scrapligo/libscrapli FFI boundary

**Status:** ROOT CAUSE FOUND AND FIXED (section 7). Pending production soak.
**Last updated:** 2026-09-29
**Scope:** `kentik/scrapligo`, `kentik/libscrapli` (fork), `kentik/ksqueegee`

---

## 1. TL;DR

A long-running `ksqueegee-agent` process died every ~20–120 minutes with an unrecoverable
whole-process memory-corruption fault. gRPC was the usual victim, not the culprit.

**Root cause:** libscrapli initialises Zig's `std.Io.Threaded`, which installs process-wide no-op
handlers for **SIGPIPE** and **SIGIO** *without* `SA_ONSTACK`. Go requires every non-Go signal
handler to use `SA_ONSTACK`. When a Go goroutine's `write` hits a closed peer (EPIPE — routine for
gRPC/HTTP2), the kernel raises SIGPIPE and, lacking `SA_ONSTACK`, pushes the ~3.4 KB signal frame
onto the **goroutine's own small stack** instead of Go's signal stack. That overflows the stack
and overwrites the neighbouring goroutine's memory.

The recurring `{0x7f…df0, 0x7}` pattern seen in every dump is not a Zig slice. It is the first two
words of a Linux x86-64 `rt_sigframe`: `pretcode` (the signal-return trampoline) and
`uc_flags = 7`. Section 7 has the full decode.

**Fix:** set `SA_ONSTACK` on those handlers. This is done in libscrapli (root cause) and in
scrapligo (defence in depth, which also covers already-released libscrapli builds). Section 9
has the details.

---

## 2. Symptoms

Unrecoverable fatal faults — `recover()` cannot catch any of these, so ksqueegee's
`recover()` in `EnsureLibscrapli` provides no protection whatsoever for this failure mode.

| Dump | Fault | Uptime | Victim |
|---|---|---|---|
| `runtime_error_log.txt` | `fatal error: unknown pc`, return PC `0x7` | 17 min | grpc `HandleStreams` |
| `segmentation_fault.log` | SIGSEGV in `runtime.scanstack` (GC) | 38 min | GC mark worker |
| `segmentation_fault2.log` | `growslice` → `memmove`, non-canonical ptr | 37 min | `fmt.Sprintf` buffer |
| `crash.txt` | `pc == addr == 0xc0001bfc80`, `code=0x2` | 44 min | grpc `HandleStreams` |
| `panic1.txt` / `panic2.txt` | `pc == addr == 0xc000aa2c80`, `code=0x2` | 21 min | grpc `HandleStreams` |
| `panic3.txt` | `runtime.sellock`, `code=0x80 addr=0x0` | 118 min | **`runtime.selectgo`** |

Notes on reading these:

- `code=0x2` is `SEGV_ACCERR`. In `crash.txt`/`panic1`/`panic2` the faulting **PC equals the fault
  address**, and that address is in the Go heap/stack arena (`0xc000…`). The CPU *jumped into data
  and tried to execute it* — a control-flow hijack from a clobbered return address.
- `code=0x80` is `SI_KERNEL` with `addr=0x0`. On x86-64 Linux that is the signature of a
  **non-canonical address** access (a `#GP` reported as SIGSEGV), not a literal null dereference.
  So in `segmentation_fault2.log` and `panic3.txt` a pointer was genuinely garbage, not just null.
- `panic1.txt` and `panic2.txt` are **byte-identical** — the same incident captured twice, not two
  independent events. Treat them as one data point.
- Goroutine 1 is always parked in `errgroup.Wait` (`run.go:35`). Every crash is steady-state,
  mid-scrape, never at startup.

---

## 3. What was tried, in order, and what happened

### Round 1 — raise the agent log level
**Rationale:** believed the purego logger callback was only registered at debug/trace.
**Result:** ✗ Wrong premise. `Options.Apply` set `opts.loggerCallback` *unconditionally*,
regardless of whether a logger was configured. Raising the level only changed
`opts.loggerLevel`; the C function pointer was still handed to Zig. Never deployed.

### Round 2 — `CGO_ENABLED=1` (ksqueegee `63c0633`)
**Rationale:** with `CGO_ENABLED=0` purego falls back to **fakecgo**, which does not export
`_cgo_getstackbound`. `runtime.callbackUpdateSystemStack` (Go 1.25 `runtime/cgocall.go:264-271`)
then *fabricates* the extra M's g0 stack bounds as `[sp-32KB, sp+1024]` with
`g0StackAccurate = false`. That is a plausible corruption mechanism for foreign-thread callbacks.
**Result:** ✗ No effect. Crashes continued.

### Round 3a — conditional logger callback (scrapligo `v2.0.0-rc.18.1`)
Made the dispatcher registration and `opts.loggerCallback` conditional on `o.Logger != nil`
(`internal/options.go`, now factored into `Options.RegisterLogger`).
**Result:** ✗ No effect. Crashes continued.

### Round 3b — remove libscrapli's slice-header write (`ffi-driver.zig:767`)
Found and fixed a genuine, proven memory-corruption bug (section 5).
**Result:** ✗ Crashes continued — and per the tasking note, *new* panics began appearing.

### Round 4 — `SA_ONSTACK` on libscrapli's signal handlers
Decoding the recurring stack residue as a kernel signal frame (section 7) identified the real
writer. **Result:** fix implemented (section 9); pending production soak.

---

## 4. Definitively eliminated

These are closed. Do not re-litigate them without new evidence.

### 4.1 Foreign-thread purego callbacks — ELIMINATED
The crashing build contained **both** `CGO_ENABLED=1` and scrapligo `rc.18.1`. ksqueegee only
calls `options.WithLogger` when `d.log.GetLevel() <= zerolog.DebugLevel`
(`ksqueegee/pkg/scrape/driver/scrapli.go:96`); agents run at info, so `o.Logger == nil`. ksqueegee
registers no recorder callback and no NETCONF capabilities callback either.

⇒ **Zero purego callbacks were registered.** No foreign thread ever entered the Go runtime.

### 4.2 The fakecgo / fabricated-g0-stack-bounds theory — ELIMINATED
Follows from 4.1 (no callbacks ⇒ `needm` is never reached) *and* from `CGO_ENABLED=1` supplying
real `runtime/cgo` with accurate bounds via `pthread_getattr_np`.

### 4.3 Goroutine-stack relocation dangling Go pointers — ELIMINATED
`go build -gcflags='-m' ./cli/` shows every pointer handed across the boundary is
`moved to heap` — `cancel`, `operationID`, `inputs`, `results`, `errString`, `splits`, etc.
The Go heap is non-moving, so stack growth/shrink cannot invalidate them.

### 4.4 `operationID *uint32` retained past the call — ELIMINATED
`operation_id.* = _operation_id` is written **synchronously** before the FFI call returns
(e.g. `libscrapli/src/ffi-root-cli.zig:423`). Not retained.

### 4.5 `cancel *bool` as the *writer* — ELIMINATED
Zig only ever reads it: `cancellation.zig:3-8` does `@atomicLoad(bool, c, .monotonic)`. A grep of
the whole Zig tree finds no write through `cancel`. It is still an illegal retention (section 6.2),
but it cannot be the thing corrupting memory.

### 4.6 FFI result buffer overruns — LIKELY ELIMINATED
All pack/copy paths are bounds-checked and alias-tolerant via `bytes.ffiCopyAt`
(`cli-result.zig:363-418`), and `reconstructRawInto` (`bytes.zig:1838-1898`) validates every
journal entry against `out.len`. No unbounded write into a caller buffer was found on the FFI path.

---

## 5. Confirmed defect found and fixed (did NOT resolve the crashes)

**`libscrapli/src/ffi-driver.zig:767` — `operation_error.* = "";`**

This *assigned a whole Zig slice through the caller's pointer* instead of copying into the buffer
it points at. Go passes `&errString` — the address of a 24-byte `{data,len,cap}` header — where
Zig declares `*[]u8`, a 16-byte `{ptr,len}`. The assignment therefore overwrote Go's data pointer
with an address inside libscrapli's own `.rodata` and zeroed Go's length.

It was the **only** whole-slice assignment through a caller-owned pointer in the entire FFI
surface; every other path (including the CLI and NETCONF error paths) correctly uses
`copyForwards` *into* the buffer.

**Proof it was real** — running scrapligo's own suite with a temporary slice-header integrity
guard (since removed) against the *released* libscrapli:

```
libscrapli wrote over the go slice header for ffi buffer "errString"
  (data 0x102f76da0 -> 0x150ddeffe, len 0->0, cap 0->0)
```

Go's data pointer replaced by a libscrapli-region address, on **every single CLI operation**.
After the fix: zero violations.

**Secondary bug fixed by the same change:** zeroing Go's `len` meant `string(errString)` was always
`""`, so **every libscrapli error message was silently discarded** on the way out
(`cli/cli.go`, the `if errSize != 0` branch). This may have been masking useful diagnostics for the
entire investigation.

**Why it wasn't the crasher:** the write lands on a *live, in-scope* Go object during a
synchronous call. To corrupt an unrelated goroutine the target would have to be stale. Also the
observed write is `len 0→0`, whereas the dumps show `7`. Section 7 explains the `7`: it is
`uc_flags`, not a length.

Branch: `libscrapli` @ `fix/ffi-error-slice-header-write` (`16077dc`). Validated with the exact CI
toolchain (`zig 0.17.0-dev.2015+3fdcbc03d`): `zig build` clean, `zig build test` passes,
`zig fmt --check` clean. `zig build lint` fails identically on baseline (broken vendored `zlinter`
cache — unrelated).

---

## 6. Known-real problems still outstanding

### 6.1 Go/Zig slice header ABI mismatch (structural)
Go `[]byte` = 24 bytes `{data,len,cap}`; Zig `[]u8` = 16 bytes `{ptr,len}`. scrapligo passes
`&goSlice` everywhere libscrapli declares `*[]u8`. Reads line up by luck; **any** write through
that pointer plants a foreign pointer inside Go-managed memory. §5 was the only such write found;
any new FFI entry point that writes through a `*[]u8` must copy *into* the buffer, never assign
the slice.

### 6.2 `cancel *bool` retained past the call
`internal/wakeup.go` returns `ctx.Err()` immediately after setting `*cancel = true`, without
waiting for the Zig operation thread to observe it. The Zig thread keeps the Go pointer
(`cli-operation.zig` `SendInputOptions.cancel`) and keeps reading it. Read-only, so not the
crasher, but it violates the pointer-passing rules and should become a C-owned flag
(`ls_cancel_operation(driverPtr, operationID)`).

### 6.3 NETCONF capabilities callback returns a Go pointer to Zig
`internal/netconf.go` — `capabilities()` returns `*string`, a pointer to a Go-heap string header,
with no keepalive. Unambiguously illegal. Not implicated in these crashes (ksqueegee registers no
capabilities callback) but it is a live footgun for any NETCONF consumer.

### 6.4 `userData` dispatcher leak
`GetUserDataDispatcherr().Register()` is called per `NewCli`; `Deregister` only runs on `Open`
failure or `Close`. Monotonic counter + map entry leak. Minor; not a crash cause (the purego
callbacks are singletons, so the 2000-callback cap is not at risk).

### 6.5 `recover()` gives false confidence
ksqueegee's `EnsureLibscrapli` installs a `recover()`. None of these faults are recoverable. Keep
it for genuine init panics; do not treat it as a safety net here.

---

## 7. Root cause: SIGPIPE/SIGIO handlers without `SA_ONSTACK`

### 7.1 The writer
In the pinned Zig std (`0.17.0-dev.2015+3fdcbc03d`), `std.Io.Threaded.init`
(`lib/std/Io/Threaded.zig` ≈ lines 1650–1665) installs `doNothingSignalHandler` for `.IO` and
`.PIPE` with `.flags = 0`. `InitOptions` has no option to turn this off, and `deinit` restores
the previous handlers. libscrapli calls it from `initThreaded()` (`src/ffi-common.zig`) via
`initIo()` on the first `ls_cli_alloc` or `ls_netconf_alloc`. From then on, every thread in the
process, Go threads included, uses Zig's handler for these two signals.

Go installs its own handlers with `SA_ONSTACK` and gives every M an alternate signal stack
(`gsignal`, 32 KB), so signal frames never land on the small, movable goroutine stacks. The
`os/signal` package docs state the rule: non-Go code that installs signal handlers must use
`SA_ONSTACK`. Without that flag the kernel ignores the alternate stack and pushes the frame at
the current `rsp`, which is on the goroutine stack.

### 7.2 The trigger
Any Go `write` to a socket whose peer has gone away returns EPIPE and raises SIGPIPE on the
writing thread. gRPC's HTTP/2 transport does this routinely. In the dumps it is a 17-byte control
frame (PING/GOAWAY) written to a disconnected client. The x86-64 `rt_sigframe` plus its xsave area
is ~3.4 KB. When the goroutine has less headroom than that, the frame runs off the end of its
stack into adjacent memory, usually another goroutine's stack. In the dumps the overrun is 0x188
bytes. The handler does nothing, so the writing goroutine carries on normally. The damage is to
the neighbour and only shows up later.

### 7.3 Decoding the residue
The `{0x7f…df0, 0x7}` pattern and the words around it match the `rt_sigframe` layout exactly in
every independent dump (`panic1`/`panic2`, which are the same incident, and `panic3`):

| Field | Value in dumps | Meaning |
|---|---|---|
| `pretcode` (frame +0x00) | `0x7f…df0` | `sa_restorer` signal-return trampoline in a mapped shared library. Same symbol, so same page offset, every time |
| `uc_flags` (frame +0x08) | `0x7` | `UC_FP_XSTATE \| UC_SIGCONTEXT_SS \| UC_STRICT_RESTORE_SS`, which current x86-64 kernels always set |
| `uc_stack.ss_size` | `0x8000` | Go's 32 KB `gsignal` stack is registered but not used, because the handler lacks `SA_ONSTACK` |
| `mcontext.rip` | `syscall.Syscall6+0xe` | The instruction just after `syscall` |
| `mcontext.rax` | `0xffffffffffffffe0` | `-EPIPE` (-32) |
| `mcontext.r11`, `eflags` | `0x216` | `syscall` saves RFLAGS into r11 |
| `mcontext.rdi`, `rdx` | fd 7 or 8, `0x11` | A 17-byte write on a gRPC socket |
| `mcontext.csgsfs` | `0x002b000000000033` | User-mode CS `0x33` and SS `0x2b` |

This is a kernel signal frame, not a Zig data structure, so every "Zig wrote a slice into Go
memory" theory is ruled out for these crashes.

### 7.4 How it explains every symptom
- **`called from 0x7` / `unknown pc`:** `uc_flags` overwrote a saved return address.
- **`pc == addr == 0xc000…`:** a stack address from the frame overwrote a return address, so the
  CPU jumped into data.
- **`panic3` `selectgo(0x7f…df0, 0x7, …)`:** the frame landed on a parked goroutine's `select`
  arguments, and `sellock` then used `pretcode` as the `scases` pointer. Any parked `select`
  fits. The scrapligo cancel watchdog is not special.
- **GC `scanstack` and `growslice` faults:** runtime code walked the corrupted neighbour.
- **Steady state only, never at startup:** it needs a libscrapli alloc first and an EPIPE later.
- **CGO on/off, logger callbacks and the §5 fix changed nothing:** none of them touch signal
  handling.

### 7.5 Earlier theory (retracted)
Earlier versions of this document suggested that libscrapli was writing a 7-byte `.rodata` slice
(`@errorName`) through a stale Go pointer, and recommended auditing the timeout path, hardening
concurrent operations and running ASan. The decode above disproves this, so that line of work is
closed.

---

## 8. Tooling notes

The `SCRAPLIGO_FFI_GUARD` slice-header guard from an earlier round has been **removed**. It was
built for the retracted §7.5 theory. It did its job by proving the §5 defect, and no other writer
of that kind exists.

### Local build notes
- libscrapli requires **zig ≥ 0.17.0**; CI pins `0.17.0-dev.2015+3fdcbc03d` (`.github/vars.env`).
  Homebrew's 0.16 cannot build the project at all. Fetch the exact build from
  `https://ziglang.org/builds/zig-aarch64-macos-<version>.tar.xz`.
- `make test` in libscrapli runs `zig fmt ./`, which walks the vendored `zig-pkg/` cache and
  fails. Run `zig build test -Doptimize=Debug` directly.
- `zig build lint` fails on a broken vendored `zlinter` cache — pre-existing, reproduces on a
  clean tree.
- In scrapligo, point at a local library with
  `LIBSCRAPLI_PATH=/path/to/libscrapli.0.0.0.dylib`.
- scrapligo's `build/dummy_ssh_server` has its own `go.mod` requiring go 1.26.4. If your
  `go env GOTOOLCHAIN` is pinned below that, `TestConcurrency` fails with a bare `exit status 1`.
  Use `GOTOOLCHAIN=auto`.
- The scrapligo signal test (`internal/signals_linux_test.go`) runs only on Linux. From macOS use
  e.g. `docker run --rm -v $PWD:/src -v $(go env GOMODCACHE):/go/pkg/mod -w /src golang:1.25 go
  test ./internal/`.

---

## 9. The fix

Neither side swaps out Zig's handler. Both keep it and add `SA_ONSTACK`. Putting Go's SIGPIPE
handler back would be *worse*: a SIGPIPE on one of libscrapli's non-Go threads would reach Go's
`badsignal` path and kill the process.

### 9.1 libscrapli (root cause)
Branch `fix/ffi-error-slice-header-write`, on top of the §5 fix. `initThreaded()` in
`src/ffi-common.zig` calls `setSignalHandlerOnStack(.IO)` / `(.PIPE)` straight after
`std.Io.Threaded.init`. Each call reads the current action, ORs in `SA.ONSTACK` and writes it
back. It is skipped on targets without POSIX `sigaction`. The regression test in
`src/ffi-root.zig` (`"ffi: initIo leaves signal handlers on the alternate signal stack"`) failed
before the change and passes after it.

### 9.2 scrapligo (defence in depth)
`internal/signals_linux.go` exposes `EnsureSignalHandlersOnStack()`. For SIGPIPE and SIGIO it
reads the action with raw `rt_sigaction` and adds `SA_ONSTACK` if a real handler is installed
without it. It changes nothing else, and leaves `SIG_DFL` and `SIG_IGN` alone. `Cli.Open` and `Netconf.Open` call it right after `Alloc`, including when alloc fails, because the handlers
are installed during alloc either way. On other platforms `internal/signals_other.go` makes it a
no-op. This protects consumers that run any libscrapli release, including ones built before 9.1.

### 9.3 Remaining steps
1. Release libscrapli (§5 + §9.1) and scrapligo (§9.2), then bump both in ksqueegee.
2. **Soak:** the previous time between crashes was 20–120 minutes, so 24–48 hours without a
   crash on the affected agents is strong confirmation.
3. Optional: report the missing `SA_ONSTACK` in `std.Io.Threaded.init` upstream to Zig, or ask
   for an `InitOptions` flag to skip handler installation.
4. §6.2–§6.4 are real but unrelated. Fix them when convenient.

### Reliability impact
- **Before the fix:** any EPIPE on a Go socket could corrupt a random goroutine and kill the
  whole agent. Config snapshots sent in the minutes before a crash should be treated as suspect.
- **After the fix:** the only change is *where* the kernel puts the signal frame. Go's own
  handlers already rely on this mechanism. Signal handling behaviour is unchanged: SIGPIPE is
  still ignored and the write still returns EPIPE.
- **Small residual window:** the scrapligo repair runs just after the first `Alloc` returns, so a
  SIGPIPE in the microseconds between Zig installing the handler and the repair could still
  land on a goroutine stack. The libscrapli fix closes this window, because it sets the flag
  inside the same init call.
- `CGO_ENABLED=1` (round 2) has already been reverted in ksqueegee (`87d3d1e`).

---

## 10. Reference index

**scrapligo**
| Location | Relevance |
|---|---|
| `internal/signals_linux.go` | `EnsureSignalHandlersOnStack` (§9.2) |
| `internal/signals_linux_test.go` | Verifies it repairs a handler that is missing `SA_ONSTACK` |
| `cli/cli.go`, `netconf/netconf.go` (after `Alloc`) | Call sites for the repair |
| `internal/options.go` `RegisterLogger` | Round 3a fix; the callback is now conditional |
| `internal/wakeup.go` | Returns early on ctx cancel (§6.2) |
| `internal/netconf.go` | Returns `*string` to Zig (§6.3) |
| `internal/uniquiness.go` | `userData` leak (§6.4) |
| `constants/versions.go` | `LibScrapliVersion` |

**libscrapli**
| Location | Relevance |
|---|---|
| `src/ffi-common.zig` `initThreaded` / `setSignalHandlerOnStack` | Root-cause fix (§9.1) |
| `src/ffi-root.zig` (signal-stack test) | Regression test for §9.1 |
| `src/ffi-driver.zig:767` | The §5 defect, removed on `fix/ffi-error-slice-header-write` |
| `src/cancellation.zig:3-8` | Read-only access to `cancel` |
| Zig std `lib/std/Io/Threaded.zig` `init` | Where the handlers without `SA_ONSTACK` are installed |

**ksqueegee**
| Location | Relevance |
|---|---|
| `.goreleaser.yml` | `CGO_ENABLED=0` (round 2 reverted) |
| `pkg/scrape/driver/scrapli.go:96` | Logger is only wired at debug or lower |
| `cmd/ksqueegee-agent/run/run.go:35` | `errgroup.Wait`, goroutine 1 in every dump |
