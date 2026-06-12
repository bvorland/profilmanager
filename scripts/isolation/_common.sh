#!/usr/bin/env bash
# scripts/isolation/_common.sh
#
# Shared helpers for probe-*.sh scripts. Source from each probe:
#   . "$(dirname "$0")/_common.sh"
#
# Contract:
#   - Every probe emits exactly one JSON object to stdout.
#   - Every probe exits 0.
#   - No writes outside ${TMPDIR:-/tmp}/pm-isolation-<probeId>-$$/.
#   - Live-cloud calls are detected and skipped, never failed.

set -u

PROBE_VERSION="1.0.0"
PROBE_STARTED_AT_MS=$(date +%s%3N 2>/dev/null || python3 -c 'import time; print(int(time.time()*1000))')

iso_timestamp() {
  date -u +"%Y-%m-%dT%H:%M:%S.%3NZ" 2>/dev/null || date -u +"%Y-%m-%dT%H:%M:%SZ"
}

host_info_json() {
  local os arch release pwsh_ver
  case "$(uname -s)" in
    Linux)   os="linux" ;;
    Darwin)  os="macos" ;;
    CYGWIN*|MINGW*|MSYS*) os="windows" ;;
    *)       os="unknown" ;;
  esac
  arch="$(uname -m)"
  release="$(uname -r 2>/dev/null || echo unknown)"
  pwsh_ver=""
  cat <<EOF
{"os":"$os","os_release":"$release","arch":"$arch","pwsh_version":"$pwsh_ver"}
EOF
}

command_available() {
  command -v "$1" >/dev/null 2>&1
}

tool_version() {
  local name="$1"
  if ! command_available "$name"; then echo ""; return; fi
  local out
  out="$("$name" --version 2>&1 || true)"
  # Prefer first line that contains a version-like token and is not WARNING/ERROR.
  local pick
  pick="$(printf '%s\n' "$out" | grep -Ev '^(WARNING|ERROR)' | grep -E '[0-9]+\.[0-9]+' | head -n 1 | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
  if [ -z "$pick" ]; then
    pick="$(printf '%s\n' "$out" | sed -n '1p' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
  fi
  echo "$pick"
}

probe_temp_dir() {
  local probe_id="$1"
  local root="${TMPDIR:-/tmp}/pm-isolation-$probe_id-$$"
  rm -rf "$root" 2>/dev/null || true
  mkdir -p "$root"
  echo "$root"
}

remove_probe_temp_dir() {
  local path="$1"
  if [ -n "$path" ] && [ -d "$path" ]; then
    rm -rf "$path" 2>/dev/null || true
  fi
}

dir_mtime_seconds() {
  local path="$1"
  if [ ! -e "$path" ]; then echo ""; return; fi
  stat -c %Y "$path" 2>/dev/null || stat -f %m "$path" 2>/dev/null || echo ""
}

dir_listing_count() {
  local path="$1"
  if [ ! -d "$path" ]; then echo 0; return; fi
  ls -A "$path" 2>/dev/null | wc -l | tr -d ' '
}

dir_listing_names() {
  local path="$1"
  if [ ! -d "$path" ]; then echo ""; return; fi
  ls -A "$path" 2>/dev/null | paste -sd ',' -
}

home_azure_dir() { echo "${HOME}/.azure"; }
home_azd_dir()   { echo "${HOME}/.azd"; }

az_not_logged_in() {
  local text="$1"
  printf '%s' "$text" | grep -qE 'Please run .az login.|az login --use-device-code|No subscription found|run .az login. to set up an account'
}

azd_not_logged_in() {
  local text="$1"
  printf '%s' "$text" | grep -qE 'not logged in|azd auth login|no credentials configured|fetching token: failed'
}

# json_escape: escape a string for safe inclusion in a JSON string value.
json_escape() {
  if command_available python3; then
    python3 -c 'import json,sys; print(json.dumps(sys.stdin.read())[1:-1])'
  else
    # Fallback: replace \  "  newline  CR  tab.
    sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' \
        -e ':a;N;$!ba;s/\n/\\n/g' \
        -e 's/\r/\\r/g' -e 's/\t/\\t/g'
  fi
}

