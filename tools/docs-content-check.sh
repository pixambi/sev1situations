#!/bin/sh
# Fails if the built docs site holds something that must not be public (R-DOC-7): a private key, a token, a license key or an
# internal hostname. It reads the built site, so it also catches what the generator copied from a scenario.
set -eu
site="${1:-docs/public}"
[ -d "$site" ] || { echo "docs-content-check: $site does not exist. Build the site first." >&2; exit 2; }

found=0
check() {
  # $1 describes the pattern, $2 is an extended regular expression.
  if grep -rIEn -- "$2" "$site" >/tmp/docs-check.$$ 2>/dev/null; then
    echo "docs-content-check: found $1:" >&2
    cut -c1-160 /tmp/docs-check.$$ | head -5 >&2
    found=1
  fi
  rm -f /tmp/docs-check.$$
}

check "a private key" '-----BEGIN [A-Z ]*PRIVATE KEY-----'
check "a Vault token" '\b(hvs|hvb|hvr)\.[A-Za-z0-9_-]{20,}'
check "an AWS access key" '\bAKIA[0-9A-Z]{16}\b'
check "a GitHub token" '\bgh[pousr]_[A-Za-z0-9]{30,}'
check "a license key" '02MV4UU43BK5[A-Za-z0-9]{20,}'
check "an internal hostname" '[A-Za-z0-9.-]+\.(corp|internal|intranet|lan)\b'
check "an IBM or HashiCorp internal host" '[A-Za-z0-9.-]+\.(ibm\.com|hashicorp\.services)\b'

exit "$found"
