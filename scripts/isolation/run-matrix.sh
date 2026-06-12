#!/usr/bin/env bash
# scripts/isolation/run-matrix.sh
#
# Discovers every probe-*.sh in this directory, runs each, aggregates the JSON
# envelopes into a single report, prints a human-readable summary, and always
# exits 0 in v1.
#
# Usage:
#   bash scripts/isolation/run-matrix.sh
#   bash scripts/isolation/run-matrix.sh --output isolation-report.json
#   bash scripts/isolation/run-matrix.sh --quiet
#
# Note: this runs ONLY the bash probes (.sh). On Windows / mixed environments,
# use run-matrix.ps1 to drive the PowerShell probes (which cover the full
# matrix; the .sh probes mirror only the most important claims).

set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
OUTPUT_FILE=""
QUIET="false"

while [ $# -gt 0 ]; do
  case "$1" in
    --output) OUTPUT_FILE="$2"; shift 2 ;;
    --quiet)  QUIET="true"; shift ;;
    -h|--help)
      sed -n '2,15p' "$0"
      exit 0
      ;;
    *) shift ;;
  esac
done

# shellcheck source=_common.sh
. "$HERE/_common.sh"

PROBES=()
while IFS= read -r f; do
  [ -n "$f" ] && PROBES+=("$f")
done < <(find "$HERE" -maxdepth 1 -name 'probe-*.sh' -type f | sort)
# Also discover package-style probes: any sub-directory containing 'probe.sh'.
while IFS= read -r f; do
  [ -n "$f" ] && PROBES+=("$f")
done < <(find "$HERE" -mindepth 2 -maxdepth 2 -name 'probe.sh' -type f | sort)

if [ "$QUIET" = "false" ]; then
  echo ""
  echo "Running ${#PROBES[@]} probes from $HERE"
  printf '=%.0s' {1..60}; echo
fi

# Accumulate raw probe JSON strings into a temp file (one envelope per line is fragile;
# instead we collect them into an array of inline JSON blobs).
PROBE_JSON_FILE="${TMPDIR:-/tmp}/pm-iso-matrix-$$.json"
: > "$PROBE_JSON_FILE"

TOTAL=0
ISOLATED=0
LEAKED=0
SKIPPED=0
DESCRIPTIVE=0
ERRORS=0

for probe in "${PROBES[@]}"; do
  pname="$(basename "$probe")"
  # Derive a stable, collision-free key. Flat probes: 'probe-foo.sh' -> 'foo'.
  # Package probes: 'foo/probe.sh' -> 'foo' (parent dir name).
  if [ "$pname" = "probe.sh" ]; then
    pkey="$(basename "$(dirname "$probe")")"
  else
    pkey="${pname#probe-}"
    pkey="${pkey%.sh}"
  fi
  if [ "$QUIET" = "false" ]; then printf '• %s' "$pkey"; fi
  OUT_FILE="${TMPDIR:-/tmp}/pm-iso-out.$$.${pkey}.txt"
  ERR_FILE="${TMPDIR:-/tmp}/pm-iso-err.$$.${pkey}.txt"
  bash "$probe" >"$OUT_FILE" 2>"$ERR_FILE"
  PROBE_EXIT=$?
  STDOUT="$(cat "$OUT_FILE" 2>/dev/null || true)"
  STDERR="$(cat "$ERR_FILE" 2>/dev/null || true)"
  rm -f "$OUT_FILE" "$ERR_FILE" 2>/dev/null

  # Try to parse JSON via python3 (preferred) — fall back to crude inspection.
  STATUS="ERROR"
  IS_SKIPPED="false"
  IS_ISOLATED="null"
  if command_available python3; then
    PARSE_SCRIPT='
import json, sys
try:
    obj = json.loads(sys.stdin.read())
    sk = obj.get("skipped")
    iso = obj.get("isolated")
    iso_s = "null" if iso is None else ("true" if iso else "false")
    sk_s  = "true" if sk else "false"
    print(f"OK\t{sk_s}\t{iso_s}")
except Exception as e:
    print("BAD\t" + str(e).replace("\t"," "))
