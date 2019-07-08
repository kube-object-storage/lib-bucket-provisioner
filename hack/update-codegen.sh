#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT="$(realpath -LP $(dirname ${BASH_SOURCE})/../)"
IMPORT_PATH="github.com/kube-object-storage/lib-bucket-provisioner"

APIS_DIR=${IMPORT_PATH}/pkg/apis
CLIENT_DIR=${IMPORT_PATH}/pkg/client
GENERATORS=${REPO_ROOT}/vendor/k8s.io/code-generator

GROUP_VERSIONS="objectbucket.io:v1alpha1"

"${GENERATORS}/generate-groups.sh" all "${CLIENT_DIR}" "${APIS_DIR}" "${GROUP_VERSIONS}"
