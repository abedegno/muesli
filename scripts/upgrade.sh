#!/bin/sh
set -eu

install_dir='.'
dir_set=0
yes=0

usage() {
  cat <<'EOF'
Usage: upgrade.sh [OPTIONS] [DIR]

Upgrade an existing hosted Muesli Compose install in DIR (default: .).
This expects a production install created by scripts/install.sh with
docker-compose.prod.yml and .env already present.

Options:
  -d, --dir DIR   Target install directory.
  -y, --yes       Skip the confirmation prompt.
  -h, --help      Show this help text and exit.

Examples:
  sh scripts/upgrade.sh --dir /opt/muesli
  sh scripts/upgrade.sh -y
EOF
}

die() {
  printf '%s\n' "$*" >&2
  exit 1
}

while [ $# -gt 0 ]; do
  arg=$1
  shift
  case "$arg" in
    -h|--help)
      usage
      exit 0
      ;;
    -y|--yes)
      yes=1
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

[ -d "$install_dir" ] || die "Install directory not found: $install_dir. Run scripts/install.sh first."
[ -f "$install_dir/docker-compose.prod.yml" ] || die "Missing $install_dir/docker-compose.prod.yml. Run scripts/install.sh first."
[ -f "$install_dir/.env" ] || die "Missing $install_dir/.env. Run scripts/install.sh first."

current_image_tag=''
image_tag_found=0
line=''
while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    MUESLI_IMAGE_TAG=*)
      current_image_tag=${line#MUESLI_IMAGE_TAG=}
      image_tag_found=1
      break
      ;;
  esac
done < "$install_dir/.env"

printf '%s\n' "Backup reminder: review docs/BACKUP.md before upgrading."
printf '%s\n' "Take a pre-upgrade Postgres dump from the install directory with:"
printf '%s\n' "  docker compose -f docker-compose.prod.yml exec -T postgres pg_dump -U postgres muesli | gzip > muesli-\$(date +%Y%m%d%H%M%S).sql.gz"

if [ "$image_tag_found" -eq 1 ]; then
  if [ -n "$current_image_tag" ]; then
    printf '%s\n' "Current MUESLI_IMAGE_TAG in $install_dir/.env: $current_image_tag"
  else
    printf '%s\n' "Current MUESLI_IMAGE_TAG in $install_dir/.env: set but empty"
  fi
else
  printf '%s\n' "Current MUESLI_IMAGE_TAG in $install_dir/.env: not set"
fi

if [ "$yes" -eq 0 ]; then
  printf '%s' "Continue with the upgrade? [y/N] "
  if ! IFS= read -r answer; then
    die "Confirmation required. Re-run with -y/--yes or provide y on stdin."
  fi
  case "$answer" in
    y|Y)
      ;;
    *)
      printf '%s\n' "Aborted."
      exit 1
      ;;
  esac
fi

for cmd in docker; do
  command -v "$cmd" >/dev/null 2>&1 || die "Missing required command: $cmd"
done

if ! docker compose version >/dev/null 2>&1; then
  die "Compose v2 is required. Install the Docker Compose plugin so 'docker compose version' works."
fi

printf '%s\n' "Pulling pinned images from GHCR..."
(
  cd "$install_dir"
  docker compose --env-file .env -f docker-compose.prod.yml pull

  printf '%s\n' "Recreating the stack with the pulled images..."
  docker compose --env-file .env -f docker-compose.prod.yml up -d
)

printf '%s\n' "Upgrade complete."
printf '%s\n' "The server applies DB migrations on boot; this script does not run any manual migration commands."
printf '%s\n' "Rollback guidance:"
if [ "$image_tag_found" -eq 1 ] && [ -n "$current_image_tag" ]; then
  printf '%s\n' "  1. Set MUESLI_IMAGE_TAG=$current_image_tag in $install_dir/.env."
else
  printf '%s\n' "  1. Restore the previous MUESLI_IMAGE_TAG value in $install_dir/.env."
fi
printf '%s\n' "  2. Run from $install_dir:"
printf '%s\n' "     docker compose -f docker-compose.prod.yml pull && docker compose -f docker-compose.prod.yml up -d"
printf '%s\n' "  3. If the new version's migration is incompatible with the old binary, restore the pre-upgrade DB backup using the restore procedure in docs/BACKUP.md before starting the old version again."
