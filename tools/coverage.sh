#!/bin/sh -ue

VIEWER=${VIEWER:-xdg-open}

coverprofile="artifacts/coverage.out"

coverprofile_raw="${coverprofile}.raw"

report_text="artifacts/coverage_report_last.txt"

pkg="./pkg/..."

covermode=${COVER_MODE:-"set"}

report_html="artifacts/coverage.html"

mkdir -p artifacts

go test -timeout=10s -coverpkg="$pkg" -coverprofile="$coverprofile_raw" -covermode="$covermode" ./...
cat "$coverprofile_raw" \
    | grep -v ".pb." \
    | grep -v ".xo." \
    > $coverprofile

if [ "${1:-""}" = html ]; then
    go tool cover -html="$coverprofile" -o /dev/stdout |
        ./tools/gocov-patch-html-report.sh > "$report_html"
    exec $VIEWER "$report_html"
fi

go tool cover -func="$coverprofile" | tee "$report_text"