'
    PARSED="$(printf '%s' "$STDOUT" | python3 -c "$PARSE_SCRIPT" 2>&1 || echo "BAD")"
    HEAD="$(printf '%s\n' "$PARSED" | awk -F'\t' '{print $1; exit}')"
    if [ "$HEAD" = "OK" ]; then
      IS_SKIPPED="$(printf '%s\n' "$PARSED"  | awk -F'\t' '{print $2; exit}')"
      IS_ISOLATED="$(printf '%s\n' "$PARSED" | awk -F'\t' '{print $3; exit}')"
      if [ "$IS_SKIPPED" = "true" ]; then
        STATUS="SKIP"; SKIPPED=$((SKIPPED+1))
      elif [ "$IS_ISOLATED" = "null" ]; then
        STATUS="DESC"; DESCRIPTIVE=$((DESCRIPTIVE+1))
      elif [ "$IS_ISOLATED" = "true" ]; then
        STATUS="PASS"; ISOLATED=$((ISOLATED+1))
      else
        STATUS="LEAK"; LEAKED=$((LEAKED+1))
      fi
    else
      ERRORS=$((ERRORS+1))
      # Synthesize an error envelope so the aggregate is still complete.
      STDOUT=$(cat <<EOF
{"test":"${pkey}","tool":"unknown","category":"harness-error","hypothesis":"probe should emit valid JSON","expected":"one JSON object on stdout, exit 0","actual":"probeExit=$PROBE_EXIT; parser=$PARSED","isolated":null,"skipped":false,"skip_reason":null,"duration_ms":0,"notes":["probe failed to emit valid JSON"],"host":$(host_info_json),"tool_version":null,"probe_version":"unknown","generated_at":"$(iso_timestamp)","_harness_error":true}
EOF
)
    fi
  else
    # No python3 available — naive fallbacks.
    if printf '%s' "$STDOUT" | grep -q '"skipped"[[:space:]]*:[[:space:]]*true'; then
      STATUS="SKIP"; SKIPPED=$((SKIPPED+1)); IS_SKIPPED="true"
    elif printf '%s' "$STDOUT" | grep -q '"isolated"[[:space:]]*:[[:space:]]*true'; then
      STATUS="PASS"; ISOLATED=$((ISOLATED+1)); IS_ISOLATED="true"
    elif printf '%s' "$STDOUT" | grep -q '"isolated"[[:space:]]*:[[:space:]]*false'; then
      STATUS="LEAK"; LEAKED=$((LEAKED+1)); IS_ISOLATED="false"
    elif printf '%s' "$STDOUT" | grep -q '"isolated"[[:space:]]*:[[:space:]]*null'; then
      STATUS="DESC"; DESCRIPTIVE=$((DESCRIPTIVE+1))
    else
      STATUS="ERROR"; ERRORS=$((ERRORS+1))
    fi
  fi

  TOTAL=$((TOTAL+1))

  # Append this probe's JSON to the accumulated array file.
  if [ -s "$PROBE_JSON_FILE" ]; then echo "," >> "$PROBE_JSON_FILE"; fi
  printf '%s' "$STDOUT" >> "$PROBE_JSON_FILE"

  if [ "$QUIET" = "false" ]; then
    case "$STATUS" in
      PASS)  printf '  \033[32m[PASS]\033[0m\n' ;;
      LEAK)  printf '  \033[31m[LEAK]\033[0m\n' ;;
      SKIP)  printf '  \033[90m[SKIP]\033[0m\n' ;;
      DESC)  printf '  \033[33m[DESC]\033[0m\n' ;;
      ERROR) printf '  \033[31m[ERROR]\033[0m\n' ;;
      *)     printf '  [%s]\n' "$STATUS" ;;
    esac
  fi
done

GENERATED_AT="$(iso_timestamp)"
HOST_JSON="$(host_info_json)"

REPORT_JSON=$(cat <<EOF
{
  "schema": "isolation-matrix/v1",
  "generated_at": "$GENERATED_AT",
  "host": $HOST_JSON,
  "summary": {
    "total":       $TOTAL,
    "isolated":    $ISOLATED,
    "leaked":      $LEAKED,
    "skipped":     $SKIPPED,
    "descriptive": $DESCRIPTIVE,
    "errors":      $ERRORS
  },
  "probes": [
$(cat "$PROBE_JSON_FILE")
  ]
}
EOF
)

rm -f "$PROBE_JSON_FILE" 2>/dev/null

if [ -n "$OUTPUT_FILE" ]; then
  OUT_DIR="$(dirname "$OUTPUT_FILE")"
  [ -d "$OUT_DIR" ] || mkdir -p "$OUT_DIR"
  printf '%s\n' "$REPORT_JSON" > "$OUTPUT_FILE"
fi

if [ "$QUIET" = "false" ]; then
  echo ""
  printf '=%.0s' {1..60}; echo
  echo "Summary"
  printf '  total       : %d\n' "$TOTAL"
  printf '  \033[32misolated    : %d\033[0m\n' "$ISOLATED"
  if [ "$LEAKED" -gt 0 ]; then
    printf '  \033[31mleaked      : %d\033[0m\n' "$LEAKED"
  else
    printf '  leaked      : %d\n' "$LEAKED"
  fi
  printf '  \033[90mskipped     : %d\033[0m\n' "$SKIPPED"
  printf '  \033[33mdescriptive : %d\033[0m\n' "$DESCRIPTIVE"
  if [ "$ERRORS" -gt 0 ]; then
    printf '  \033[31merrors      : %d\033[0m\n' "$ERRORS"
  else
    printf '  errors      : %d\n' "$ERRORS"
  fi
  if [ -n "$OUTPUT_FILE" ]; then
    echo ""
    echo "Full report: $OUTPUT_FILE"
  fi
  echo ""
  echo "Exit code: 0 (v1 always exits 0 — use --strict in a future version to fail on leaks)"
else
  printf '%s\n' "$REPORT_JSON"
fi

exit 0
