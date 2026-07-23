#!/bin/sh
set -eu

work_dir=/workspace/code/project
audit_path=/tmp/pk-events.jsonl
pid_path=/tmp/pk-target.pid

mkdir -p "$work_dir"
/fixture/process-reaper reap /fixture/vite "$work_dir" "$pid_path" &
reaper_pid=$!
monitor_pid=

cleanup() {
  if [ -f "$pid_path" ]; then
    target_pid=$(cat "$pid_path")
    kill "$target_pid" 2>/dev/null || true
  fi
  if [ -n "$monitor_pid" ]; then
    kill "$monitor_pid" 2>/dev/null || true
  fi
  kill "$reaper_pid" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

attempt=0
while [ ! -f "$pid_path" ]; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 50 ]; then
    echo "target pid was not created" >&2
    exit 1
  fi
  sleep 0.1
done
target_pid=$(cat "$pid_path")

attempt=0
until /fixture/pk scan --cpu 100000 --mem 1000000 > /tmp/pk-scan.txt && grep -F "$target_pid" /tmp/pk-scan.txt >/dev/null; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 50 ]; then
    echo "target process was not discovered" >&2
    cat /tmp/pk-scan.txt >&2
    exit 1
  fi
  sleep 0.1
done
grep -F "kill" /tmp/pk-scan.txt >/dev/null
grep -F "high" /tmp/pk-scan.txt >/dev/null
grep -F "vite" /tmp/pk-scan.txt >/dev/null

/fixture/pk monitor --cpu 100000 --mem 0 --grace 0 --interval 50ms \
  --protected process-reaper,sh,sleep,grep,cat > /tmp/pk-monitor.txt 2>&1 &
monitor_pid=$!
sleep 0.4
kill "$monitor_pid"
wait "$monitor_pid" || true
monitor_pid=
grep -F "Preview - skipping kill" /tmp/pk-monitor.txt >/dev/null
kill -0 "$target_pid"

PK_AUDIT_PATH="$audit_path" /fixture/pk cleanup \
  --scope processes --cpu 100000 --mem 1000000 > /tmp/pk-preview.txt
grep -F "$target_pid" /tmp/pk-preview.txt >/dev/null
grep -F "false" /tmp/pk-preview.txt >/dev/null
kill -0 "$target_pid"

PK_AUDIT_PATH="$audit_path" /fixture/pk cleanup --apply \
  --scope processes --cpu 100000 --mem 1000000 > /tmp/pk-apply.txt
grep -F "$target_pid" /tmp/pk-apply.txt >/dev/null
grep -F "true" /tmp/pk-apply.txt >/dev/null
if kill -0 "$target_pid" 2>/dev/null; then
  echo "target process survived applied cleanup" >&2
  exit 1
fi

wait "$reaper_pid"
PK_AUDIT_PATH="$audit_path" /fixture/pk history > /tmp/pk-history.txt
grep -F '"applied":false' /tmp/pk-history.txt >/dev/null
grep -F '"applied":true' /tmp/pk-history.txt >/dev/null

trap - EXIT INT TERM
echo "isolated process cleanup e2e passed"
