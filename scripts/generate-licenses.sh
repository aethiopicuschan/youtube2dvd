
#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

APP_PACKAGE="."
APP_MODULE="$(sed -n 's/^module[[:space:]]\+//p' go.mod | head -n 1)"
GENERATED_DIR="internal/licenses/generated"
REDISTRIBUTION_DIR="dist/THIRD_PARTY_LICENSES"

echo "Generating third-party licenses..."

mkdir -p "$GENERATED_DIR"

# Generate license metadata and full license texts.
go-licenses report "$APP_PACKAGE" \
    --ignore="$APP_MODULE" \
    --template=scripts/licenses.json.tmpl \
    > "$GENERATED_DIR/licenses.json.tmp"

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

echo "Third-party licenses generated successfully."
