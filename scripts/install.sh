#!/bin/sh
set -eu

repo_owner='abedegno'
repo_name='muesli'
release_tag=${MUESLI_RELEASE_TAG:-latest}
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
docker-compose.prod.yml and .env.example from a GitHub release.

Options:
  -d, --dir DIR   Target install directory.
  -r, --release TAG
                   GitHub release tag to install (default: latest release).
  --force         Regenerate .env even if one already exists.
  --up            Run "docker compose -f docker-compose.prod.yml up -d" after install.
  -h, --help      Show this help text and exit.

Environment:
  MUESLI_RELEASE_TAG   GitHub release tag to fetch (default: latest release).
  MUESLI_IMAGE_TAG     Image tag to write into .env (default: latest).

Examples:
  curl -fsSL https://raw.githubusercontent.com/abedegno/muesli/main/scripts/install.sh | sh
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
    -r|--release)
      [ $# -gt 0 ] || die "--release requires a value"
      release_tag=$1
      shift
      ;;
    --release=*)
      release_tag=${arg#*=}
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

for cmd in docker curl openssl awk mktemp mv mkdir rm sha256sum python3; do
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

release_api_url="https://api.github.com/repos/$repo_owner/$repo_name/releases"
if [ "$release_tag" = "latest" ]; then
  release_api_url="$release_api_url/latest"
else
  release_api_url="$release_api_url/tags/$release_tag"
fi

get_asset_url() {
  asset_name=$1
  python3 - "$tmpdir/release.json" "$asset_name" <<'PY'
import json
import sys

release_path = sys.argv[1]
asset_name = sys.argv[2]

with open(release_path, encoding='utf-8') as fh:
    release = json.load(fh)

for asset in release.get('assets', []):
    if asset.get('name') == asset_name:
        print(asset['browser_download_url'])
        raise SystemExit(0)

raise SystemExit(f"Missing release asset: {asset_name}")
PY
}

printf '%s\n' "Fetching release assets for: $release_tag"
curl -fsSL -H 'Accept: application/vnd.github+json' -H 'X-GitHub-Api-Version: 2022-11-28' "$release_api_url" -o "$tmpdir/release.json"

compose_url=$(get_asset_url 'docker-compose.prod.yml')
env_example_url=$(get_asset_url '.env.example')
checksums_url=$(get_asset_url 'SHA256SUMS')

curl -fsSL "$compose_url" -o "$tmpdir/docker-compose.prod.yml"
curl -fsSL "$env_example_url" -o "$tmpdir/.env.example"
curl -fsSL "$checksums_url" -o "$tmpdir/SHA256SUMS"

(cd "$tmpdir" && awk '$2 == "docker-compose.prod.yml" || $2 == ".env.example" { print }' SHA256SUMS | sha256sum -c -) || die "Checksum verification failed for release assets"

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
  printf '%s\n' "Started the production stack with docker compose."
fi

printf '%s\n' "Install complete."
printf '%s\n' "Next steps:"
printf '%s\n' "  cd $install_dir"
printf '%s\n' "  review .env before using the stack"
printf '%s\n' "  docker compose -f docker-compose.prod.yml up -d"
