# AGENTS.md

## Licensing

* `make reuse` checks [REUSE](https://reuse.software/) compliance and must always pass
* New files should have copyright assigned to "The Samoyed Authors" and be GPL-2.0-or-later, as per REUSE.toml
* New individual files should declare this via SPDX headers where possible - if adding new entire directories, then adding an annotation path to REUSE.toml is acceptable

## Review notes

* cgo `C.CString` memory leaks are not a concern while porting - when the port is finished, all the cgo will be gone
