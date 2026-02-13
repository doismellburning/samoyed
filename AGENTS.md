# AGENTS.md

## Development

* `make all` builds and tests everything - a good general check
* `make test` runs the full test suite and should always pass
* `make check` runs assorted linters and should always pass
* `make fix` will attempt assorted auto-fixes and also do a partial lint run and is worth running after every change

## Style

* Prefer to declare variables as `var foo = bar` and not `foo := bar`, unless necessary e.g. with a `for` loop variable
* As this is still an incremental port from C (Dire Wolf) there are a lot of things that aren't idiomatic Go yet, and that's often somewhat intentional

## Licensing

* `make reuse` checks [REUSE](https://reuse.software/) compliance and must always pass
* New files should have copyright assigned to "The Samoyed Authors" and be GPL-2.0-or-later, as per REUSE.toml
* New individual files should declare this via SPDX headers where possible - if adding new entire directories, then adding an annotation path to REUSE.toml is acceptable

## Review notes

* Memory leaks via cgo conversion functions (`C.CString`, `C.CBytes`, etc.) are not a concern while porting - when the port is finished, all the cgo will be gone
