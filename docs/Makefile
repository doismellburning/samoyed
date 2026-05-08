SRCDIR=source
TESTDIR=test
CODEDIRS=$(SRCDIR) $(TESTDIR)

.DEFAULT_GOAL := check

.PHONY: bootstrap
bootstrap:
	uv sync --dev

.PHONY: check
check: format lint ty

.PHONY: format
format:
	uv run ruff format --check $(CODEDIRS)

.PHONY: lint
lint:
	uv run ruff check $(CODEDIRS)

.PHONY: ty
ty:
	uv run ty check

.PHONY: fix
fix:
	uv run ruff format $(CODEDIRS)
	uv run ruff check --fix $(CODEDIRS)

.PHONY: clean
clean:
	git clean -X --force

.PHONY: test
test:
	uv run pytest test

# Sphinx

# You can set these variables from the command line, and also
# from the environment for the first two.
SPHINXOPTS    ?= --fail-on-warning --keep-going --fresh-env --verbose
SPHINXBUILD   ?= sphinx-build
SOURCEDIR     = source
BUILDDIR      = build

# Put it first so that "make" without argument is like "make help".
help:
	uv run $(SPHINXBUILD) -M help "$(SOURCEDIR)" "$(BUILDDIR)" $(SPHINXOPTS) $(O)

.PHONY: help Makefile

# Catch-all target: route all unknown targets to Sphinx using the new
# "make mode" option.  $(O) is meant as a shortcut for $(SPHINXOPTS).
%: Makefile
	uv run $(SPHINXBUILD) -M $@ "$(SOURCEDIR)" "$(BUILDDIR)" $(SPHINXOPTS) $(O)

PORT ?= 8000
HOST ?= localhost
autobuild:
	uv run sphinx-autobuild --builder dirhtml "$(SOURCEDIR)" "$(BUILDDIR)" --host $(HOST) --port $(PORT) $(SPHINXOPTS) $(O)
