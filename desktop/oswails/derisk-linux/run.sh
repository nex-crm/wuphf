#!/usr/bin/env bash
# Runs INSIDE the container. Builds the Wails desktop shell for Linux/WebKitGTK,
# runs it headless under xvfb+dbus, and captures proof that the heavy SPA renders
# and the webview connects to the in-process broker.
set -uo pipefail
export HOME=/root
export GOFLAGS=-mod=mod
# WebKitGTK uses GPU/dmabuf by default, which fails under xvfb. Force software.
export WEBKIT_DISABLE_DMABUF_RENDERER=1
export WEBKIT_DISABLE_COMPOSITING_MODE=1

echo "=== go build -tags 'desktop webkit2_41' ==="
cd /src || exit 90
go build -tags "desktop production webkit2_41" -ldflags "-w -s" -o /out/wuphf-desktop ./desktop/oswails 2>/out/build.log
RC=$?
echo "build rc=$RC"
if [ $RC -ne 0 ]; then echo "BUILD FAILED:"; tail -40 /out/build.log; exit "$RC"; fi
ls -lh /out/wuphf-desktop | awk '{print "linux binary:",$5}'

cat > /tmp/inner.sh <<'INNER'
#!/usr/bin/env bash
export DISPLAY=:99
/out/wuphf-desktop >/out/app.log 2>&1 &
APP=$!
# wait for the webview to connect to the in-process UI (port 7973)
for i in $(seq 1 40); do
  ss -tnH 2>/dev/null | grep -q '127.0.0.1:7973' && break
  sleep 1
done
sleep 5
{
  echo "--- LISTEN (in-process broker + UI in one process) ---"
  ss -tlnpH 2>/dev/null | grep -E '127.0.0.1:(7890|7973)'
  echo "--- ESTABLISHED (WebKitGTK webview <-> in-process broker) ---"
  ss -tnpH 2>/dev/null | grep '127.0.0.1:7973'
  echo "--- WebKit processes ---"
  pgrep -a -f 'WebKit|webkit' | head
  echo "--- curl in-process UI ---"
  curl -s -o /dev/null -w "HTTP %{http_code}\n" http://127.0.0.1:7973/
  echo "--- single process? (broker is inside the GUI binary) ---"
  pgrep -a -f 'wuphf-desktop' | head
} > /out/sockets.txt 2>&1
# screenshot the virtual root (xvfb gives a real framebuffer — no Spaces problem)
import -window root /out/linux-screenshot.png 2>/out/import.log \
  || { xwd -root -silent | convert xwd:- /out/linux-screenshot.png; } 2>>/out/import.log \
  || echo "screenshot failed" >> /out/import.log
kill "$APP" 2>/dev/null
INNER
chmod +x /tmp/inner.sh

echo "=== run under xvfb + dbus ==="
xvfb-run -a -s "-screen 0 1440x900x24" dbus-run-session -- /tmp/inner.sh

echo "=== app.log (broker boot) ==="; tail -20 /out/app.log
echo "=== proof ==="; cat /out/sockets.txt
echo "=== screenshot ==="; ls -lh /out/linux-screenshot.png 2>/dev/null || echo "NO SCREENSHOT"
