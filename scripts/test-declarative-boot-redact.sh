#!/usr/bin/env bash
set -euo pipefail

: "${SBX_DECLARATIVE_RUN_DIR:?required}"
: "${SBX_DECLARATIVE_HSM_BOOTSTRAP_REDACTIONS:?required}"
if [[ -e "$SBX_DECLARATIVE_RUN_DIR/identity-redaction-incomplete" || -L "$SBX_DECLARATIVE_RUN_DIR/identity-redaction-incomplete" ]]; then
  printf 'credential-safe log capture unavailable\n' >&2
  exit 1
fi
exec 2> >(tee -a "$SBX_DECLARATIVE_RUN_DIR/redaction-errors-$(date -u +%Y%m%dT%H%M%SZ).log" >&2)
perl -ne '
  use strict; use warnings;
  our $inside_key;
  my $marker = "$ENV{SBX_DECLARATIVE_RUN_DIR}/identity-redaction-incomplete";
  (-e $marker || -l $marker) and die "credential-safe log capture unavailable\n";
  if ($inside_key) { $inside_key = 0 if /-----END [^-]+-----/; next; }
  s/-----BEGIN [^-]+-----.*?-----END [^-]+-----/[REDACTED-KEY]/gi;
  if (/-----BEGIN [^-]+-----/) { s/-----BEGIN [^-]+-----.*/[REDACTED-KEY]/; $inside_key = 1; }
  s/\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b/[REDACTED-JWT]/g;
  s/("(?:[^" ]*(?:token|password|secret|authorization)[^" ]*)"\s*:\s*")[^"]*/${1}[REDACTED]/gi;
  open(my $ledger, "<", $ENV{SBX_DECLARATIVE_HSM_BOOTSTRAP_REDACTIONS}) or die "cannot read credential redaction ledger\n";
  while (my $token = <$ledger>) { chomp $token; length($token) or die "empty credential in redaction ledger\n"; s/\Q$token\E/[REDACTED]/g; }
  close($ledger) or die "cannot close credential redaction ledger\n";
  (-e $marker || -l $marker) and die "credential-safe log capture unavailable\n";
  print or die "cannot write redacted output\n";
' | tee -a "$SBX_DECLARATIVE_RUN_DIR/redacted-$(date -u +%Y%m%dT%H%M%SZ).log"
