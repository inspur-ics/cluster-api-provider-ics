#!/bin/bash

# Copyright 2024 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

#
# TODO(akutz) This script can probably be removed once v1alpha2 is released.
#

set -o errexit
set -o nounset
set -o pipefail

# Change directories to the parent directory of the one in which this
# script is located.
cd "$(dirname "${BASH_SOURCE[0]}")/.."

export CGO_ENABLED=0
export GOFLAGS="-ldflags=-extldflags=-static"
export GOPROXY="${GOPROXY:-https://proxy.golang.org}"

CLUSTERCTL_OUT="${CLUSTERCTL_OUT:-$(pwd)/clusterctl}"
CAPICS_MANAGER_IMAGE="${CAPICS_MANAGER_IMAGE:-gcr.io/cluster-api-provider-ics/dev/v1beta1/capics-manager:latest}"
CAPICS_MANIFEST_IMAGE="${CAPICS_MANIFEST_IMAGE:-gcr.io/cluster-api-provider-ics/dev/v1beta1/capics-manifests:latest}"
CAPI_MANAGER_IMAGE="${CAPI_MANAGER_IMAGE:-gcr.io/cluster-api-provider-ics/dev/v1beta1/capi-manager:latest}"
CABPK_MANAGER_IMAGE="${CABPK_MANAGER_IMAGE:-gcr.io/cluster-api-provider-ics/dev/v1beta1/cabpk-manager:latest}"

# Build the CAPICS manager image.
docker build -t "${CAPICS_MANAGER_IMAGE}" .
docker push "${CAPICS_MANAGER_IMAGE}"

# Build the CAPICS manifest image.
docker build \
  --build-arg "CAPICS_MANAGER_IMAGE=${CAPICS_MANAGER_IMAGE}" \
  -t "${CAPICS_MANIFEST_IMAGE}" \
  -f hack/tools/generate-yaml/Dockerfile \
  .
docker push "${CAPICS_MANIFEST_IMAGE}"

# Create a temporary directory into which the CAPI and CABPK repos can
# be cloned.
cd "$(mktemp -d)"

# Clone the CAPI and CABPK repositories.
git clone "${CAPI_REPO:-https://github.com/kubernetes-sigs/cluster-api.git}" capi
git clone "${CABPK_REPO:-https://github.com/kubernetes-sigs/cluster-api-bootstrap-provider-kubeadm}" cabpk

# Switch to the CAPI repo.
pushd capi

# Checkout the CAPI ref if one is set.
[ -n "${CAPI_REF-}" ] && git checkout -b "${CAPI_REF}" "${CAPI_REF}"

# Build clusterctl
go build -o "${CLUSTERCTL_OUT}" ./cmd/clusterctl

# Build the CAPI manager image.
docker build -t "${CAPI_MANAGER_IMAGE}" .
docker push "${CAPI_MANAGER_IMAGE}"

# Switch to the CABPK repo.
popd && pushd cabpk

# Checkout the CABPK ref if one is set.
[ -n "${CABPK_REF-}" ] && git checkout -b "${CABPK_REF}" "${CABPK_REF}"

# Build the CABPK manager image.
docker build -t "${CABPK_MANAGER_IMAGE}" .
docker push "${CABPK_MANAGER_IMAGE}"

cat <<EOF

clusterctl     ${CLUSTERCTL_OUT}
capi_manager   ${CAPI_MANAGER_IMAGE}
cabpk_manager  ${CABPK_MANAGER_IMAGE}
capics_manager   ${CAPICS_MANAGER_IMAGE}
capics_manifests ${CAPICS_MANIFEST_IMAGE}
EOF
