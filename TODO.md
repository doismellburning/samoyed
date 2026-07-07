# TODO

Prioritised task list derived from `CODE_QUALITY_REPORT.md` (analysis date
2026-07-06, commit `cb3923e`). Ordered roughly by value/effort per the
report's §5 sequencing, with quick wins pulled to the top.

## 0. Quick wins (do first, low effort/risk)

- [x] Fix `strconv.ParseFloat`/`Atoi` error swallowing in
      `cmd/samoyed-ll2utm/main.go` and `cmd/samoyed-utm2ll/main.go` — print
      parse errors + usage instead of silently continuing with zero values.
- [ ] Remove `//nolint:unused` dead stats counters in `igate.go`, or wire
      them up if they should be live.
- [ ] Delete `IfThenElse` from `util.go` (evaluates both arms — footgun for
      side-effecting/nil-deref arms; hides control flow). Update call sites.
- [ ] Delete `exit()` wrapper from `util.go` so remaining `os.Exit` sites are
      grep-able; migrate call sites case-by-case as part of §2.3 below.

## 1. Move standalone tool implementations out of `src/` (§2.6)

Mechanical, big reduction in library surface, unblocks lint ratcheting.
Move each `*Main()` body (and its globals) into its `cmd/samoyed-*/`
directory, or a shared `internal/tools/…` package where logic is genuinely
shared.

- [ ] `atest.go` → `cmd/samoyed-atest/` (17 globals, 27 exit sites; also
      replace `unsafe.Sizeof` WAV header I/O with `binary.Read` sizes)
- [ ] `gen_packets.go` → `cmd/samoyed-gen-packets/` (11 globals, 16 exits)
- [ ] `kissutil.go` → `cmd/samoyed-kissutil/` (10 globals)
- [ ] `aclients.go` → `cmd/samoyed-aclients/` (hand-rolled AGW parsing —
      share with `agwpe.go`, see §5 below)
- [ ] `tnctest.go` → `cmd/samoyed-tnctest/` (11 globals incl. `[MAX_TNC]`
      state arrays)
- [ ] `walk96.go` → its `cmd/` dir
- [ ] `fxrec.go`, `fxsend.go` → their `cmd/` dirs
- [ ] `cm108_main_linux.go` → its `cmd/` dir
- [ ] `decode_aprs_main.go` → its `cmd/` dir
- [ ] `dwgpsnmea_main.go` → its `cmd/` dir
- [ ] `ttcalc_main.go`, `text2tt.go` (Main half), `tt2text.go` (Main half)
      → their `cmd/` dirs

## 2. Output abstraction (§2.5)

- [ ] Introduce a `Console` interface (`Printf(color, format, ...)`) owned
      by the top-level app and threaded through service structs, replacing
      `text_color_set()` + `dw_printf()` globals in `textcolor.go`/`util.go`.
- [ ] Delete the stdout-pipe hijack in `testutils.go` once output is
      injectable; move `testutils.go` itself into a `testsupport` package
      or `_test.go` file (it's currently a non-test file exporting test
      helpers into the production package).
- [ ] Route `-q`/`q_h_opt`/`q_d_opt` quiet-flag handling through the new
      abstraction instead of globals in `direwolf.go`.

## 3. Channel-ise `dlq` and `tq` (§2.4)

Biggest correctness payoff; existing ported test suites protect behaviour.

- [ ] Replace `dlq.go`'s intrusive linked list, `dlq_mutex`,
      `dlq_wake_up_chan` condition-variable pattern, `*_thread_is_waiting`
      flags, and manual leak counters with a single buffered
      `chan *dlq_item_t`.
- [ ] Replace `tq.go`'s per-channel priority linked lists +
      `sync.Cond`/mutex/`xmit_thread_is_waiting` with two buffered channels
      per radio channel (hi/lo priority) and a `select` in the xmit loop;
      replace `tq_count_bytes`'s locked list-walk with atomic counters.
- [ ] In `ax25_pad.go`: drop `magic1`/`magic2` canaries, drop intrusive
      `nextp` once queues are channels, consider `frame_data []byte`
      instead of the fixed max-size array, and convert the 59 accessor
      functions to methods on `*packet_t`.

## 4. Error handling: return errors, don't print-and-exit (§2.3)

- [ ] Library functions return `error` (wrap with `%w` once
      `wrapcheck`/`err113` are re-enabled); only `main`/`*Main()` call
      `os.Exit`.
- [ ] Fix `NewWaypointSender` in `waypoint.go` (prints and returns a sender
      with a nil socket on failure — silent no-op sender).
- [ ] Fix `log_write` in `log.go` (silently drops the log on open failure
      after one message).
