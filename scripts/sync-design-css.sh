#!/bin/sh
# Copy design/tokens.css and the wink icon into each app, and refresh the
# mobile.css marker block at the bottom of that app's input.css. Docker
# builds only see the app directory, so the copies are what ships.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
apps="account notes assistant calendar spend people email"
for app in $apps; do
  css="$root/apps/$app/web/static/css"
  static="$root/apps/$app/web/static"
  cp "$root/design/tokens.css" "$css/tokens.css"
  cp "$root/design/icon.svg" "$static/icon.svg"
  cp "$root/design/icon.png" "$static/icon.png"
  cp "$root/design/apple-touch-icon.png" "$static/apple-touch-icon.png"
  input="$css/input.css"
  tmp=$(mktemp)
  awk '
    BEGIN { skip = 0 }
    /\/\* BEGIN design\/mobile.css \*\// { skip = 1; next }
    /\/\* END design\/mobile.css \*\// { skip = 0; next }
    skip == 0 { print }
  ' "$input" > "$tmp"
  # Trim trailing blank lines so the marker block is appended once.
  printf '%s\n' "$(cat "$tmp")" > "$input"
  rm "$tmp"
  {
    printf '\n/* BEGIN design/mobile.css */\n'
    cat "$root/design/mobile.css"
    printf '\n/* END design/mobile.css */\n'
  } >> "$input"
done
