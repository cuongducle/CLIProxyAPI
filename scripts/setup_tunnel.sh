#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

[[ -f config.yaml ]] || cp config.example.yaml config.yaml
[[ -x ./cli-proxy-api ]] || go build -o cli-proxy-api ./cmd/server
pgrep -f "[\/]cli-proxy-api" >/dev/null || nohup ./cli-proxy-api > logs.setup.cli.log 2>&1 &

if ! pgrep -f "cloudflared tunnel --url localhost:8317" >/dev/null; then
  nohup cloudflared tunnel --url localhost:8317 > logs.setup.cloudflared.log 2>&1 &
fi

for _ in {1..30}; do
  url="$(python3 - <<'PY'
import re
from pathlib import Path
p = Path('logs.setup.cloudflared.log')
if not p.exists():
    print('')
else:
    m = re.search(r'https://[a-z0-9-]+\.trycloudflare\.com', p.read_text(errors='ignore'))
    print(m.group(0) if m else '')
PY
)"
  [[ -n "$url" ]] && echo "$url" && exit 0
  sleep 1
done

echo "Tunnel URL chưa thấy trong log: logs.setup.cloudflared.log"
exit 1
