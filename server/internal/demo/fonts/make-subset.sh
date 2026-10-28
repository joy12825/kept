#!/bin/sh
# Regenerates NotoSansSC-subset.ttf: Noto Sans SC Regular cut down to the
# characters the demo data uses. Run it after changing the sample data.
#
#   ./make-subset.sh path/to/NotoSansSC[wght].ttf
#
# The variable font comes from https://github.com/google/fonts/tree/main/ofl/notosanssc
# (SIL Open Font License 1.1, see OFL.txt). Needs fontTools; the Homebrew
# ocrmypdf environment ships one, set PYTHON to its interpreter if needed.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
src=$1
py=${PYTHON:-python3}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Every character in the demo sources, plus printable ASCII.
cat "$here/../data.go" "$here/../documents.go" "$here/../pdf.go" "$here/../books.go" > "$tmp/text.txt"

# A static Regular works too: the instancer refuses it, so it is used as is.
"$py" -m fontTools.varLib.instancer "$src" wght=400 -q -o "$tmp/regular.ttf" 2>/dev/null || cp "$src" "$tmp/regular.ttf"
"$py" -m fontTools.subset "$tmp/regular.ttf" \
	--text-file="$tmp/text.txt" --unicodes=U+0020-007E,U+00A5,U+FF0C,U+3002 \
	--no-hinting --layout-features='' --name-IDs='*' \
	--output-file="$here/NotoSansSC-subset.ttf"
ls -l "$here/NotoSansSC-subset.ttf"
