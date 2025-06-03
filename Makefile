C_FILES = $(shell find * -name \*.c)
GO_FILES = $(shell find * -name \*.go)
SRC_DIRS = ./cmd/... ./src/...

.PHONY: all
all: samoyed test

samoyed: $(C_FILES) $(GO_FILES)
	go build ./cmd/samoyed

.PHONY: test
test:
	go test $(SRC_DIRS)

.PHONY: check
check: vet

.PHONY: vet
vet:
	go vet $(SRC_DIRS)

./bin/golangci-lint:
	# This is not pleasant but it's also the/a recommended way of installation and means that we're explicitly pinning version
	# https://golangci-lint.run/welcome/install/#binaries
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s v2.1.6

.PHONY: lint
lint: ./bin/golangci-lint
	./bin/golangci-lint run $(SRC_DIRS)

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

.PHONY: oldhelp
oldhelp:
	@echo "The build procedure has changed in version 1.6."
	@echo "In general, it now looks like this:"
	@echo " "
	@echo "Download the source code:"
	@echo " "
	@echo "	cd ~"
	@echo "	git clone https://www.github.com/wb2osz/direwolf"
	@echo "	cd direwolf"
	@echo " "
	@echo "Optional - Do this to get the latest development version"
	@echo "rather than the latest stable release."
	@echo " "
	@echo "	git checkout dev"
	@echo " "
	@echo "Build it.  There are two new steps not used for earlier releases."
	@echo " "
	@echo "	mkdir build && cd build"
	@echo "	cmake .."
	@echo "	make -j4"
	@echo " "
	@echo "Install:"
	@echo " "
	@echo "	sudo make install"
	@echo "	make install-conf"
	@echo " "
	@echo "You will probably need to install additional applications and"
	@echo "libraries depending on your operating system."
	@echo "More details are in the README.md file."
	@echo " "
	@echo "Questions?"
	@echo " "
	@echo " - Extensive documentation can be found in the 'doc' directory."
	@echo " - Join the discussion forum here:   https://groups.io/g/direwolf"
	@echo " "
