#!/bin/bash
# Builds a dedicated Chrome profile for driving examples/saucedemo live, with
# Chrome's own "found in a data breach" password nag pre-disabled.
#
# saucedemo's published test password (secret_sauce) is public enough to be in
# breach corpora Chrome's Safe Browsing checks against, so a fresh profile logs
# a "Change your password" dialog on every log_in. That dialog is a native
# browser overlay, not a page element — it silently swallows clicks meant for
# the page underneath it, which is exactly the kind of failure a plan replay
# can't tell apart from a real bug. No Chrome flag actually suppresses this
# check; it has to be patched into the profile's own Preferences file.
#
# Safe to re-run any time: it tears down the profile and rebuilds it from
# scratch. Run this once, then drive examples/saucedemo with:
#
#   sightmap browser start --detach --url https://www.saucedemo.com/ \
#     --sightmap-dir examples/saucedemo/.sightmap --cdp-port 7892 --port 7891 \
#     --profile ~/.sightmap/profiles/sightkick-demo
set -e

PROFILE_DIR=~/.sightmap/profiles/sightkick-demo
CDP_PORT=7892
SERVER_PORT=7891
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# tear down anything running, then remove any existing profile
lsof -ti:$CDP_PORT,$SERVER_PORT 2>/dev/null | xargs kill 2>/dev/null
sleep 1
rm -rf "$PROFILE_DIR"

# materialize a fresh profile (throwaway launch, just to create it)
cd "$REPO_ROOT"
sightmap browser start --detach --url https://www.saucedemo.com/ \
  --sightmap-dir examples/saucedemo/.sightmap --cdp-port $CDP_PORT --port $SERVER_PORT \
  --profile "$PROFILE_DIR"
sleep 2
sightmap browser stop
lsof -ti:$CDP_PORT,$SERVER_PORT 2>/dev/null | xargs kill 2>/dev/null
sleep 1

python3 - <<PYEOF
import json, os

path = os.path.expanduser("$PROFILE_DIR/Default/Preferences")
prefs = json.load(open(path)) if os.path.exists(path) else {}

prefs.setdefault("profile", {})["password_manager_leak_detection"] = False
prefs["profile"]["password_manager_enabled"] = False
prefs["credentials_enable_service"] = False

json.dump(prefs, open(path, "w"))
print("profile ready:", path)
PYEOF
