#!/bin/sh
set -eu

repo_owner='abedegno'
repo_name='muesli'
install_ref=${MUESLI_INSTALL_REF:-main}
image_tag=${MUESLI_IMAGE_TAG:-latest}
install_dir='./muesli'
run_up=0
force=0
dir_set=0
tmpdir=

usage() {
  cat <<'EOF'
Usage: install.sh [OPTIONS] [DIR]

Install the production Muesli stack into DIR (default: ./muesli) by fetching
docker-compose.prod.yml and .env.example from an explicit git ref.

Options:
  -d, --dir DIR   Target install directory.
  --force         Regenerate .env even if one already exists.
  --up            Run "docker compose -f docker-compose.prod.yml up -d" after install.
  -h, --help      Show this help text and exit.

Environment:
  MUESLI_INSTALL_REF   Git ref to fetch from (default: main).
  MUESLI_IMAGE_TAG     Image tag to write into .env (default: latest).
  MUESLI_INSTALL_READY_TIMEOUT   Seconds to wait for /readyz after --up (default: 240).
  MUESLI_INSTALL_READY_INTERVAL   Seconds between /readyz polls after --up (default: 5).

Examples:
  curl -fsSL https://raw.githubusercontent.com/abedegno/muesli/main/scripts/install.sh | sh
  MUESLI_INSTALL_REF=main sh scripts/install.sh --dir /opt/muesli --up
EOF
}

die() {
  printf '%s\n' "$*" >&2
  exit 1
}

cleanup() {
  if [ -n "${tmpdir:-}" ] && [ -d "$tmpdir" ]; then
    rm -rf "$tmpdir"
  fi
}

wait_for_readyz() {
  readyz_url=${1:?}
  ready_timeout=${2:?}
  ready_interval=${3:?}
  ready_start=$(date +%s)
  ready_deadline=$((ready_start + ready_timeout))

  printf '%s\n' "Waiting for $readyz_url to return HTTP 200..."
  while :; do
    ready_status=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 3 "$readyz_url" 2>/dev/null || true)
    if [ "$ready_status" = "200" ]; then
      return 0
    fi

    ready_now=$(date +%s)
    if [ "$ready_now" -ge "$ready_deadline" ]; then
      return 1
    fi

    sleep "$ready_interval"
  done
}

trap cleanup 0 1 2 15

while [ $# -gt 0 ]; do
  arg=$1
  shift
  case "$arg" in
    -h|--help)
      usage
      exit 0
      ;;
    --up)
      run_up=1
      ;;
    --force)
      force=1
      ;;
    -d|--dir)
      [ $# -gt 0 ] || die "--dir requires a value"
      install_dir=$1
      dir_set=1
      shift
      ;;
    --dir=*)
      install_dir=${arg#*=}
      dir_set=1
      ;;
    --)
      if [ $# -gt 0 ]; then
        if [ "$dir_set" -eq 0 ]; then
          install_dir=$1
          dir_set=1
          shift
        else
          die "Unexpected extra arguments: $*"
        fi
      fi
      [ $# -eq 0 ] || die "Unexpected extra arguments: $*"
      break
      ;;
    -*)
      die "Unknown option: $arg"
      ;;
    *)
      if [ "$dir_set" -eq 0 ]; then
        install_dir=$arg
        dir_set=1
      else
        die "Unexpected extra argument: $arg"
      fi
      ;;
  esac
done

for cmd in docker curl openssl awk mktemp mv mkdir rm; do
  command -v "$cmd" >/dev/null 2>&1 || die "Missing required command: $cmd"
done

if ! docker info >/dev/null 2>&1; then
  die "Docker is not ready. Start the Docker daemon and make sure your user can talk to it, then try again."
fi

if ! docker compose version >/dev/null 2>&1; then
  die "Compose v2 is required. Install the Docker Compose plugin so 'docker compose version' works."
fi

tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/muesli-install.XXXXXX")

mkdir -p "$install_dir"

# Fetch individual files from raw.githubusercontent.com at an explicit git ref
# so the install is pinned without cloning the whole repository.
compose_url="https://raw.githubusercontent.com/$repo_owner/$repo_name/$install_ref/docker-compose.prod.yml"
env_example_url="https://raw.githubusercontent.com/$repo_owner/$repo_name/$install_ref/.env.example"

printf '%s\n' "Fetching production files from ref: $install_ref"
curl -fsSL "$compose_url" -o "$tmpdir/docker-compose.prod.yml"
curl -fsSL "$env_example_url" -o "$tmpdir/.env.example"

mv "$tmpdir/docker-compose.prod.yml" "$install_dir/docker-compose.prod.yml"
mv "$tmpdir/.env.example" "$install_dir/.env.example"

if [ -e "$install_dir/.env" ] && [ "$force" -ne 1 ]; then
  printf '%s\n' "Existing .env found at $install_dir/.env; leaving it unchanged. Use --force to regenerate it."
else
  master_key=$(openssl rand -base64 32)
  storage_key=$(openssl rand -base64 32)
  env_tmp="$tmpdir/.env"

  awk -v mk="$master_key" -v sk="$storage_key" -v it="$image_tag" '
    BEGIN {
      found_mk = 0
      found_sk = 0
      found_it = 0
    }
    /^MUESLI_MASTER_KEY=/ {
      print "MUESLI_MASTER_KEY=" mk
      found_mk = 1
      next
    }
    /^MUESLI_STORAGE_SIGNING_KEY=/ {
      print "MUESLI_STORAGE_SIGNING_KEY=" sk
      found_sk = 1
      next
    }
    /^MUESLI_IMAGE_TAG=/ {
      print "MUESLI_IMAGE_TAG=" it
      found_it = 1
      next
    }
    {
      print
    }
    END {
      if (!found_mk) {
        print "MUESLI_MASTER_KEY=" mk
      }
      if (!found_sk) {
        print "MUESLI_STORAGE_SIGNING_KEY=" sk
      }
      if (!found_it) {
        print "MUESLI_IMAGE_TAG=" it
      }
    }
  ' "$install_dir/.env.example" > "$env_tmp"

  mv "$env_tmp" "$install_dir/.env"
  printf '%s\n' "Generated $install_dir/.env with fresh secrets."
fi

if [ "$run_up" -eq 1 ]; then
  (
    cd "$install_dir"
    docker compose -f docker-compose.prod.yml up -d
  )
  ready_timeout=${MUESLI_INSTALL_READY_TIMEOUT:-240}
  ready_interval=${MUESLI_INSTALL_READY_INTERVAL:-5}

  if ! wait_for_readyz 'http://localhost:8080/readyz' "$ready_timeout" "$ready_interval"; then
    die "Timed out waiting for http://localhost:8080/readyz after ${ready_timeout}s. Check 'docker compose -f docker-compose.prod.yml ps' and 'docker compose -f docker-compose.prod.yml logs' in $install_dir."
  fi

  printf '%s\n' "Started the production stack with docker compose."
fi

printf '%s\n' "Install complete."
printf '%s\n' "Next steps:"
printf '%s\n' "  cd $install_dir"
printf '%s\n' "  review .env before using the stack"
printf '%s\n' "  docker compose -f docker-compose.prod.yml up -d"
