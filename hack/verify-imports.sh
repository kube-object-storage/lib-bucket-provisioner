#!/usr/bin/env bash

REPO_ROOT="$(readlink -f $(dirname ${BASH_SOURCE})/../)"
readonly LOCAL_IMPORT="sigs.k8s.io/controller-runtime,github.com/yard-turkey/lib-bucket-provisioner/"
readonly PKGS="./pkg"

valid_sub_packages() {
  # Exclude packages which should not be edited (pkg/apis/* && pkg/client/.*)
  local filteredPkgs="$(awk '!/pkg\/apis|pkg\/client/' <(go list -f '{{.Dir}}' ./...))"
  echo "$filteredPkgs"
}

verify_imports(){
  (
    cd "${REPO_ROOT}"

    # For each root PKG
    for p in ${PKGS}; do
      sub_pkgs=$(valid_sub_packages)

      # Call goimport for each sub package
      for sp in ${sub_pkgs}; do
        echo "Formatting $sp"
        goimports -w -local "$LOCAL_IMPORT" "$sp"
      done

    done
  )
}

verify_imports

