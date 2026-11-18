#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE="${ROOT}/icon/icon.png"
TEMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TEMP_DIR}"' EXIT

for command in sips iconutil python3 go; do
	if ! command -v "${command}" >/dev/null 2>&1; then
		echo "Required command not found: ${command}" >&2
		exit 1
	fi
done

iconset="${TEMP_DIR}/youtube2dvd.iconset"
mkdir -p "${iconset}"

create_png() {
	local size="$1"
	local name="$2"
	sips -s format png -z "${size}" "${size}" "${SOURCE}" --out "${iconset}/${name}" >/dev/null
}

create_png 16 icon_16x16.png
create_png 32 icon_16x16@2x.png
create_png 32 icon_32x32.png
create_png 64 icon_32x32@2x.png
create_png 128 icon_128x128.png
create_png 256 icon_128x128@2x.png
create_png 256 icon_256x256.png
create_png 512 icon_256x256@2x.png
create_png 512 icon_512x512.png
create_png 1024 icon_512x512@2x.png

iconutil -c icns "${iconset}" -o "${ROOT}/icon/icon.icns"

win_icons="${TEMP_DIR}/windows"
mkdir -p "${win_icons}"
for size in 16 32 48 64 128 256; do
	sips -s format png -z "${size}" "${size}" "${SOURCE}" --out "${win_icons}/${size}.png" >/dev/null
done

python3 - "${win_icons}" "${ROOT}/icon/icon.ico" <<'PY'
import pathlib
import struct
import sys

source_dir = pathlib.Path(sys.argv[1])
output = pathlib.Path(sys.argv[2])
sizes = (16, 32, 48, 64, 128, 256)
images = [(size, (source_dir / f"{size}.png").read_bytes()) for size in sizes]

offset = 6 + 16 * len(images)
header = struct.pack("<HHH", 0, 1, len(images))
entries = bytearray()
payload = bytearray()
for size, data in images:
	entries.extend(struct.pack(
		"<BBBBHHII",
		0 if size == 256 else size,
		0 if size == 256 else size,
		0,
		0,
		1,
		32,
		len(data),
		offset,
	))
	payload.extend(data)
	offset += len(data)

output.write_bytes(header + entries + payload)
PY

go run github.com/akavel/rsrc -arch amd64 -ico "${ROOT}/icon/icon.ico" -o "${ROOT}/rsrc_windows_amd64.syso"
go run github.com/akavel/rsrc -arch arm64 -ico "${ROOT}/icon/icon.ico" -o "${ROOT}/rsrc_windows_arm64.syso"
