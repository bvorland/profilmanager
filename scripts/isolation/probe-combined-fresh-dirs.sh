#!/usr/bin/env bash
# probe-combined-fresh-dirs.sh
#
# Both AZURE_CONFIG_DIR and AZD_CONFIG_DIR point at distinct, fresh temp dirs.
# Verify both temp dirs receive state and neither $HOME/.azure nor $HOME/.azd
# is mutated.

set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=_common.sh
. "$HERE/_common.sh"

PROBE_ID="combined-fresh-dirs"
HYPOTHESIS='AZURE_CONFIG_DIR and AZD_CONFIG_DIR isolate az and azd state simultaneously into distinct dirs'
EXPECTED="after az+azd reads, \$T_azure populated, \$T_azd populated, \$HOME/.azure unchanged, \$HOME/.azd unchanged"

HAS_AZ="false"; command_available az  && HAS_AZ="true"
HAS_AZD="false"; command_available azd && HAS_AZD="true"

if [ "$HAS_AZ" = "false" ] && [ "$HAS_AZD" = "false" ]; then
  emit_probe_result \
    --test "$PROBE_ID" --tool combined --category isolation \
    --hypothesis "$HYPOTHESIS" --expected "$EXPECTED" \
    --skipped true --skip-reason "neither az nor azd found on PATH"
  exit 0
fi

T_AZURE="$(probe_temp_dir "$PROBE_ID-azure")"
T_AZD="$(probe_temp_dir   "$PROBE_ID-azd")"
HOME_AZURE="$(home_azure_dir)"
HOME_AZD="$(home_azd_dir)"

M_AZURE_BEFORE="$(dir_mtime_seconds "$HOME_AZURE")"
M_AZD_BEFORE="$(dir_mtime_seconds   "$HOME_AZD")"

START_MS="$(date +%s%3N 2>/dev/null || python3 -c 'import time; print(int(time.time()*1000))')"
if [ "$HAS_AZ" = "true" ]; then
  AZURE_CONFIG_DIR="$T_AZURE" AZD_CONFIG_DIR="$T_AZD" az config get --output json >/dev/null 2>&1 || true
fi
if [ "$HAS_AZD" = "true" ]; then
  AZURE_CONFIG_DIR="$T_AZURE" AZD_CONFIG_DIR="$T_AZD" azd config list --output json >/dev/null 2>&1 || true
fi
END_MS="$(date +%s%3N 2>/dev/null || python3 -c 'import time; print(int(time.time()*1000))')"
DURATION=$((END_MS - START_MS))

M_AZURE_AFTER="$(dir_mtime_seconds "$HOME_AZURE")"
M_AZD_AFTER="$(dir_mtime_seconds   "$HOME_AZD")"
AZURE_COUNT="$(dir_listing_count "$T_AZURE")"
AZD_COUNT="$(dir_listing_count   "$T_AZD")"

AZURE_HOME_UNCHANGED="true"
AZD_HOME_UNCHANGED="true"
if [ -n "$M_AZURE_BEFORE" ] && [ -n "$M_AZURE_AFTER" ] && [ "$M_AZURE_BEFORE" != "$M_AZURE_AFTER" ]; then AZURE_HOME_UNCHANGED="false"; fi
if [ -n "$M_AZD_BEFORE" ]   && [ -n "$M_AZD_AFTER" ]   && [ "$M_AZD_BEFORE"   != "$M_AZD_AFTER" ];   then AZD_HOME_UNCHANGED="false"; fi

ISOLATED="true"
if [ "$HAS_AZ" = "true" ]; then
  [ "$AZURE_COUNT" -eq 0 ] && ISOLATED="false"
  [ "$AZURE_HOME_UNCHANGED" = "false" ] && ISOLATED="false"
fi
if [ "$HAS_AZD" = "true" ]; then
  [ "$AZD_COUNT" -eq 0 ] && ISOLATED="false"
  [ "$AZD_HOME_UNCHANGED" = "false" ] && ISOLATED="false"
fi

NOTES=""
if [ "$HAS_AZ" = "false" ]; then NOTES="${NOTES};;az not present — claim partially tested"; fi
if [ "$HAS_AZD" = "false" ]; then NOTES="${NOTES};;azd not present — claim partially tested"; fi

ACTUAL="azPresent=$HAS_AZ; azdPresent=$HAS_AZD; tempAzureFiles=$AZURE_COUNT; tempAzdFiles=$AZD_COUNT; homeAzureUnchanged=$AZURE_HOME_UNCHANGED; homeAzdUnchanged=$AZD_HOME_UNCHANGED"

TV=""
if [ "$HAS_AZ" = "true" ]; then TV="$(tool_version az)"; elif [ "$HAS_AZD" = "true" ]; then TV="$(tool_version azd)"; fi

emit_probe_result \
  --test "$PROBE_ID" --tool combined --category isolation \
  --hypothesis "$HYPOTHESIS" --expected "$EXPECTED" \
  --actual "$ACTUAL" --isolated "$ISOLATED" \
  --tool-version "$TV" \
  --notes "$NOTES" --duration-ms "$DURATION"

remove_probe_temp_dir "$T_AZURE"
remove_probe_temp_dir "$T_AZD"
exit 0
