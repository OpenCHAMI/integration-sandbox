#!/usr/bin/env bash
set -euo pipefail

: "${SBX_DECLARATIVE_RUN_DIR:?required}"
: "${SBX_DECLARATIVE_QUIESCENCE_LOCK:?required}"
exec > >(tee -a "$SBX_DECLARATIVE_RUN_DIR/quiescence-$(date -u +%Y%m%dT%H%M%SZ).log") 2>&1
lock="$SBX_DECLARATIVE_QUIESCENCE_LOCK"
markers="$lock.inflight"
[[ "$lock" == "$SBX_DECLARATIVE_RUN_DIR/quiescence.lock" ]] || { printf 'invalid quiescence lock path\n' >&2; exit 1; }
[[ ! -L "$lock" && ! -L "$markers" ]] || { printf 'quiescence paths must not be symlinks\n' >&2; exit 1; }
if [[ -n "${SBX_DECLARATIVE_QUIESCENCE_INODE:-}" && ! -f "$lock" ]]; then
  printf 'BLOCKED: initialized quiescence lock missing\n' >&2
  exit 1
fi
case "${1:-}" in
  init)
    if [[ ! -e "$lock" ]]; then (umask 077; set -o noclobber; : > "$lock"); fi
    if [[ ! -e "$markers" ]]; then (umask 077; mkdir -m 700 "$markers"); fi
    [[ -f "$lock" && -d "$markers" ]] || { printf 'invalid quiescence paths\n' >&2; exit 1; }
    chmod 600 "$lock"
    chmod 700 "$markers"
    ;;
  check)
    [[ -f "$lock" && -d "$markers" ]] || { printf 'quiescence paths missing\n' >&2; exit 1; }
    ;;
  *) printf 'usage: %s init|check\n' "$0" >&2; exit 2 ;;
esac
if [[ -n "${SBX_DECLARATIVE_QUIESCENCE_INODE:-}" && "$(ls -di "$lock")" != "$SBX_DECLARATIVE_QUIESCENCE_INODE" ]]; then
  printf 'BLOCKED: quiescence lock inode changed\n' >&2
  exit 1
fi
shopt -s nullglob dotglob
entries=("$markers"/*)
if (( ${#entries[@]} != 0 )); then
  printf 'BLOCKED: outstanding quiescence marker; discard the owned run\n' >&2
  exit 1
fi
printf 'quiescence %s: no outstanding markers\n' "$1"
