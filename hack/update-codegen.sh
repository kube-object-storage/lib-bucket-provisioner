#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

KUBE_CODE_GEN_VERSION="kubernetes-1.19.0"
GROUP_VERSIONS="objectbucket.io:v1alpha1"

scriptdir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
repo_root="$( cd "${scriptdir}"/../ && pwd )"
codegendir="${repo_root}/vendor/k8s.io/code-generator"

# vendoring k8s.io/code-generator temporarily
echo "require k8s.io/code-generator ${KUBE_CODE_GEN_VERSION}" >> "${repo_root}/go.mod"
go mod vendor
git checkout HEAD "${repo_root}/go.mod" "${repo_root}/go.sum" # reset go.mod and go.sum

IMPORT_PATH="github.com/kube-object-storage/lib-bucket-provisioner"

APIS_PATH="${IMPORT_PATH}/pkg/apis"
CLIENT_PATH="${IMPORT_PATH}/pkg/client"

bash "${codegendir}/generate-groups.sh" \
    all \
    "${CLIENT_PATH}" \
    "${APIS_PATH}" \
    "${GROUP_VERSIONS}" \
    --output-base "${repo_root}/vendor" \
    --go-header-file "${scriptdir}/boilerplate.go.txt"

cp -r "${repo_root}/vendor/${IMPORT_PATH}/pkg" "${repo_root}/"
