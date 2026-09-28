#!/usr/bin/env bash
#
# System Health Check & Alert Script

# --- Threshold Configurations ---
CPU_LOAD_THRESHOLD_FACTOR=1.0  # Alert if 1-min load > (factor * CPU cores)
RAM_AVAIL_MIN_PCT=15           # Alert if available RAM drops below this %
SWAP_MAX_PCT=10                # Alert if swap usage exceeds this %
DISK_MAX_PCT=85                # Alert if any partition exceeds this %
TEMP_MAX_C=85                  # Alert if any CPU core temperature exceeds this °C

# Colors for terminal output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m' # No Color

ALERTS=0

echo -e "${BOLD}=========================================="
echo -e "       SYSTEM HEALTH CHECK REPORT         "
echo -e "==========================================${NC}"
echo "Hostname: $(hostname) | Date: $(date)"
echo ""

# --- 1. CPU LOAD CHECK ---
echo -e "${BOLD}[1] CPU Load Average${NC}"
CORES=$(nproc 2>/dev/null || grep -c '^processor' /proc/cpuinfo)
LOAD1=$(uptime | awk -F'load average:' '{ print $2 }' | cut -d, -f1 | tr -d ' ')
MAX_RECOMMENDED_LOAD=$(awk -v cores="$CORES" -v factor="$CPU_LOAD_THRESHOLD_FACTOR" 'BEGIN { print cores * factor }')

echo "  Physical CPU Cores: $CORES"
echo "  Current 1-min Load: $LOAD1"

IS_HIGH_LOAD=$(awk -v load="$LOAD1" -v max="$MAX_RECOMMENDED_LOAD" 'BEGIN { print (load > max) ? 1 : 0 }')
if [ "$IS_HIGH_LOAD" -eq 1 ]; then
  echo -e "  Status: ${RED}[ALERT] High CPU Load (Limit: $MAX_RECOMMENDED_LOAD)${NC}"
  ((ALERTS++))
else
  echo -e "  Status: ${GREEN}[OK] CPU Load within healthy bounds${NC}"
fi
echo ""

# --- 2. MEMORY CHECK ---
echo -e "${BOLD}[2] Memory & Swap Usage${NC}"
if command -v free >/dev/null 2>&1; then
  TOTAL_RAM_MB=$(free -m | awk '/^Mem:/ {print $2}')
  AVAIL_RAM_MB=$(free -m | awk '/^Mem:/ {print $7}')
  PCT_AVAIL=$(awk -v avail="$AVAIL_RAM_MB" -v total="$TOTAL_RAM_MB" 'BEGIN { printf "%.1f", (avail/total)*100 }')

  TOTAL_SWAP_MB=$(free -m | awk '/^Swap:/ {print $2}')
  USED_SWAP_MB=$(free -m | awk '/^Swap:/ {print $3}')
  
  if [ "$TOTAL_SWAP_MB" -gt 0 ]; then
    PCT_SWAP=$(awk -v used="$USED_SWAP_MB" -v total="$TOTAL_SWAP_MB" 'BEGIN { printf "%.1f", (used/total)*100 }')
  else
    PCT_SWAP=0
  fi

  echo "  Available RAM: ${AVAIL_RAM_MB}MB / ${TOTAL_RAM_MB}MB (${PCT_AVAIL}%)"
  echo "  Swap Usage:    ${USED_SWAP_MB}MB / ${TOTAL_SWAP_MB}MB (${PCT_SWAP}%)"

  # Check RAM
  IS_LOW_RAM=$(awk -v pct="$PCT_AVAIL" -v min="$RAM_AVAIL_MIN_PCT" 'BEGIN { print (pct < min) ? 1 : 0 }')
  if [ "$IS_LOW_RAM" -eq 1 ]; then
    echo -e "  RAM Status:  ${RED}[ALERT] Low Available RAM (< ${RAM_AVAIL_MIN_PCT}%)${NC}"
    ((ALERTS++))
  else
    echo -e "  RAM Status:  ${GREEN}[OK] RAM availability healthy${NC}"
  fi

  # Check Swap
  IS_HIGH_SWAP=$(awk -v pct="$PCT_SWAP" -v max="$SWAP_MAX_PCT" 'BEGIN { print (pct > max) ? 1 : 0 }')
  if [ "$IS_HIGH_SWAP" -eq 1 ]; then
    echo -e "  Swap Status: ${YELLOW}[WARNING] High Swap usage (> ${SWAP_MAX_PCT}%)${NC}"
    ((ALERTS++))
  else
    echo -e "  Swap Status: ${GREEN}[OK] Swap usage low${NC}"
  fi
else
  echo "  'free' command unavailable."
fi
echo ""

# --- 3. DISK SPACE CHECK ---
echo -e "${BOLD}[3] Filesystem Storage Usage${NC}"
DISK_ALERT=0
while read -r line; do
  USAGE=$(echo "$line" | awk '{print $5}' | sed 's/%//')
  MOUNT=$(echo "$line" | awk '{print $6}')
  FS=$(echo "$line" | awk '{print $1}')

  if [ "$USAGE" -ge "$DISK_MAX_PCT" ]; then
    echo -e "  ${RED}[ALERT] $MOUNT ($FS) is at ${USAGE}% capacity (Threshold: ${DISK_MAX_PCT}%)${NC}"
    DISK_ALERT=1
    ((ALERTS++))
  fi
done < <(df -h -P | grep -E '^/dev/')

if [ "$DISK_ALERT" -eq 0 ]; then
  echo -e "  Status: ${GREEN}[OK] All primary disk partitions under ${DISK_MAX_PCT}% usage${NC}"
fi
echo ""

# --- 4. TEMPERATURE CHECK ---
echo -e "${BOLD}[4] Hardware Temperatures${NC}"
if command -v sensors >/dev/null 2>&1; then
  MAX_TEMP=$(sensors | grep -E 'Core|Package|temp1' | awk '{print $2}' | tr -d '+°C' | sort -nr | head -n1)
  if [ -n "$MAX_TEMP" ]; then
    echo "  Max Detected Temp: ${MAX_TEMP}°C"
    IS_HOT=$(awk -v temp="$MAX_TEMP" -v max="$TEMP_MAX_C" 'BEGIN { print (temp >= max) ? 1 : 0 }')
    if [ "$IS_HOT" -eq 1 ]; then
      echo -e "  Status: ${RED}[ALERT] CPU temperature high (>= ${TEMP_MAX_C}°C)${NC}"
      ((ALERTS++))
    else
      echo -e "  Status: ${GREEN}[OK] Temperatures within safe range${NC}"
    fi
  else
    echo "  Could not parse temperature sensor data."
  fi
else
  echo -e "  ${YELLOW}[SKIP] 'lm-sensors' not installed. Run 'sudo apt install lm-sensors' to enable.${NC}"
fi
echo ""

# --- SUMMARY ---
echo -e "${BOLD}=========================================="
if [ "$ALERTS" -eq 0 ]; then
  echo -e "   FINAL VERDICT: ${GREEN}SYSTEM IS HEALTHY${NC}"
else
  echo -e "   FINAL VERDICT: ${RED}${ALERTS} HEALTH ALERT(S) DETECTED${NC}"
fi
echo -e "==========================================${NC}"

