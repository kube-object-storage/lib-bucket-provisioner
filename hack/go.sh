#!/usr/bin/env bash

REPO_ROOT="$(readlink -f $(dirname ${BASH_SOURCE})/../)"
readonly LOCAL_IMPORT="sigs.k8s.io/controller-runtime,github.com/yard-turkey/lib-bucket-provisioner/"

readonly PKGS="./pkg/..."

# Because generated code exists under $REPO_ROOT/pkg/, it's necessary to filter it out
# This function get all sub packages under $REPO_ROOT/pkg/... except
valid_sub_packages() {
  # Exclude packages which should not be edited (pkg/apis/* && pkg/client/.*)
  local filteredPkgs="$(awk '!/pkg\/client/' <(go list -f '{{.Dir}}' ./...))"
  echo "$filteredPkgs"
}

readonly SUB_PACKAGES=$(valid_sub_packages)

imports(){
  echo "--------  formatting"
  (
    cd "${REPO_ROOT}"
    # Call goimport for each sub package
    for sp in ${SUB_PACKAGES}; do
      echo "goimports -w -local $LOCAL_IMPORT for packages under $sp"
      goimports -w -local "$LOCAL_IMPORT" "$sp"
    done
  )
}

imports-check(){
  echo "--------  checking format"
  (
    cd "${REPO_ROOT}"
    # Call goimport for each sub package
    for sp in ${SUB_PACKAGES}; do
      goimports -d -e -local "$LOCAL_IMPORT" "$sp"
    done
  )
}

vet(){
  echo "--------  vetting"
  (
    cd "${REPO_ROOT}"
    for sp in ${SUB_PACKAGES}; do
      go vet "${sp}"
    done
  )
}

build(){
  echo "--------  compiling"
  (
    cd "${REPO_ROOT}"
    for p in ${PKGS}; do
      echo "go build'ing package $p"
      go build -a "${p}"
    done
  )
}

test(){
  echo "-------- testing"
  (
    cd "${REPO_ROOT}"
    for p in "${PKGS}"; do
      go test "${p}"
    done
  )
}

preflight(){
    echo "-------- beginning preflight checks"
    imports-check
    vet
    test
    build
}

help(){
  msg=\
'
  This script accepts the following args:
  help          print this text
  vet           run go vet on core project code
  build         run go build on core project code
  test          run unit tests
  preflight     runs test, vet, build, and imports-check, then exits.
  imports-check run goimports with and reports errors and diffs
  imports       run goimports with defined import priorities on core project code
                  (goimports also runs gofmt)

  For example, to vet and gofmt/imports, run:
  $ ./go.sh vet imports
'
  printf "%s" "${msg}"
}

main(){
  while [[ ${#@} -gt 0 ]]; do
    case "$1" in
    "vet")
      vet
      shift 1
      ;;
    "build")
      build
      shift 1
      ;;
    "imports")
      imports
      shift 1
      ;;
     "imports-check")
      imports-check
      shift 1
      ;;
    "test")
      test
      shift 1
      ;;
    "preflight")
      preflight
      exit
      ;;
    "help"|'h')
      help
      exit
      ;;
    *)
      echo "unrecongnized args \"$1\""
      exit 1
      ;;
    esac
  done
}

main ${@}