# emit_probe_result: prints the JSON envelope. Args are passed as named flags.
#   --test ID --tool TOOL --category CAT --hypothesis H --expected E --actual A
#   --isolated true|false|null  --skipped true|false  --skip-reason TEXT
#   --tool-version V  --notes 'note1;;note2'  --duration-ms N
emit_probe_result() {
  local test="" tool="" category="" hypothesis="" expected="" actual=""
  local isolated="null" skipped="false" skip_reason="" tool_version=""
  local notes_raw="" duration_ms="-1"
  while [ $# -gt 0 ]; do
    case "$1" in
      --test)           test="$2"; shift 2 ;;
      --tool)           tool="$2"; shift 2 ;;
      --category)       category="$2"; shift 2 ;;
      --hypothesis)     hypothesis="$2"; shift 2 ;;
      --expected)       expected="$2"; shift 2 ;;
      --actual)         actual="$2"; shift 2 ;;
      --isolated)       isolated="$2"; shift 2 ;;
      --skipped)        skipped="$2"; shift 2 ;;
      --skip-reason)    skip_reason="$2"; shift 2 ;;
      --tool-version)   tool_version="$2"; shift 2 ;;
      --notes)          notes_raw="$2"; shift 2 ;;
      --duration-ms)    duration_ms="$2"; shift 2 ;;
      *) shift ;;
    esac
  done

  if [ "$duration_ms" -lt 0 ] 2>/dev/null; then
    local now_ms
    now_ms="$(date +%s%3N 2>/dev/null || python3 -c 'import time; print(int(time.time()*1000))')"
    duration_ms=$((now_ms - PROBE_STARTED_AT_MS))
  fi

  local test_e hypothesis_e expected_e actual_e skip_reason_e tool_version_e
  test_e="$(printf '%s' "$test" | json_escape)"
  hypothesis_e="$(printf '%s' "$hypothesis" | json_escape)"
  expected_e="$(printf '%s' "$expected" | json_escape)"
  actual_e="$(printf '%s' "$actual" | json_escape)"
  skip_reason_e="$(printf '%s' "$skip_reason" | json_escape)"
  tool_version_e="$(printf '%s' "$tool_version" | json_escape)"

  # Build notes JSON array from ';;'-separated string.
  local notes_json="[]"
  if [ -n "$notes_raw" ]; then
    local items=""
    local IFS=';'
    set -f
    # Split on ';;' by temporarily replacing it with a unit-separator char.
    local sentinel
    sentinel=$(printf '\037')
    local normalized
    normalized="$(printf '%s' "$notes_raw" | sed "s/;;/${sentinel}/g")"
    notes_json="["
    local first=1
    local IFS="${sentinel}"
    for n in $normalized; do
      if [ -z "$n" ]; then continue; fi
      local esc
      esc="$(printf '%s' "$n" | json_escape)"
      if [ $first -eq 1 ]; then
        notes_json="${notes_json}\"${esc}\""
        first=0
      else
        notes_json="${notes_json},\"${esc}\""
      fi
    done
    notes_json="${notes_json}]"
    set +f
  fi

  # isolated normalization to JSON literal.
  local isolated_json
  case "$isolated" in
    true|false|null) isolated_json="$isolated" ;;
    *) isolated_json="null" ;;
  esac

  # Nullable string fields.
  local skip_reason_json tool_version_json
  if [ -z "$skip_reason" ]; then skip_reason_json="null"; else skip_reason_json="\"$skip_reason_e\""; fi
  if [ -z "$tool_version" ]; then tool_version_json="null"; else tool_version_json="\"$tool_version_e\""; fi

  local skipped_json
  case "$skipped" in
    true) skipped_json="true" ;;
    *)    skipped_json="false" ;;
  esac

  local host_json generated_at
  host_json="$(host_info_json)"
  generated_at="$(iso_timestamp)"

  cat <<EOF
{
  "test": "$test_e",
  "tool": "$tool",
  "category": "$category",
  "hypothesis": "$hypothesis_e",
  "expected": "$expected_e",
  "actual": "$actual_e",
  "isolated": $isolated_json,
  "skipped": $skipped_json,
  "skip_reason": $skip_reason_json,
  "duration_ms": $duration_ms,
  "notes": $notes_json,
  "host": $host_json,
  "tool_version": $tool_version_json,
  "probe_version": "$PROBE_VERSION",
  "generated_at": "$generated_at"
}
EOF
}
