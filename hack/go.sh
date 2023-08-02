#!/usr/bin/env bash

REPO_ROOT="$(readlink -f $(dirname ${BASH_SOURCE})/../)"
readonly LOCAL_IMPORT="sigs.k8s.io/controller-runtime,github.com/kube-object-storage/lib-bucket-provisioner/"

readonly PKGS="./pkg/..."

# Because generated code exists under $REPO_ROOT/pkg/, it's necessary to filter it out
# This function get all sub packages under $REPO_ROOT/pkg/... except
valid_sub_packages() {
  # Exclude packages which should not be edited (pkg/apis/* && pkg/client/.*)
  local filteredPkgs="$(awk '!/pkg\/client/' <(go list -f '{{.Dir}}' ./...))"
  echo "$filteredPkgs"
}

readonly SUB_PACKAGES=$(valid_sub_packages)

# TODO (copejon) go tools should be staticly defined to a commit hash to enforce parity between dev and CI environment
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
      go build -a "${p}" || RET=$?
    done
    return $RET
  )
  return $?
}

test(){
  echo "-------- testing"
  (
    cd "${REPO_ROOT}"
    for p in "${PKGS}"; do
      go test -v "${p}" || RET=$?
    done
    return $RET
  )
  return $?
}

lint(){
  (
    cd "${REPO_ROOT}"
    for p in "${PKGS}"; do
      golangci-lint run "${p}"
    done
  )
}

linters(){
  golangci-lint linters
}

ci-checks(){
    echo "-------- beginning preflight checks"
    lint
    test || RET=$?
    build || RET=$?
    return $RET
}

help(){
  local msg=\
'
  This script accepts the following args:
  help          print this text
  vet           run go vet on core project code
  imports       run goimports with defined import priorities on core project code
                  (goimports also runs gofmt)
  imports-check run goimports but only report errors and diffs
  build         run go build on core project code
  test          run unit tests
  lint          run golangci-lint default linters
  linters       show enabled and disabled golangci-linters
  ci-checks     run golangci-lint, test, and build (executed in CI)

  For example, to vet and gofmt/imports, run:
  $ ./go.sh vet imports
'
  printf "%s\n" "${msg}"
}

verify-tool(){
  which golangci-lint &> /dev/null || (echo \
'WARNING! golangci-lint not found in PATH.

If you have not installed golangci-lint, you can do so with ANY of the following commands, replacing vX.Y.Z with the release version.
It is recommended you use v1.16.0 for parity with CI.

# binary will be $(go env GOPATH)/bin/golangci-lint
curl -sfL https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s -- -b $(go env GOPATH)/bin vX.Y.Z

# or install it into ./bin/
curl -sfL https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s vX.Y.Z

# In alpine linux (as it does not come with curl by default)
wget -O - -q https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s vX.Y.Z

Releases can be found at https://github.com/golangci/golangci-lint/releases

' && exit 1)
}

main(){
  verify-tool
  [[ ${#@} -eq 0 ]] && (help; exit 0)
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
    "lint")
      lint
      shift 1
      ;;
    "ci-checks")
      ci-checks
      exit $?
      ;;
    "linters")
      linters
      exit 1
      ;;
    "help"|'h')
      help
      exit
      ;;
    *)
      echo "unrecongnized args: $1"
      exit 1
      ;;
    esac
  done
}

main ${@}
