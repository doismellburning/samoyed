# AGENTS.md

## Development

* `make all` builds and tests everything - a good general check
* `make test` runs the full test suite and should always pass
* `make check` runs assorted linters and should always pass
* `make fix` will attempt assorted auto-fixes and also do a partial lint run and is worth running after every change

## Style

* Prefer to declare variables as `var foo = bar` and not `foo := bar`, unless necessary e.g. with a `for` loop variable
* As this started as a port from C (Dire Wolf) there are a lot of things that aren't idiomatic Go yet - new things should be, but we don't need to change existing things if not necessary
* Prefer to use `new(Foo)` over `&Foo{}` - the latter makes the exhaustruct linter grumble

## Licensing

* `make reuse` checks [REUSE](https://reuse.software/) compliance and must always pass
* New files should have copyright assigned to "The Samoyed Authors" and be GPL-2.0-or-later, as per REUSE.toml
* New individual files should declare this via SPDX headers where possible - if adding new entire directories, then adding an annotation path to REUSE.toml is acceptable
