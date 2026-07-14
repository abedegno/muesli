#!/bin/sh
set -eu

repo_owner='abedegno'
repo_name='muesli'
release_tag=${MUESLI_RELEASE_TAG:-}
install_dir='./muesli'
run_up=0
force=0
dir_set=0
tmpdir=

usage() {
  cat <<'EOF'
Usage: install.sh [OPTIONS] [DIR]

Install the production Muesli stack into DIR (default: ./muesli) by
downloading the GitHub Release asset bundle for a version tag: a
version-pinned docker-compose.prod.yml (image tags baked in, so it does not
depend on MUESLI_IMAGE_TAG in .env), .env.example, install.sh, and a
SHA256SUMS file. Every downloaded file's checksum is verified against
SHA256SUMS before anything is moved into DIR.

Options:
  -d, --dir DIR   Target install directory.
  --force         Regenerate .env even if one already exists.
  --up            Run "docker compose -f docker-compose.prod.yml up -d" after install.
  -h, --help      Show this help text and exit.

Environment:
  MUESLI_RELEASE_TAG   Release tag to install, e.g. v1.2.3 (default: the
                        latest release, resolved via the GitHub API).
  MUESLI_INSTALL_READY_TIMEOUT   Seconds to wait for /readyz after --up (default: 240).
  MUESLI_INSTALL_READY_INTERVAL   Seconds between /readyz polls after --up (default: 5).

Examples:
  curl -fsSL https://github.com/abedegno/muesli/releases/latest/download/install.sh | sh
  MUESLI_RELEASE_TAG=v1.2.3 sh scripts/install.sh --dir /opt/muesli --up
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

# Resolve the latest release tag via the GitHub API (no jq dependency; parsed
# with sed so the script stays POSIX sh with no extra required commands).
resolve_latest_release_tag() {
  api_url="https://api.github.com/repos/$repo_owner/$repo_name/releases/latest"
  api_body=$(curl -fsSL "$api_url") || die "Failed to query $api_url to resolve the latest release. Set MUESLI_RELEASE_TAG to pin a specific release instead."
  latest_tag=$(printf '%s\n' "$api_body" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
  [ -n "$latest_tag" ] || die "Could not parse a release tag from $api_url. Set MUESLI_RELEASE_TAG to pin a specific release instead."
  printf '%s\n' "$latest_tag"
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

for cmd in docker curl openssl awk sed mktemp mv mkdir rm; do
  command -v "$cmd" >/dev/null 2>&1 || die "Missing required command: $cmd"
done

if command -v sha256sum >/dev/null 2>&1; then
  checksum_cmd='sha256sum -c'
elif command -v shasum >/dev/null 2>&1; then
  checksum_cmd='shasum -a 256 -c'
else
  die "Missing required command: sha256sum (or shasum) to verify release checksums."
fi

if ! docker info >/dev/null 2>&1; then
  die "Docker is not ready. Start the Docker daemon and make sure your user can talk to it, then try again."
fi

if ! docker compose version >/dev/null 2>&1; then
  die "Compose v2 is required. Install the Docker Compose plugin so 'docker compose version' works."
fi

if [ -z "$release_tag" ]; then
  printf '%s\n' "Resolving the latest release of $repo_owner/$repo_name..."
  release_tag=$(resolve_latest_release_tag)
fi

tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/muesli-install.XXXXXX")

mkdir -p "$install_dir"

# Fetch the self-contained release asset bundle: a docker-compose.prod.yml
# with image tags pinned literally to this release, .env.example, install.sh,
# and a SHA256SUMS covering all three.
release_base="https://github.com/$repo_owner/$repo_name/releases/download/$release_tag"

printf '%s\n' "Fetching release $release_tag from $repo_owner/$repo_name..."
curl -fsSL "$release_base/docker-compose.prod.yml" -o "$tmpdir/docker-compose.prod.yml" || die "Failed to download docker-compose.prod.yml for release $release_tag. Check that the release and its assets exist."
curl -fsSL "$release_base/.env.example" -o "$tmpdir/.env.example" || die "Failed to download .env.example for release $release_tag."
curl -fsSL "$release_base/install.sh" -o "$tmpdir/install.sh" || die "Failed to download install.sh for release $release_tag."
curl -fsSL "$release_base/SHA256SUMS" -o "$tmpdir/SHA256SUMS" || die "Failed to download SHA256SUMS for release $release_tag; refusing to install without checksum verification."

for asset in docker-compose.prod.yml .env.example install.sh; do
  awk -v f="$asset" '$2 == f || $2 == "*" f { found = 1 } END { exit !found }' "$tmpdir/SHA256SUMS" \
    || die "SHA256SUMS for release $release_tag has no entry for $asset; refusing to install. Nothing has been installed."
done

checksum_output=$(cd "$tmpdir" && $checksum_cmd SHA256SUMS 2>&1) || {
  printf '%s\n' "$checksum_output" >&2
  die "Checksum verification failed for release $release_tag assets; aborting install. Nothing has been installed."
}

mv "$tmpdir/docker-compose.prod.yml" "$install_dir/docker-compose.prod.yml"
mv "$tmpdir/.env.example" "$install_dir/.env.example"
mv "$tmpdir/install.sh" "$install_dir/install.sh"
chmod +x "$install_dir/install.sh" 2>/dev/null || true

if [ -e "$install_dir/.env" ] && [ "$force" -ne 1 ]; then
  printf '%s\n' "Existing .env found at $install_dir/.env; leaving it unchanged. Use --force to regenerate it."
else
  master_key=$(openssl rand -base64 32)
  storage_key=$(openssl rand -base64 32)
  env_tmp="$tmpdir/.env"

  awk -v mk="$master_key" -v sk="$storage_key" '
    BEGIN {
      found_mk = 0
      found_sk = 0
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

printf '%s\n' "Install complete (release $release_tag)."
printf '%s\n' "Next steps:"
printf '%s\n' "  cd $install_dir"
printf '%s\n' "  review .env before using the stack"
printf '%s\n' "  docker compose -f docker-compose.prod.yml up -d"
