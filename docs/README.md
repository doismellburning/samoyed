# samoyed/docs

Sphinx-based documentation site for [Samoyed](https://github.com/doismellburning/samoyed/).

Built from https://github.com/doismellburning/python-template/ so there may be some slightly vestigial infrastructure.

The build needs Go and Graphviz, as it generates a package dependency graph with `go list` - `../dev-setup.sh docs` installs Graphviz (and uv).

* `make dirhtml` to build
* `make autobuild` for local development
