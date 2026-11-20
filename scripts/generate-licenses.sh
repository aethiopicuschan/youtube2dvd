#!/usr/bin/env bash

# Generates internal/licenses/generated/licenses.json, the third-party license
# manifest embedded in the app, and collects the license files for
# redistribution under dist/.
#
#   scripts/generate-licenses.sh           regenerate the manifest
#   scripts/generate-licenses.sh --check   fail if the manifest is out of date
#
# Requires go-licenses and jq.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

CHECK_ONLY=false
case "${1:-}" in
    "") ;;
    --check) CHECK_ONLY=true ;;
    *)
        echo "usage: $0 [--check]" >&2
        exit 2
        ;;
esac

APP_PACKAGE="."
APP_MODULE="$(go list -m)"
GENERATED_DIR="internal/licenses/generated"
MANIFEST="$GENERATED_DIR/licenses.json"
REDISTRIBUTION_DIR="dist/THIRD_PARTY_LICENSES"

# The dependencies differ between platforms, so the manifest covers every
# release target (see .github/workflows/release.yaml) rather than the machine
# the script runs on. This also makes the result the same on every machine.
TARGETS=(
    darwin/amd64 darwin/arm64
    linux/amd64 linux/arm64
    windows/amd64 windows/arm64
)

for tool in go-licenses jq; do
    if ! command -v "$tool" > /dev/null; then
        echo "$tool is required to generate the license manifest." >&2
        exit 1
    fi
done

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

echo "Generating third-party licenses..."

# Generate license metadata and full license texts for each target.
for target in "${TARGETS[@]}"; do
    report="$WORK_DIR/report-${target%/*}-${target#*/}.json"
    if ! GOOS="${target%/*}" GOARCH="${target#*/}" CGO_ENABLED=0 \
        go-licenses report "$APP_PACKAGE" \
        --ignore="$APP_MODULE" \
        --template=scripts/licenses.json.tmpl \
        > "$report" 2> "$WORK_DIR/go-licenses.log"; then
        cat "$WORK_DIR/go-licenses.log" >&2
        echo "go-licenses failed for $target." >&2
        exit 1
    fi
done

# video2dvd embeds the Shinonome bitmap font for Japanese DVD menu text. It is
# not a Go module, so go-licenses cannot see it; take the notice shipped with
# the library.
VIDEO2DVD_DIR="$(go list -m -f '{{.Dir}}' github.com/aethiopicuschan/video2dvd)"
SHINONOME_NOTICE="$VIDEO2DVD_DIR/third_party_licenses/shinonome-LICENSE.txt"

# Merge the targets, dropping the entries they share.
jq --slurp --rawfile shinonome "$SHINONOME_NOTICE" '{
        packages: (
            [.[].packages[]] | unique | sort_by(.name)
            + [{
                name: "Shinonome font family (embedded in github.com/aethiopicuschan/video2dvd)",
                version: "0.9.11",
                license: "Public Domain",
                license_url: "http://openlab.ring.gr.jp/efont/shinonome/",
                text: $shinonome
            }]
        )
    }' "$WORK_DIR"/report-*.json \
    > "$WORK_DIR/licenses.json"

go run ./cmd/validate-licenses "$WORK_DIR/licenses.json"

if "$CHECK_ONLY"; then
    # Compare the contents, not the bytes: jq versions format JSON differently.
    if [[ ! -f "$MANIFEST" ]] ||
        ! diff <(jq --sort-keys . "$MANIFEST") <(jq --sort-keys . "$WORK_DIR/licenses.json") > "$WORK_DIR/manifest.diff"; then
        echo "$MANIFEST is out of date:" >&2
        jq --raw-output '.packages[] | "\(.name) \(.version) \(.license)"' "$MANIFEST" > "$WORK_DIR/committed.txt" 2> /dev/null || true
        jq --raw-output '.packages[] | "\(.name) \(.version) \(.license)"' "$WORK_DIR/licenses.json" > "$WORK_DIR/generated.txt"
        diff "$WORK_DIR/committed.txt" "$WORK_DIR/generated.txt" >&2 ||
            true
        echo "Run scripts/generate-licenses.sh and commit the result." >&2
        exit 1
    fi
    echo "Third-party licenses are up to date."
    exit 0
fi

mkdir -p "$GENERATED_DIR"
cp "$WORK_DIR/licenses.json" "$MANIFEST"

# Collect redistribution artifacts.
rm -rf "$REDISTRIBUTION_DIR"

go-licenses save "$APP_PACKAGE" \
    --ignore="$APP_MODULE" \
    --ignore=github.com/dop251/goja/ftoa \
    --ignore=github.com/guigui-gui/guigui/basicwidget \
    --save_path="$REDISTRIBUTION_DIR" \
    2> "$WORK_DIR/go-licenses.log" || {
    cat "$WORK_DIR/go-licenses.log" >&2
    exit 1
}

# go-licenses cannot classify the Lucent license used by goja/ftoa or OFL-1.1
# bundled with guigui/basicwidget for save. Their full texts are present in the
# generated manifest, so include it with the collected license files.
cp "$MANIFEST" "$REDISTRIBUTION_DIR/licenses.json"
mkdir -p "$REDISTRIBUTION_DIR/shinonome"
cp "$SHINONOME_NOTICE" "$REDISTRIBUTION_DIR/shinonome/LICENSE.txt"

echo "Third-party licenses generated successfully."
