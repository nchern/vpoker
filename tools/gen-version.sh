#!/bin/sh
set -ue

VERSION_FILE="$1"

COMMIT_HASH=$(git rev-parse --short HEAD)

generate() {
    cat <<EOF
    package version

    func init() { value = "$COMMIT_HASH" }
EOF

}

generate | gofmt > "$VERSION_FILE"
