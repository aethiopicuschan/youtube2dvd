
#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

APP_PACKAGE="."
APP_MODULE="$(sed -n 's/^module[[:space:]]\+//p' go.mod | head -n 1)"
GENERATED_DIR="internal/licenses/generated"
REDISTRIBUTION_DIR="dist/THIRD_PARTY_LICENSES"

if ! command -v jq > /dev/null; then
    echo "jq is required to generate the license manifest." >&2
    exit 1
fi

trap 'rm -f "$GENERATED_DIR/licenses.json.tmp" "$GENERATED_DIR/licenses.json.tmp2"' EXIT

echo "Generating third-party licenses..."

mkdir -p "$GENERATED_DIR"

# Generate license metadata and full license texts.
go-licenses report "$APP_PACKAGE" \
    --ignore="$APP_MODULE" \
    --template=scripts/licenses.json.tmpl \
    > "$GENERATED_DIR/licenses.json.tmp"

# video2dvd embeds the Shinonome bitmap font for Japanese DVD menu text. It is
# not a Go module, so go-licenses cannot see it; take the notice shipped with
# the library.
VIDEO2DVD_DIR="$(go list -m -f '{{.Dir}}' github.com/aethiopicuschan/video2dvd)"
SHINONOME_NOTICE="$VIDEO2DVD_DIR/third_party_licenses/shinonome-LICENSE.txt"

jq --rawfile text "$SHINONOME_NOTICE" '.packages += [{
        name: "Shinonome font family (embedded in github.com/aethiopicuschan/video2dvd)",
        version: "0.9.11",
        license: "Public Domain",
        license_url: "http://openlab.ring.gr.jp/efont/shinonome/",
        text: $text
    }]' "$GENERATED_DIR/licenses.json.tmp" \
    > "$GENERATED_DIR/licenses.json.tmp2"

mv "$GENERATED_DIR/licenses.json.tmp2" \
    "$GENERATED_DIR/licenses.json.tmp"

# Validate generated JSON before replacing the existing manifest.
go run ./cmd/validate-licenses \
    "$GENERATED_DIR/licenses.json.tmp"

mv "$GENERATED_DIR/licenses.json.tmp" \
    "$GENERATED_DIR/licenses.json"

# Collect redistribution artifacts.
rm -rf "$REDISTRIBUTION_DIR"

go-licenses save "$APP_PACKAGE" \
    --ignore="$APP_MODULE" \
    --ignore=github.com/dop251/goja/ftoa \
    --ignore=github.com/guigui-gui/guigui/basicwidget \
    --save_path="$REDISTRIBUTION_DIR"

# go-licenses cannot classify the Lucent license used by goja/ftoa or OFL-1.1
# bundled with guigui/basicwidget for save. Their full texts are present in the
# generated manifest, so include it with the collected license files.
cp "$GENERATED_DIR/licenses.json" "$REDISTRIBUTION_DIR/licenses.json"
mkdir -p "$REDISTRIBUTION_DIR/shinonome"
cp "$SHINONOME_NOTICE" "$REDISTRIBUTION_DIR/shinonome/LICENSE.txt"

echo "Third-party licenses generated successfully."
