# Samoyed

Samoyed is a fully-featured software modem/TNC for packet radio.
It supports AX.25 v2.2, FX.25, IL2P, APRS, multi-speed modems (300-9600 bps), digipeating, IGates, and more.
Samoyed is a Go port of [Dire Wolf](https://github.com/wb2osz/direwolf).

> **Status:** Port functionally complete, but broad testing needed - consider this pre-release, and likely to contain bugs.
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

## Installation

Pre-built binaries are available on the [GitHub Releases page](https://github.com/doismellburning/samoyed/releases), including tarballs and `.deb` packages.

The `.deb` packages are recommended for Debian-based systems (Debian, Ubuntu, Raspberry Pi OS, etc.) and can be installed with:

```sh
sudo apt install ./samoyed-binary_*.deb
```

## Usage

The primary binary is `samoyed-direwolf`. Run it with `--help` for a full list of options:

```sh
samoyed-direwolf --help
```

For more detail on configuration and features, see the [Dire Wolf documentation](https://github.com/wb2osz/direwolf/tree/master/doc) — Samoyed aims for broad compatibility with Dire Wolf.

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

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## Platform Support

| Platform | Status |
|----------|--------|
| x86_64 Linux | Primary target |
| arm64 Linux | Supported |
| macOS | Supported |
| Windows | TBD (depends on demand / testability) |

## About Dire Wolf

Samoyed is based on [Dire Wolf](https://github.com/wb2osz/direwolf) by John Langner WB2OSZ —
a modern software replacement for hardware TNCs, with outstanding receive performance.
Dire Wolf decodes over 1000 error-free frames from the
[WA8LMF TNC Test CD](https://github.com/wb2osz/direwolf/tree/dev/doc/WA8LMF-TNC-Test-CD-Results.pdf),
outperforming all hardware TNCs and first-generation soundcard modems.

For Dire Wolf documentation, see the [Dire Wolf doc directory](https://github.com/wb2osz/direwolf/tree/master/doc).

## Further Reading

- [Samoyed - An Amateur Radio Software Modem](https://radio.doismellburning.co.uk/projects/samoyed-an-amateur-radio-software-modem/) — project background

## License

GPL-2.0-or-later. See [LICENSES/](./LICENSES/) and [REUSE.toml](./REUSE.toml) for full details.

Samoyed is a fork of Dire Wolf — see [AUTHORS.md](./AUTHORS.md) for authorship and attribution.
