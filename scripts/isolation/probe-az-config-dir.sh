#!/usr/bin/env bash
# probe-az-config-dir.sh
#
# Hypothesis: setting AZURE_CONFIG_DIR=$T causes 'az' to write only under $T,
# and never mutates $HOME/.azure during the call.

set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=_common.sh
. "$HERE/_common.sh"

PROBE_ID="az-config-dir"
HYPOTHESIS='AZURE_CONFIG_DIR=$T fully isolates az config/cache writes under $T'
EXPECTED="After 'az config get' with AZURE_CONFIG_DIR=\$T, \$T is populated and \$HOME/.azure mtime is unchanged"

if ! command_available az; then
  emit_probe_result \
    --test "$PROBE_ID" --tool az --category config-dir \
    --hypothesis "$HYPOTHESIS" --expected "$EXPECTED" \
    --skipped true --skip-reason "az CLI not found on PATH"
  exit 0
fi

T="$(probe_temp_dir "$PROBE_ID")"
HOME_AZURE="$(home_azure_dir)"
HOME_MTIME_BEFORE="$(dir_mtime_seconds "$HOME_AZURE")"
T_BEFORE_COUNT="$(dir_listing_count "$T")"

START_MS="$(date +%s%3N 2>/dev/null || python3 -c 'import time; print(int(time.time()*1000))')"
STDOUT="$(AZURE_CONFIG_DIR="$T" az config get --output json 2>/tmp/pm-isol-az-err.$$ || true)"
EXIT=$?
STDERR="$(cat /tmp/pm-isol-az-err.$$ 2>/dev/null || true)"
rm -f /tmp/pm-isol-az-err.$$ 2>/dev/null
END_MS="$(date +%s%3N 2>/dev/null || python3 -c 'import time; print(int(time.time()*1000))')"
DURATION=$((END_MS - START_MS))

HOME_MTIME_AFTER="$(dir_mtime_seconds "$HOME_AZURE")"
T_AFTER_COUNT="$(dir_listing_count "$T")"
T_AFTER_NAMES="$(dir_listing_names "$T")"

HOME_UNCHANGED="true"
if [ -n "$HOME_MTIME_BEFORE" ] && [ -n "$HOME_MTIME_AFTER" ] && [ "$HOME_MTIME_BEFORE" != "$HOME_MTIME_AFTER" ]; then
  HOME_UNCHANGED="false"
fi

T_POPULATED="false"
if [ "$T_AFTER_COUNT" -gt "$T_BEFORE_COUNT" ]; then T_POPULATED="true"; fi

ISOLATED="false"
if [ "$T_POPULATED" = "true" ] && [ "$HOME_UNCHANGED" = "true" ]; then ISOLATED="true"; fi

NOTES=""
if [ "$T_POPULATED" = "false" ]; then
  NOTES="Probe dir was not populated; 'az config get' may be read-only on this version."
fi

ACTUAL="tempDir='$T'; tempFilesAfter=[$T_AFTER_NAMES]; homeAzureMtimeUnchanged=$HOME_UNCHANGED; azExit=$EXIT"

emit_probe_result \
  --test "$PROBE_ID" --tool az --category config-dir \
  --hypothesis "$HYPOTHESIS" --expected "$EXPECTED" \
  --actual "$ACTUAL" --isolated "$ISOLATED" \
  --tool-version "$(tool_version az)" \
  --notes "$NOTES" --duration-ms "$DURATION"

remove_probe_temp_dir "$T"
exit 0
