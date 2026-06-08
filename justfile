default: build

build:
    go build -o crypto .

run: build
    ./crypto

install: build
    sudo install -m 755 crypto /usr/local/bin/crypto

lint:
    go vet ./...

test:
    go test ./...

tidy:
    go mod tidy

check: tidy lint test

release-dry:
    goreleaser release --snapshot --clean

release tag:
    git tag {{tag}}
    git push origin {{tag}}

clean:
    #!/usr/bin/env bash
    set -euo pipefail
    args=(-fdx)
    if [[ -f .cleanexclude ]]; then
        while IFS= read -r line || [[ -n "$line" ]]; do
            line="${line%%#*}"
            line="${line#"${line%%[![:space:]]*}"}"
            line="${line%"${line##*[![:space:]]}"}"
            [[ -z "$line" ]] && continue
            args+=(-e "$line")
        done < .cleanexclude
    fi
    git clean "${args[@]}"
