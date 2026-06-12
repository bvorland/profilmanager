#!/usr/bin/env bash
# probe-azd-config-dir.sh
#
# Hypothesis: AZD_CONFIG_DIR=$T causes 'azd' to write only under $T.

set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=_common.sh
. "$HERE/_common.sh"

PROBE_ID="azd-config-dir"
HYPOTHESIS='AZD_CONFIG_DIR=$T fully isolates azd config writes under $T'
EXPECTED="After 'azd config list' with AZD_CONFIG_DIR=\$T, \$T contains azd state and \$HOME/.azd mtime is unchanged"

if ! command_available azd; then
  emit_probe_result \
    --test "$PROBE_ID" --tool azd --category config-dir \
    --hypothesis "$HYPOTHESIS" --expected "$EXPECTED" \
    --skipped true --skip-reason "azd CLI not found on PATH"
  exit 0
fi

T="$(probe_temp_dir "$PROBE_ID")"
HOME_AZD="$(home_azd_dir)"
HOME_MTIME_BEFORE="$(dir_mtime_seconds "$HOME_AZD")"
T_BEFORE_COUNT="$(dir_listing_count "$T")"

START_MS="$(date +%s%3N 2>/dev/null || python3 -c 'import time; print(int(time.time()*1000))')"
STDOUT="$(AZD_CONFIG_DIR="$T" azd config list --output json 2>/tmp/pm-isol-azd-err.$$ || true)"
EXIT=$?
STDERR="$(cat /tmp/pm-isol-azd-err.$$ 2>/dev/null || true)"
rm -f /tmp/pm-isol-azd-err.$$ 2>/dev/null
END_MS="$(date +%s%3N 2>/dev/null || python3 -c 'import time; print(int(time.time()*1000))')"
DURATION=$((END_MS - START_MS))

HOME_MTIME_AFTER="$(dir_mtime_seconds "$HOME_AZD")"
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
  NOTES="\$T was not populated; 'azd config list' may be read-only on this version."
fi

ACTUAL="tempDir='$T'; tempFilesAfter=[$T_AFTER_NAMES]; homeAzdMtimeUnchanged=$HOME_UNCHANGED; azdExit=$EXIT"

emit_probe_result \
  --test "$PROBE_ID" --tool azd --category config-dir \
  --hypothesis "$HYPOTHESIS" --expected "$EXPECTED" \
  --actual "$ACTUAL" --isolated "$ISOLATED" \
  --tool-version "$(tool_version azd)" \
  --notes "$NOTES" --duration-ms "$DURATION"

remove_probe_temp_dir "$T"
exit 0
