#!/usr/bin/env bash
# diagnose-gpu.sh — read-only GPU/CUDA diagnostic for Muesli deployments.
# Checks nvidia-smi, the NVIDIA Container Toolkit, Docker GPU runtime config,
# kernel modules, device nodes, and a container GPU smoke test.
# Prints [PASS]/[FAIL] + FIX hints; exits 0 always.
# Output is tee'd to /tmp/muesli-gpu-diag-<timestamp>.log
set -euo pipefail

# ── logging setup ────────────────────────────────────────────────────────────
LOG_FILE="/tmp/muesli-gpu-diag-$(date +%Y%m%dT%H%M%S).log"
# Redirect all output through tee so both console and file get it.
exec > >(tee -a "$LOG_FILE") 2>&1

echo "=== Muesli GPU/CUDA diagnostic — $(date) ==="
echo ""

# ── counters ─────────────────────────────────────────────────────────────────
PASS=0
FAIL=0

pass() {
  PASS=$(( PASS + 1 ))
  echo "[PASS] $*"
}

fail() {
  FAIL=$(( FAIL + 1 ))
  echo "[FAIL] $1"
  echo "  FIX: $2"
}

# ── Check 1: nvidia-smi present and functional ───────────────────────────────
check_nvidia_smi() {
  if ! command -v nvidia-smi >/dev/null 2>&1; then
    fail "nvidia-smi not found in PATH" \
      "Install the NVIDIA driver (Ubuntu: apt install nvidia-driver-<ver>) — https://docs.nvidia.com/datacenter/tesla/driver-installation-guide/"
    return
  fi
  if nvidia-smi >/dev/null 2>&1; then
    pass "nvidia-smi present and returned exit 0"
  else
    fail "nvidia-smi found but returned a non-zero exit code" \
      "Check driver health: run 'nvidia-smi' manually and inspect the error, then reinstall the driver if needed"
  fi
}

# ── Check 2: NVIDIA Container Toolkit (nvidia-ctk) ──────────────────────────
check_container_toolkit() {
  if command -v nvidia-ctk >/dev/null 2>&1; then
    pass "NVIDIA Container Toolkit (nvidia-ctk) is present"
  else
    fail "nvidia-ctk not found in PATH" \
      "Install the NVIDIA Container Toolkit: https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html"
  fi
}

# ── Check 3: Docker GPU runtime configured ───────────────────────────────────
check_docker_runtime() {
  local found=0
  if docker info 2>/dev/null | grep -i nvidia >/dev/null 2>&1; then
    found=1
  fi
  if [ "$found" -eq 0 ] && [ -f /etc/docker/daemon.json ]; then
    if grep -q nvidia /etc/docker/daemon.json 2>/dev/null; then
      found=1
    fi
  fi
  if [ "$found" -eq 1 ]; then
    pass "Docker GPU runtime (nvidia) is configured"
  else
    fail "Docker does not appear to have the nvidia runtime configured" \
      "Run: nvidia-ctk runtime configure --runtime=docker && systemctl restart docker  (see https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html#docker)"
  fi
}

# ── Check 4: Kernel modules loaded ───────────────────────────────────────────
check_kernel_modules() {
  if ! command -v lsmod >/dev/null 2>&1; then
    fail "lsmod not available — cannot check kernel modules" \
      "Install kmod (Ubuntu: apt install kmod) or check /proc/modules manually for nvidia entries"
    return
  fi
  if lsmod 2>/dev/null | grep -q nvidia; then
    pass "NVIDIA kernel modules are loaded (lsmod shows nvidia)"
  else
    fail "No nvidia kernel modules found via lsmod" \
      "Load the driver: 'modprobe nvidia' (requires root) or reinstall the NVIDIA driver and reboot"
  fi
}

# ── Check 5: Device nodes present ────────────────────────────────────────────
check_device_nodes() {
  local count
  count=$(find /dev -maxdepth 1 -name 'nvidia*' 2>/dev/null | wc -l)
  if [ "$count" -gt 0 ]; then
    pass "NVIDIA device nodes present under /dev (found $count matching /dev/nvidia*)"
  else
    fail "No /dev/nvidia* device nodes found" \
      "Ensure the NVIDIA driver is installed and the kernel modules are loaded, then reboot if needed"
  fi
}

# ── Check 6: Container GPU smoke test ────────────────────────────────────────
check_container_smoke() {
  if ! command -v docker >/dev/null 2>&1; then
    fail "docker not found — skipping container GPU smoke test" \
      "Install Docker Engine: https://docs.docker.com/engine/install/"
    return
  fi
  local rc=0
  timeout 30 docker run --rm --gpus all \
    --entrypoint nvidia-smi \
    nvidia/cuda:12.3.1-base-ubuntu22.04 \
    -L >/dev/null 2>&1 || rc=$?
  if [ "$rc" -eq 0 ]; then
    pass "Container GPU smoke test passed (nvidia-smi -L listed GPU(s) inside nvidia/cuda:12.3.1-base-ubuntu22.04)"
  else
    fail "Container GPU smoke test failed (exit $rc)" \
      "Ensure the nvidia container runtime is configured and GPU is accessible; check: docker run --rm --gpus all nvidia/cuda:12.3.1-base-ubuntu22.04 nvidia-smi -L"
  fi
}

# ── Run all checks ────────────────────────────────────────────────────────────
check_nvidia_smi
check_container_toolkit
check_docker_runtime
check_kernel_modules
check_device_nodes
check_container_smoke

# ── Summary ───────────────────────────────────────────────────────────────────
TOTAL=$(( PASS + FAIL ))
echo ""
echo "=== Summary: ${PASS}/${TOTAL} checks passed ==="
echo ""
echo "Log written to: ${LOG_FILE}"

exit 0
