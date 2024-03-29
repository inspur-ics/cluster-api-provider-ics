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

################################################################################
# usage: verify-crds.sh
#  This program ensures the commited CRDs match generated CRDs
################################################################################

set -o errexit
set -o nounset
set -o pipefail

# Change directories to the parent directory of the one in which this
# script is located.
cd "$(dirname "${BASH_SOURCE[0]}")/.."

_diff_log="$(mktemp)"
_output_dir="$(mktemp -d)"

echo "verify-crds: generating crds"
MANIFEST_ROOT="${_output_dir}" make generate-manifests

_exit_code=0
echo "verify-crds: comparing crds"
while IFS= read -r -d '' dst_file; do
  src_file="${dst_file#"${_output_dir}/"}"
  echo "  config/${src_file}"
  diff "${dst_file}" "./config/${src_file}" >"${_diff_log}" || _exit_code="${?}"
  if [ "${_exit_code}" -ne "0" ]; then
    echo "config/${src_file}" 1>&2
    cat "${_diff_log}" 1>&2
    echo 1>&2
  fi
done < <(find "${_output_dir}" -type f -print0)

exit "${_exit_code}"
