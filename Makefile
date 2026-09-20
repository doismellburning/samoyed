DIST_DIR = dist
DEB_STAGING = .deb-staging
C_FILES = $(shell find * -name \*.c)
GO_FILES = $(shell find * -name \*.go)
SHELL_FILES = $(shell grep -rIl '^\#!.*sh' * .claude)
SRC_DIRS = ./cmd/... ./internal/... ./src/...
CMDS = $(notdir $(wildcard ./cmd/*))
COVERAGE_FILE = cover.out
GOLANGCI_LINT_VERSION = v2.13.2
GOVULNCHECK_VERSION = v1.8.0
GOTEST_FLAGS = # Anything extra you'd like to pass to `go test`, e.g. `-v`

.PHONY: all
all: $(CMDS) test

# Installs the system dependencies (hamlib, Dire Wolf, shellcheck, ...) needed
# by the targets below
.PHONY: setup
setup:
	./dev-setup.sh

.PHONY: cmds
cmds: $(CMDS)

# samoyed, ll2utm, etc. etc.
.PHONY: $(CMDS)
.SECONDEXPANSION:
$(CMDS): $(addprefix $(DIST_DIR)/,$$@)

$(DIST_DIR)/%: $(DIST_DIR) $(C_FILES) $(GO_FILES) ./cmd/%
	go build -o $(DIST_DIR)/ -ldflags "-X 'github.com/doismellburning/samoyed/src.SAMOYED_VERSION=$(SAMOYED_VERSION)'" ./cmd/$*/...

$(DIST_DIR):
	mkdir -p $(DIST_DIR)

.PHONY: deb
deb: cmds
	test -n "$(SAMOYED_VERSION)" || (echo "ERROR: SAMOYED_VERSION is not set" >&2; exit 1)
	rm -rf $(DEB_STAGING)
	mkdir -p $(DEB_STAGING)/DEBIAN $(DEB_STAGING)/usr/bin $(DEB_STAGING)/usr/share/man/man1
	strip $(DIST_DIR)/*
	cp $(DIST_DIR)/samoyed-* $(DEB_STAGING)/usr/bin/
	cp man/*.1 $(DEB_STAGING)/usr/share/man/man1/
	grep -v '^#' packaging/debian/control.in \
		| sed -e 's/@@VERSION@@/$(SAMOYED_VERSION)/g' \
		      -e "s/@@ARCH@@/$$(dpkg --print-architecture)/g" \
		> $(DEB_STAGING)/DEBIAN/control
	dpkg-deb --build --root-owner-group $(DEB_STAGING) samoyed-binary_$(SAMOYED_VERSION)_$$(dpkg --print-architecture).deb
	rm -rf $(DEB_STAGING)

.PHONY: test
test: gotest test-scripts

.PHONY: gotest
gotest:
	go test $(GOTEST_FLAGS) -cover -coverpkg=./cmd/...,./internal/...,./src/... -coverprofile $(COVERAGE_FILE) $(SRC_DIRS)  # TODO Construct coverpkg from $SRC_DIRS

.PHONY: race
race:
	go test $(GOTEST_FLAGS) -race $(SRC_DIRS)

# Go fuzzes one target at a time, so each gets its own invocation, and
# FUZZTIME is therefore per target rather than for the run as a whole.
# The seed corpus of every target runs under `make test` regardless.
FUZZTIME = 30s

.PHONY: fuzz
fuzz:
	@for pkg in $$(go list $(SRC_DIRS)); do \
		for target in $$(go test -list '^Fuzz' $$pkg | grep '^Fuzz'); do \
			echo "Fuzzing $$target in $$pkg for $(FUZZTIME)..."; \
			go test -run '^$$$$' -fuzz "^$$target\$$$$" -fuzztime $(FUZZTIME) $$pkg || exit 1; \
		done; \
	done

# TODO Better output name, non-PHONY target, docs, etc.
.PHONY: gotest-bin
gotest-bin:
	go test -c -gcflags "-N -l" ./src

.PHONY: test-scripts
test-scripts: $(CMDS)
	./test-scripts/runall

.PHONY: coveragereport
coveragereport:
	go tool cover -func=$(COVERAGE_FILE)

.PHONY: perfilecoverage
perfilecoverage:
	$(MAKE) coveragereport | awk -F'\t' '/^github/ { split($$1, a, ":"); file = a[1]; sub(".*samoyed/", "", file); pct = $$NF; sub(/%/, "", pct); totals[file] += pct; counts[file]++ } END { for (f in totals) { printf "%.1f%%\t%s\n", totals[f]/counts[f], f } }' | sort -k2


.PHONY: check
check: vet lint shellcheck reuse
	go mod tidy -diff

.PHONY: reuse
reuse:
	reuse lint

.PHONY: shellcheck
shellcheck:
	shellcheck --external-sources --exclude SC1091 $(SHELL_FILES)

.PHONY: vet
vet:
	go vet $(SRC_DIRS)

# Depending on the Makefile means a GOLANGCI_LINT_VERSION bump reinstalls the binary
# rather than leaving a stale one in place.
./bin/golangci-lint: Makefile
	# Clear out any previous binary first, so the `test -x` below is a real check of
	# whether the download delivered rather than something a stale binary can satisfy.
	rm -f $@
	# This is not pleasant but it's also the/a recommended way of installation and means that we're explicitly pinning version
	# https://golangci-lint.run/welcome/install/#binaries
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s $(GOLANGCI_LINT_VERSION) || true
	# ...but that's unreachable from some sandboxed containers, so fall back to building the same pinned version ourselves.
	# We check for the binary rather than the pipeline's exit status, which is `sh`'s and so is 0 even when curl fetched nothing.
	# golangci-lint refuses to lint a module targeting a newer Go than golangci-lint
	# itself was built with, and its own go.mod pins an older toolchain than ours, so
	# build it with the toolchain this module uses - as the official binaries are.
	test -x $@ || GOTOOLCHAIN=$$(go env GOVERSION)+auto GOBIN=$(CURDIR)/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: lint
lint: ./bin/golangci-lint
	./bin/golangci-lint run $(SRC_DIRS)

# Depending on the Makefile means a GOVULNCHECK_VERSION bump reinstalls the binary
# rather than leaving a stale one in place.
./bin/govulncheck: Makefile
	rm -f $@
	# govulncheck type-checks our packages with the go/types it was built against, so
	# one built with an older toolchain than ours reports "package requires newer Go
	# version" for every package and checks nothing. Build it with the toolchain this
	# module uses, as with golangci-lint above.
	GOTOOLCHAIN=$$(go env GOVERSION)+auto GOBIN=$(CURDIR)/bin go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

# Reports advisories from https://vuln.go.dev whose vulnerable symbols we actually
# call, so it stays quiet about vulnerabilities in code we never reach.
#
# Deliberately not part of `check`: its answer depends on the day's advisories and
# on vuln.go.dev being reachable, where everything else `check` runs gives the same
# answer for a given commit forever. It gets its own scheduled workflow instead.
.PHONY: vuln
vuln: ./bin/govulncheck
	./bin/govulncheck $(SRC_DIRS)

.PHONY: fix
fix: ./bin/golangci-lint
	./bin/golangci-lint run --fix $(SRC_DIRS) || true  # golangci-lint will still run other non-fix linters, and fail if it didn't fix everything - I just want best-effort
	go mod tidy


.PHONY: stats
stats:
	@echo "Code Stats"
	@echo "=========="
	@echo ""
	@echo -n "C (src):      "
	@find src -name \*.c -exec wc -l {} + | tail -n 1 | sed -e "s/^ *//"
	@echo -n "H (src):      "
	@find src -name \*.h -exec wc -l {} + | tail -n 1 | sed -e "s/^ *//"
	@echo -n "C (external): "
	@find external -name \*.c -exec wc -l {} + | tail -n 1 | sed -e "s/^ *//"
	@echo -n "H (external): "
	@find external -name \*.h -exec wc -l {} + | tail -n 1 | sed -e "s/^ *//"
	@echo -n "Go:           "
	@find * -name \*.go -exec wc -l {} + | tail -n 1 | sed -e "s/^ *//"
	@echo -n "CMake:        "
	@find * -name CMakeLists.txt -exec wc -l {} + | tail -n 1 | sed -e "s/^ *//"

tags: $(C_FILES) $(GO_FILES)
	ctags --recurse --languages=C,Go --c-kinds=+p --fields=+iaS --extras=+q cmd/ internal/ src/
