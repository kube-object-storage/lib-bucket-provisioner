#!/usr/bin/env bash

REPO_ROOT="$(readlink -f $(dirname ${BASH_SOURCE})/../)"
PKGS="pkg/"

(
  cd $REPO_ROOT
  for p in $PKGS; do
    goimports -s -w -local "$p/..."
  done
)
