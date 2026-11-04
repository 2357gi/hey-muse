#!/bin/sh
# BusyBox init restarts the recognizer with the selected directory.
set -eu
cd "$(dirname "$0")"
case "$#:${1:-}" in
    1:tiny|1:base) ;;
    *) echo "usage: select-model tiny|base" >&2; exit 2 ;;
esac
for file in encoder_model.ort decoder_model_merged.ort tokens.txt; do
    if [ ! -s "models/$1/$file" ]; then echo "missing models/$1/$file" >&2; exit 1; fi
done
tmp=$(mktemp .selected-model.XXXXXX)
trap 'rm -f "$tmp"' EXIT
printf '%s\n' "$1" > "$tmp"
mv -f "$tmp" selected-model
# If the server is already stopped, init reads the selection on its next start.
killall -TERM echo-asr-server 2>/dev/null || :
