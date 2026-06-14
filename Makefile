BINARY := crypto

.PHONY: all build run install lint test tidy check release-dry release clean

all: build

build:
	go build -o $(BINARY) ./cmd/crypto-ticker

run: build
	./$(BINARY)

install: build
	sudo install -m 755 $(BINARY) /usr/local/bin/$(BINARY)

lint:
	go vet ./...

test:
	go test ./...

tidy:
	go mod tidy

check: tidy lint test

release-dry:
	goreleaser release --snapshot --clean

release:
ifndef TAG
	$(error TAG is required, e.g. make release TAG=v1.0.0)
endif
	git tag $(TAG)
	git push origin $(TAG)

clean:
	@set -euo pipefail; \
	args=(-fdx); \
	if [ -f .cleanexclude ]; then \
		while IFS= read -r line || [ -n "$$line" ]; do \
			line="$${line%%#*}"; \
			line="$${line#"$${line%%[![:space:]]*}"}"; \
			line="$${line%"$${line##*[![:space:]]}"}"; \
			[ -z "$$line" ] && continue; \
			args+=(-e "$$line"); \
		done < .cleanexclude; \
	fi; \
	git clean "$${args[@]}"
