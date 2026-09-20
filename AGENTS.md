# AGENTS.md

## Development

* `make setup` installs the system dependencies everything else needs (apt-based Linux and macOS/Homebrew) - it's what CI runs too, so a new build/test/lint dependency belongs in `dev-setup.sh` rather than in a workflow step
* `make all` builds and tests everything - a good general check
* `make test` runs the full test suite and should always pass
* `make check` runs assorted linters and should always pass
* `make fix` will attempt assorted auto-fixes and also do a partial lint run and is worth running after every change
* Claude Code on the web sessions run `.claude/hooks/session-start.sh`, which does `make setup` and a little container-specific fixing-up, so a fresh session can build, test and lint without further ceremony

## Documentation

* Update documentation in `docs/source` where appropriate

## Git and PRs

* A bug fix carries the regression test that reproduces the bug, in the same
  commit as the fix.
* Keep merge commits out of a PR: rebase onto `main` rather than merging `main`
  in.
* Keep fixup commits out of a PR: when a later commit corrects an earlier one,
  amend the earlier commit. This holds for review feedback too, so the PR reads
  as though the point had never been missed. A new commit is for a distinct
  logical change the review turned up — a second bug, a behaviour the fix newly
  exposes — not for correcting what an existing commit already does.
* Commit messages and PR titles are Conventional Commits — `<type>: <summary>`,
  using one of `build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`,
  `refactor`, `revert`, `style`, `test`; check `git log` on `main`. Capitalise
  the summary after the tag: `feat: Improve ...`.
  `./check-conventional-commits.sh` checks subjects against this, and CI runs it
  over every commit in a pull request and the pull request title.
* A commit message describes what its own diff does; check the two against each
  other once the commit exists.
* An agent's commits carry a `Co-Authored-By:` line for it, and never a
  `Claude-Session:` trailer.
* Never name a pull request in a commit message — describe what the other change
  did instead (`splitting makewholine took the worst of this out already`, not
  `#1939 did`). Issues are fine and are what `main` already carries: `Fixes
  #<issue>`, `Refs #<issue>`. Cross-reference PRs in the PR description, where
  the link is live.

## Style