- [ ] `config.go`: collect `[]error` across the 84 `handle*` functions
      instead of printing mid-parse; report once. Biggest win for testable
      config validation.
- [ ] Audit remaining `os.Exit`/`exit(1)` sites in library code (21
      non-test files per report), including `ptt.go` (8 sites),
      `demod_psk.go` (3 sites), `dtmf.go` (2 sites), and others — convert to
      error returns.
- [ ] `pfilter.go`: return errors from the filter expression evaluator
      instead of printing to stdout, so bad filter configs fail loudly at
      config time.
- [ ] Review `Assert()` call sites (panic-based, fine during the port) —
      convert to `error` or add a comment explaining the invariant, one at
      a time as files are touched.

## 5. Continue globals → service-struct migrations (§2.2)

Roughly in order of value; each is independently mergeable (see
`samoyed-obj` skill / `waypoint.go` template).

- [ ] `igate.go` (24 globals) → `IGateClient` struct (socket, stats, dedupe
      history, delayed-packet queue); unify the two duplicate hand-rolled
      dedupe history rings (`rx2ig_*`, `ig2tx_*`) into one generic ring
      type; add a small connection state machine for reconnect/`ok_to_send`.
- [ ] `ax25_link.go` (14 globals) → `DataLinkService` owning the DLSM list
      + timers; split into `ax25_link_dl.go` / `ax25_link_lm.go` /
      `ax25_link_timers.go` by the spec's own layering (2192-line ported
      test file provides cover).
- [ ] `gen_tone.go` (14 globals) → `ToneGenerator` per channel.
- [ ] `hdlc_rec.go` (9 globals) → per-channel/subchannel receiver state
      struct per slicer (mostly already in `demod_state.go`).
- [ ] `dlq.go` (9 globals) — resolved by the channel rewrite in §3.
- [ ] `tq.go` / `xmit.go` (5 globals) → fold `tq` state into the existing
      `XmitService`; stop `xmit.go` reaching into package-level `tq_*`
      functions.
- [ ] `kiss.go`, `kissserial.go`, `nettnc.go` (4 globals each) → per-port
      structs.
- [ ] `log.go` (4 `g_*` globals) → `PacketLogger` struct; also de-duplicate
      the "who did we hear" logic shared with `direwolf.go` (its own
      comment flags this).
- [ ] `textcolor.go` (1 global) — resolved by §2 Console abstraction.
- [ ] `dwgps.go`/`dwgpsnmea.go` (3+2 globals) → `GPSSource` struct; replace
      the `dwgpsnmea_get_fd` port-sharing back-door used by `waypoint.go`
      with an explicit shared-port type.
- [ ] `audio.go` (4 globals) → finish struct conversion so multiple audio
      devices are instances, not array slots; error returns instead of
      print-and-continue on device failures.
- [ ] `audio_stats.go` (4 globals) → small ticker struct.
- [ ] `multi_modem.go` (3 globals, candidate matrix) → struct per channel.
- [ ] `dtmf.go` (4 globals) → struct (plus exit-site fixes from §4).
- [ ] `tt_user.go` (5 globals) → struct with mutex it already needs.
- [ ] `digipeater.go`/`cdigipeater.go` → light struct conversion for config
      pointers/regexes when touched.
- [ ] `server.go` (AGW server, 4 globals) → `AGWServer` struct; stop
      per-command error printing to stdout (protocol errors invisible to
      clients).
- [ ] `nettnc.go` (4 globals) → per-channel struct.
- [ ] `cmd/samoyed-appserver/main.go` — convert `session []session_s`
      "clear callsign means unused" slots to `map[sessionKey]*session`
      behind a small server struct; replace the receive-loop if/else chain
      with a table of handlers.
- [ ] `cmd/samoyed-log2gpx/main.go` — replace package-level
      `var things []thing_t` accumulator with pass/return slice; switch GPX
      generation from `fmt.Printf` string building to `encoding/xml` with
      structs (currently a real bug: unescaped `<`/`&` in APRS comments
      produces invalid GPX — add a regression test case for this).

## 6. Split `config.go` (6375 lines)

- [ ] Split into `config_audio.go`, `config_channel.go`, `config_aprs.go`,
      etc.
- [ ] Replace the `handleX(ps *parseState) bool` dispatch with a registry
      `map[string]func(*parseState) error` so adding a keyword is data, not
      a switch arm.
- [ ] Make `parseState` own tokenization instead of the `split(str,
      rest_of_line)` hidden-state tokenizer.
- [ ] Return errors (depends on §4).

## 7. Package split (§2.1) — do last

Do once globals are gone; moves become mostly `git mv` + import fixes.

