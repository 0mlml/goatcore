#!/usr/bin/env bash
# Hardware diagnostics for NETHOST power-cycling investigation
set -euo pipefail

section() { printf '\n\033[1;36m=== %s ===\033[0m\n' "$1"; }

section "System"
hostnamectl 2>/dev/null | grep -E 'Static hostname|Operating System|Kernel|Hardware Model' || uname -a
echo "Uptime: $(uptime -p)"

section "Reboot history (last 10)"
last reboot | head -10

section "Thermal"
if command -v sensors &>/dev/null; then
  sensors 2>/dev/null
else
  echo "lm-sensors not installed"
  for z in /sys/class/thermal/thermal_zone*/; do
    type=$(cat "${z}type" 2>/dev/null)
    temp=$(cat "${z}temp" 2>/dev/null)
    if [ -n "$temp" ]; then
      printf "  %s: %.1f°C\n" "$type" "$(echo "scale=1; $temp/1000" | bc)"
    fi
  done
fi

section "Previous boot errors (journalctl -b -1 -p err)"
if journalctl -b -1 -p err --no-pager -q 2>/dev/null | head -30; then
  :
else
  echo "No previous boot journal available (hard power cut or no persistent journaling)"
  echo "Persistent journal dir: $(ls -d /var/log/journal/ 2>/dev/null || echo 'MISSING — logs do not survive reboots')"
fi

section "OOM killer (last 3 boots)"
for b in 0 -1 -2; do
  hits=$(journalctl -b "$b" --no-pager -q 2>/dev/null | grep -ci 'oom\|out of memory\|killed process' || true)
  if [ "$hits" -gt 0 ]; then
    echo "Boot $b: $hits OOM events"
    journalctl -b "$b" --no-pager -q 2>/dev/null | grep -i 'oom\|out of memory\|killed process' | tail -5
  else
    echo "Boot $b: none"
  fi
done

section "Kernel panics (last 3 boots)"
for b in 0 -1 -2; do
  hits=$(journalctl -b "$b" --no-pager -q 2>/dev/null | grep -ci panic || true)
  if [ "$hits" -gt 0 ]; then
    echo "Boot $b: $hits panic events"
    journalctl -b "$b" --no-pager -q 2>/dev/null | grep -i panic | tail -5
  else
    echo "Boot $b: none"
  fi
done

section "Machine check exceptions (MCE)"
dmesg 2>/dev/null | grep -i mce || echo "None in current dmesg"
if [ -d /var/log/mcelog ]; then
  echo "mcelog entries:"
  ls -lt /var/log/mcelog/ 2>/dev/null | head -5
fi

section "Hardware errors in dmesg"
dmesg 2>/dev/null | grep -iE 'hardware error|critical|voltage|power|overcurrent|usb.*reset' | tail -10 || echo "None"

section "Memory"
free -h
echo ""
echo "Swap usage:"
swapon --show 2>/dev/null || echo "No swap"

section "Disk"
df -h / /var /var/log 2>/dev/null | sort -u
echo ""
echo "SMART health (if available):"
for disk in /dev/sda /dev/nvme0n1; do
  if [ -b "$disk" ]; then
    sudo smartctl -H "$disk" 2>/dev/null | grep -E 'SMART overall|result' || echo "$disk: smartctl not available or no SMART support"
  fi
done

section "Docker resource usage"
if command -v docker &>/dev/null; then
  docker stats --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}" 2>/dev/null || echo "Docker not running"
else
  echo "Docker not installed"
fi

section "Persistent journal check"
if [ -d /var/log/journal ] && [ "$(ls -A /var/log/journal 2>/dev/null)" ]; then
  echo "OK — persistent journaling enabled"
  journalctl --disk-usage 2>/dev/null
else
  echo "WARNING — no persistent journal. Enable with:"
  echo "  sudo mkdir -p /var/log/journal && sudo systemctl restart systemd-journald"
fi

section "Power supply (kernel)"
dmesg 2>/dev/null | grep -iE 'ACPI.*power|power.supply|battery|adapter' | tail -5 || echo "No power supply info in dmesg"

echo ""
echo "Done. If all sections are clean, suspect PSU or loose connector."