* Prefer to declare variables as `var foo = bar` and not `foo := bar`, unless necessary e.g. with a `for` loop variable
* A value that might not be there is `maybe.Maybe[T]` (`internal/maybe`, modelled on Haskell's `Maybe`), not the `G_UNKNOWN` sentinel inherited from Dire Wolf, which is gone (see issue #619). Its zero value is Nothing, so a new struct needs no "clear everything to unknown" block, and `maybe.Fmap`/`LiftA2`/`Bind` keep the absence from leaking into arithmetic. A function that cannot represent an absence - the fixed-width conversions in `internal/latlong`, say - takes a plain value instead, and its caller unwraps and decides what an absent one means
* As this started as a port from C (Dire Wolf) there are a lot of things that aren't idiomatic Go yet - new things should be, but we don't need to change existing things if not necessary
* Prefer to use `new(Foo)` over `&Foo{}` - the latter makes the exhaustruct linter grumble
* Code paths that can run during config-file validation (before the rest of startup init has run, e.g. `pfilter_validate` from `handleFILTER`/`handleCFILTER`) must not assume other subsystems' global config pointers (e.g. `save_igate_config_p`) are non-nil - guard with a nil check and a safe default
* Fixed-size byte-array types that hold text (e.g. `Callsign [10]byte`) don't format as text with `%s` - they print as `%!s(main.Callsign=...)`. Give such types a `String()` method (e.g. via `direwolf.ByteArrayToString`, which trims trailing NULs) so `fmt` renders them correctly everywhere, rather than converting at each call site
* When an `int` from config or the CLI is stored into one of the fixed-width fields of a C-derived binary struct (e.g. the `int32`/`int16` fields of a `.WAV` or KISS/AX.25 header), validate that it fits *before* converting - an out-of-range value otherwise wraps silently and the struct ends up describing something nobody asked for. Remember to bound any derived fields too (e.g. a byte rate that is a sample rate times a frame size overflows sooner than the rate itself). `gosec`'s integer-overflow check does not catch these
* State shared between the main loop and a callback goroutine (e.g. anything started via `agwlib_init`, which dispatches `agw_cb_*` callbacks on a separate thread) needs explicit synchronization - plain maps/slices touched from both sides will race
* `dw_printf` (in `src/util.go`) is deprecated in favour of `logrus` - a `logrus` entry carries structured fields (`logrus.WithField("channel", channel).Info("Heard")`) instead of a line built up by successive `dw_printf` calls. Don't add new uses; converting the ones you find nearby while you're there is welcome. There are still ~1800 of them, so this will take a while, and staticcheck won't flag them for you - it says nothing about a deprecated identifier used inside its own package
* A converted entry's level follows how often it fires, not how interesting it is. `Debug` is for configuration and lifecycle - init, a port bound, a packet queued - and `Trace` for anything the signal drives: per audio sample, per bit, per byte, or several times per queue operation (the `try_decode` retry machinery, the `tq_*` lock tracing, the per-byte KISS/IGate/serial reads). Nothing enables `Trace` yet (see issue #645), so those entries are dormant until it does
* logrus builds an entry's fields before it consults the level, so `logrus.WithField(k, expensive()).Trace(...)` pays for `expensive()` even when nothing will print it. Where the fields cost an allocation - a `fmt.Sprintf`, a `string(...)` conversion - and the site is hot, guard the block with `logrus.IsLevelEnabled(logrus.TraceLevel)` first
* `dw_printf` is not recognised by `go vet`/golangci-lint as a printf-style function, so mismatched verbs (e.g. `%d` on an `error`) are not caught automatically - take extra care with format verbs when using it, especially for `error` values (use `%v`, not `%d`)
* When refactoring per-channel globals (arrays indexed by channel) into a per-channel struct accessed via a pointer array/map, guard lookups (e.g. `s_foo[channel]`) and any field/method access on the result against `nil` - a channel that was never attached, or whose connection has since dropped, can legitimately be `nil`
* When a per-channel/per-object struct has a field that's read, mutated, or closed from more than one goroutine (e.g. a listener thread and a send path both touching a connection handle), guard it with a `sync.Mutex` and route access through accessor methods rather than touching the field directly - see `PacketLogger.mu` in `src/log.go` for the existing pattern. When clearing/closing such a field after an error, compare against the value you read before acting on it, so you don't clobber a connection that another goroutine already replaced

### Test Callsigns

* Use `Q1TEST`, `Q2TEST`, etc. as synthetic callsigns in tests - the `Q` prefix is ITU-reserved for Q-codes and never assigned to amateur radio operators, so these can't be real callsigns
* Avoid `N0CALL` as a test callsign - Dire Wolf uses it as a sentinel value

### Test File Naming

* Per Go convention, generally tests for `foo.go` live in `foo_test.go` but we also do the following:
    * Tests ported from Dire Wolf are often in `foo_test_shim.go` because `go test` doesn't support cgo in _test.go files so that was a place to put the test code while the port was ongoing. Now we don't (directly) use cgo, this is obsolete, and they should live in `foo_direwolf_test.go` - kept separately to make it easier to track upstream
    * Tests where an LLM has just generated a test suite for complex functionality live in `foo_impl_test.go` - this signifies that they don't necessarily reflect intended/specified behaviour, they just test an implementation as-is, so failing tests may not necessarily signify a bug

## Concurrency

* For a goroutine that loops on `select { case <-stop: ...; case <-ticker.C: ... }` and reads/writes a resource (socket, buffer) also torn down elsewhere: don't rely on `stop` being closed to prevent the ticker branch from running one more time after teardown starts — `select` picks randomly among ready cases. Instead, have teardown nil the resource under the same mutex the goroutine locks before touching it, and re-check for nil inside that locked section before using it.
* If a mutex-guarded struct exposes one accessor per field, and a caller ever needs two of those fields together (e.g. a connection and its paired decoder/session state), add a combined accessor that locks once and returns both. Two separately-locked calls can straddle another goroutine's update, handing back a pair that never coexisted under the lock.
* A long-lived goroutine takes a `context.Context` as its first argument and returns when it is cancelled. `cmd/samoyed-direwolf` creates the context and `DirewolfMain` threads it through startup, so a new `go something(...)` there should be handed it too. In the goroutine: loop on `ctx.Err() == nil`, use `sleepCtx`/`sleepSecCtx` (`src/util.go`) in place of `SLEEP_MS`/`SLEEP_SEC`, and wrap a blocking `Accept`/`Read` with `closeOnDone`, which closes the socket on cancellation - nothing else reaches a goroutine sitting in a system call. Check `ctx.Err()` once the call returns, so our own close isn't reported as the far end failing
* Don't reach for the underlying file descriptor of a socket you expect to close from another goroutine: `net.TCPListener.File` (and `TCPConn.File`) puts the socket into blocking mode, which takes it out of the runtime's poller, and `Close` then no longer interrupts a pending `Accept` or `Read`. Set socket options through `net.ListenConfig.Control`/`net.Dialer.Control` instead - and note that Go already sets `SO_REUSEADDR` on a Unix TCP listener

## Licensing

* `make reuse` checks [REUSE](https://reuse.software/) compliance and must always pass
* New files should have copyright assigned to "The Samoyed Authors" and be GPL-2.0-or-later, as per REUSE.toml
* New individual files should declare this via SPDX headers where possible - if adding new entire directories, then adding an annotation path to REUSE.toml is acceptable
