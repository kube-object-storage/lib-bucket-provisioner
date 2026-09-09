#!/usr/bin/env bash

# Generates the CRD manifests from the API types in
# pkg/apis/objectbucket.io/v1alpha1/*types.go using controller-gen.

set -o errexit
set -o nounset
set -o pipefail

CRD_DIR="deploy/crds"

go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.21.0 crd \
    paths="./pkg/apis/objectbucket.io/v1alpha1/..." \
    output:crd:dir="${CRD_DIR}"

# controller-gen names its output "<group>_<plural>.yaml"; rename to the
# existing objectbucket_v1alpha1_*_crd.yaml file names.
mv "${CRD_DIR}/objectbucket.io_objectbuckets.yaml" \
    "${CRD_DIR}/objectbucket_v1alpha1_objectbucket_crd.yaml"
mv "${CRD_DIR}/objectbucket.io_objectbucketclaims.yaml" \
    "${CRD_DIR}/objectbucket_v1alpha1_objectbucketclaim_crd.yaml"
