#!/usr/bin/env bash
set -euo pipefail

RESET_TUNNEL=false
if [[ "${1-}" == "--reset-tunnel" ]]; then
  RESET_TUNNEL=true
fi

[[ -x ./cli-proxy-api ]] || go build -o cli-proxy-api ./cmd/server
pgrep -f "[\/]cli-proxy-api" >/dev/null || nohup ./cli-proxy-api > logs.setup.cli.log 2>&1 &

start_tunnel() {
  if [[ "$RESET_TUNNEL" == true ]]; then
    pkill -f "cloudflared tunnel --url localhost:8317" 2>/dev/null || true
    sleep 1
    rm -f logs.setup.cloudflared.log
  fi

  if ! pgrep -f "cloudflared tunnel --url localhost:8317" >/dev/null; then
    nohup cloudflared tunnel --url localhost:8317 > logs.setup.cloudflared.log 2>&1 &
  fi
}

read_tunnel_url() {
  python3 - <<'PY2'
import re
from pathlib import Path
p = Path('logs.setup.cloudflared.log')
if not p.exists():
    print('')
else:
    m = re.search(r'https://[a-z0-9-]+\.trycloudflare\.com', p.read_text(errors='ignore'))
    print(m.group(0) if m else '')
PY2
}

wait_for_url() {
  for _ in {1..30}; do
    url="$(read_tunnel_url)"
    [[ -n "$url" ]] && echo "$url" && return 0
    sleep 1
  done
  return 1
}

start_tunnel
if wait_for_url; then
  exit 0
fi

# Auto recovery: keep old behavior by default, but self-heal once when log/url missing.
if [[ "$RESET_TUNNEL" == false ]]; then
  echo "Tunnel URL chưa thấy trong log, thử reset tunnel 1 lần..."
  RESET_TUNNEL=true
  start_tunnel
  if wait_for_url; then
    exit 0
  fi
fi

echo "Tunnel URL chưa thấy trong log: logs.setup.cloudflared.log"
if [[ -f logs.setup.cloudflared.log ]]; then
  echo "--- Nội dung log cloudflared ---"
  cat logs.setup.cloudflared.log
  echo "--- Kết thúc log ---"
fi
echo "Gợi ý: chạy 'bash setup.sh --reset-tunnel' để ép tạo tunnel mới."
exit 1