- [ ] `internal/ax25/` — `ax25_pad*.go`, `ax25_link.go`, `xid.go`,
      `fcs_calc.go`
- [ ] `internal/aprs/` — `decode_aprs.go`, `encode_aprs.go`, `telemetry.go`,
      `symbols.go`, `latlong.go`, `base91.go`, `deviceid.go`, `aprs.go`
- [ ] `internal/dsp/` — `demod*.go`, `dsp.go`, `hdlc_rec*.go`,
      `hdlc_send.go`, `gen_tone.go`, `pll_dcd.go`, `rrbb.go`,
      `multi_modem.go`, `dtmf.go`, `morse.go`
- [ ] `internal/fec/fx25/` — `fx25*.go`
- [ ] `internal/fec/il2p/` — `il2p*.go` (best-factored subsystem already —
      use as the template)
- [ ] `internal/kiss/` — `kiss*.go`, `kissnet.go`, `kissserial.go`
- [ ] `internal/agw/` — `server.go`, `agwpe.go` + a shared AGW client
      package (promote one real client used by `appserver`, `aclients`,
      and `tnctest`, replacing their three divergent AGW implementations)
- [ ] `internal/audio/` — `audio.go`, `audio_stats.go`
- [ ] `internal/ptt/` — `ptt.go`, `cm108*.go`, `gpiod*.go`,
      `serial_port.go` (also consider replacing archived
      `github.com/pkg/term` with `go.bug.st/serial`)
- [ ] `internal/gps/` — `dwgps.go`, `dwgpsnmea.go`, `coordconv.go`
- [ ] `internal/tt/` — `aprs_tt.go`, `tt_text.go`, `tt_user.go`,
      `text2tt.go`, `tt2text.go`
- [ ] `internal/igate/` — `igate.go`
- [ ] `internal/config/` — `config.go`
- [ ] `internal/tui/` — `textcolor.go` + the output abstraction from §2
- [ ] `cmd/<tool>/` — each remaining tool implementation moves next to its
      `main` (should already be mostly done via §1)
- [ ] `internal/version/` — single ldflags target for `SAMOYED_VERSION`
      once the split is underway

## 8. Lint debt ratchet (§2.8)

Cheapest first; enable per-file as each file is converted rather than
flipping a linter globally.

- [ ] `whitespace`, `godot` (mechanical)
- [ ] `intrange`, `dupword`, `perfsprint`, `prealloc`
- [ ] `err113`, `wrapcheck` (alongside §4 error-handling work)
- [ ] `paralleltest`, `testpackage` (after §5 globals work — needed for
      test parallelism)
- [ ] `funlen`, `gocyclo`, `maintidx` (cyclop max-complexity 150!) — last,
      driven by the `config.go` (§6) and `ax25_link.go` (§5) splits

## 9. Naming/style cleanups (opportunistic — do when touching a file, don't churn otherwise)

- [ ] Rename `snake_case` functions/types (`ax25_get_addr_with_ssid`,
      `dlq_item_t`, `session_s`) as files get struct treatment.
- [ ] Rename reserved-word workarounds `_type`, `_chan` in `dlq_item_t` to
      `kind`, `channel`.
- [ ] Replace `G_UNKNOWN` sentinel floats in `util.go` (`DW_X_TO_Y`
      conversions) with a `type Knots float64`-style unit layer or
      `math.NaN()` + `IsUnknown()` helper.
- [ ] Convert function-header comment blocks (`Purpose:/Inputs:/Outputs:`)
      to godoc when a file is refactored; drop stale parts (e.g. "Global
      Out:" lists once globals are gone) and `/* end foo */` trailers.
- [ ] Rename `decode_aprs_t` result struct's `g_*`-prefixed fields (they're
      struct fields, not globals) when `decode_aprs.go` is split by
      data-type family (position/object/telemetry/mic-e/weather).
- [ ] Rename `aprs.go` or merge into `encode_aprs.go` (name oversells a
      25-line file of two wire-format structs).
- [ ] Make `fx25_init.go`'s global RS tables explicit as immutable
      (`sync.Once` or package `init`).
- [ ] Document `tt_text.go`'s 4 const-like table globals as immutable
      (`var … = [...]`) or generate them.

## 10. Repo hygiene

- [ ] Makefile TODOs: `coverpkg` duplication, fuzz target naming.
- [ ] One-shot `reuse annotate` pass to raise per-file SPDX header coverage
      (currently 8 of 150 `src/` files; REUSE.toml covers the rest so this
      is cosmetic/provenance, not a compliance gap).
- [ ] `version.go`: resolve the `MAJOR_VERSION`/`MINOR_VERSION` CalVer
      mismatch TODO by deriving the protocol version constants separately
      from the display version.
</content>
</invoke>
