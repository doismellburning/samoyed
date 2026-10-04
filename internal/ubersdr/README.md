<!--
SPDX-FileCopyrightText: The Samoyed Authors
SPDX-License-Identifier: AGPL-3.0-or-later
-->

# UberSDR audio input

[UberSDR](https://ubersdr.org/) ([source](https://github.com/madpsy/ka9q_ubersdr)) is a web SDR,
built on [ka9q-radio](https://github.com/ka9q/ka9q-radio),
that serves many listeners from one receiver over HTTP and WebSockets.
This package lets Samoyed take its received audio straight from an UberSDR instance,
as an audio device named like
`ubersdr:https://sdr.example.org/?frequency=10147600&mode=usb`
(see "Receive from an UberSDR instance" in `docs/source/how-to-guides.rst`).

## How it works

1. `POST /connection` registers a random session ID, which the server must have seen before it will accept a WebSocket for it.
2. The `/ws` WebSocket is opened with the frequency, mode, `format=pcm-zstd` and `version=4`.
3. The server streams binary messages in its "PCM v4" format.
   Each message has a change-tracked header carrying the sample rate, channel count and sample count,
   followed by a body coded with a backward-adaptive predictor and Rice coding.
   Text messages are JSON (`error`, `status`, `pong`), and a `{"type":"ping"}` goes the other way every 30 s.

The predictor is stateful and is never told its coefficients:
both ends derive them from the samples decoded so far.
A decoder that differs from the server's in any arithmetic detail therefore produces plausible noise rather than an error,
and one that skips a packet desynchronises for the rest of the connection.
For that reason Samoyed uses UberSDR's own decoder, as described below, rather than a reimplementation.

## Layout

| Path | What | Whose | Licence |
|---|---|---|---|
| `source.go`, `client.go` | Parses device names; registers, connects, reconnects with backoff, and hands decoded audio to a sink | Samoyed | AGPL-3.0-or-later |
| `ubersdrtest/` | A fake UberSDR server for tests, here and in `internal/direwolf` | Samoyed | AGPL-3.0-or-later |
| `pcmv4/pcm_*.go` | The PCM v4 decoder: header, predictive codec, packet assembly | **UberSDR, copied** | GPL-3.0-only |
| `pcmv4/pcmv4_test.go`, `pcmv4/roundtrip_test.go`, `pcmv4/testdata/` | UberSDR's conformance tests and fixtures for that decoder | **UberSDR, copied** | GPL-3.0-only |
| `internal/v4enc/` | UberSDR's server-side encoder, used only by tests | **UberSDR, copied** | GPL-3.0-only |
| `pcmv4/doc.go`, `pcmv4/fuzz_test.go` | Package documentation, and a fuzz target for the decoder | Samoyed | AGPL-3.0-or-later |

`internal/direwolf/audio_ubersdr.go` connects this package to the audio device code.

## What is copied, and from where

The copied files come from `clients/hpsdr-go/internal/pcmv4/` in [madpsy/ka9q_ubersdr](https://github.com/madpsy/ka9q_ubersdr)
at commit [`c13b7e70f3800dd9d4d83104e76eaef98e6015b9`](https://github.com/madpsy/ka9q_ubersdr/tree/c13b7e70f3800dd9d4d83104e76eaef98e6015b9/clients/hpsdr-go/internal/pcmv4).
That directory is the standalone, receive-only decoder UberSDR's HPSDR bridge uses.
Its `v4enc/` subdirectory is, in turn, the server's own encoder copied into the client for its tests.

| Upstream | Here |
|---|---|
| `pcm_predictive.go`, `pcm_v4_header.go`, `pcm_v4_stream.go` | `pcmv4/` |
| `pcmv4_test.go`, `roundtrip_test.go`, `testdata/*.bin` | `pcmv4/`, `pcmv4/testdata/` |
| `v4enc/encoder.go`, `v4enc/header.go`, `v4enc/predictive.go` | `internal/v4enc/` |

The copies are verbatim, apart from two changes:

- **SPDX headers.** Each Go file gains `SPDX-FileCopyrightText: The UberSDR Authors` and `SPDX-License-Identifier: GPL-3.0-only`,
  and `REUSE.toml` annotates the fixtures in the same way.
- **One import path.** `roundtrip_test.go` imports the encoder from its new location.
  The encoder moved from `pcmv4/v4enc` to `internal/v4enc` so that `ubersdrtest` can use it too.

The copied files are kept as they were upstream, so that an upstream fix can be diffed straight in.
For the same reason they are excluded from golangci-lint in `.golangci.yml`.
`pcmv4/fuzz_test.go`, which is Samoyed's own, covers the decoder instead,
as it is fed by whatever server a configuration names.

## Licensing

- **UberSDR's code** is licensed under the GNU GPL version 3, according to the `LICENSE.TXT` at the root of its repository.
  The repository doesn't say "or any later version", so the copied files are marked `GPL-3.0-only`,
  and the licence text is in `LICENSES/GPL-3.0-only.txt`.
  The copyright belongs to UberSDR's authors.
- **Samoyed's own files** here are `AGPL-3.0-or-later`, like the rest of Samoyed's own work.
- **Combining them is permitted.**
  Section 13 of the GPLv3 allows GPLv3 code to be combined with AGPLv3 code.
  This is the same provision that lets Dire Wolf's `GPL-2.0-or-later` code be part of Samoyed.
  The GPLv3 files keep their own licence,
  and the program as a whole is distributed on AGPL terms.
- The only dependency the package adds is [`github.com/coder/websocket`](https://github.com/coder/websocket), which is ISC-licensed.

## Updating from upstream

To carry an upstream change across:

1. Diff the files in the table above against a newer upstream commit.
   Ignore the leading SPDX lines and the one import path.
2. Copy over what changed, and update the commit hash in this file and in `pcmv4/doc.go`.
3. Run `go test ./internal/ubersdr/...`.
   The conformance tests check the decoded samples against SHA-256 hashes,
   so a change that alters decoding fails them loudly.
   If upstream has re-recorded its fixtures, copy the new `testdata/` and hashes too.
