# Samoyed

Samoyed is a software "soundcard" modem/TNC and APRS encoder/decoder,
ported to Go from [Dire Wolf](https://github.com/wb2osz/direwolf).

> **Status:** Active port in progress — pre-release, likely to contain bugs.
> Linux x86_64 is the primary supported platform during porting.
> Please [file issues](https://github.com/doismellburning/samoyed/issues) if you encounter problems.

## Why Samoyed?

Dire Wolf is a mature, capable piece of software written in C. Samoyed is (currently) a near-straight port to Go, motivated by:

- **Better tooling** — Go's testing, linting, and static analysis ecosystem, as well as the stdlib
- **Simpler builds** — no cmake, no preprocessor conditionals for test infrastructure
- **Reduced platform scope** — dropping old Windows / old CPU complexity makes the codebase easier to extend
- **Easier contribution** — idiomatic Go and improved test suites should make things more approachable for new contributors

Samoyed aims for broad-strokes compatibility with Dire Wolf to minimise switching costs.

## Features

Samoyed inherits Dire Wolf's feature set:

- **APRS** encoder/decoder — position, objects, messages, telemetry, weather, and more
- **Modems** — 300 bps AFSK (HF), 1200 bps AFSK (VHF/UHF), 2400 & 4800 bps PSK, 9600 bps GMSK/G3RUH, AIS, EAS SAME
- **FX.25** — Forward Error Correction fully compatible with existing AX.25 systems
- **IL2P** — Improved Layer 2 Protocol with lower overhead FEC
- **KISS interface** — TCP/IP, serial port, and Bluetooth
- **AGW network interface** — TCP/IP, compatible with many third-party applications
- **Digipeater** — APRS and traditional packet radio, with flexible cross-band and filtering options
- **Internet Gateway (IGate)** — bridging disjoint radio networks via the internet
- **GPS tracker beacons** — SmartBeaconing support
- **APRStt gateway** — DTMF tone sequences into the APRS network
- **AX.25 v2.2 connected mode** — automatic retransmission and in-order delivery
- **DTMF** decoding and encoding
- **Morse code** generator
- **DNS Service Discovery** — network KISS TNC auto-discovery (Linux)
- Concurrent operation with multiple soundcards and radio channels

## Building

### Prerequisites

See .github/workflows/build-and-test.yml for package dependencies

### Build

```sh
git clone https://github.com/doismellburning/samoyed
cd samoyed
make cmds        # build all binaries into dist/
make test        # run the full test suite
```

## Development

See [AGENTS.md](./AGENTS.md) for general development notes and style guidelines.

Key `make` targets:

| Target | Description |
|--------|-------------|
| `make all` | Build everything and run all tests — run before committing |
| `make cmds` | Build all binaries |
| `make test` | Run the full test suite |
| `make check` | Run linters (`vet`, `golangci-lint`, `shellcheck`, `reuse`) |
| `make fix` | Auto-fix linting issues where possible |
| `make coveragereport` | Show test coverage breakdown |
| `make stats` | Show lines-of-code breakdown (C vs Go) |

## Contributing

Contributions are welcome! While the Go port is ongoing, new features are generally discouraged
as they risk delaying the port's completion — but bug fixes, test improvements, and
help porting remaining C modules are all very useful.

See [CONTRIBUTING.md](./CONTRIBUTING.md) for details.

## Platform Support

| Platform | Status |
|----------|--------|
| x86_64 Linux | Primary target |
| arm64 Linux | Planned after initial port |
| Windows | TBD (depends on demand / testability) |
| macOS | TBD (depends on demand / testability) |

## About Dire Wolf

Samoyed is based on [Dire Wolf](https://github.com/wb2osz/direwolf) by John Langner WB2OSZ —
a modern software replacement for hardware TNCs, with outstanding receive performance.
Dire Wolf decodes over 1000 error-free frames from the
[WA8LMF TNC Test CD](https://github.com/wb2osz/direwolf/tree/dev/doc/WA8LMF-TNC-Test-CD-Results.pdf),
outperforming all hardware TNCs and first-generation soundcard modems.

For Dire Wolf documentation, see the [Dire Wolf doc directory](https://github.com/wb2osz/direwolf/tree/master/doc).

## License

GPL-2.0-or-later. See [LICENSES/](./LICENSES/) and [REUSE.toml](./REUSE.toml) for full details.

Samoyed is a fork of Dire Wolf — see [AUTHORS.md](./AUTHORS.md) for authorship and attribution.
