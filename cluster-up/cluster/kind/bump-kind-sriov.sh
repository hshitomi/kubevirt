#!/bin/bash -e
#
# This file is part of the KubeVirt project
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
# Copyright 2024 Red Hat, Inc.


# Usage ./hack/bump-kind-sriov.sh <kind_version> <k8s version>
# if no parameters are provided, it will take latest kind version, with k8s version according latest kubevirtci vm based provider
# if only kind_version provided, it will take k8s version according latest kubevirtci vm based provider
# examples: ./hack/bump-kind-sriov.sh v0.19.0
#           ./hack/bump-kind-sriov.sh v0.19.0 1.28
# Note: always takes the latest patch available
#
# https://github.com/kubernetes-sigs/kind/releases

KIND_RELEASE=${1:-$(curl -s https://api.github.com/repos/kubernetes-sigs/kind/releases/latest | jq -r .tag_name)}
K8S_VERSION=${2:-$(find cluster-provision/k8s/* -maxdepth 0 -type d -printf '%f\n' | tail -1 | cut -d'-' -f1)}

PROVIDER=kind-sriov

function main() {
    image=$(curl -sL https://api.github.com/repos/kubernetes-sigs/kind/releases/tags/$KIND_RELEASE | jq -r '.body' | grep $K8S_VERSION: | head -1 | awk '{print $3}' | tr -d \` | sed 's/\r//g')
    if [[ $image == "" ]]; then
        echo "ERROR: image not found for kind release $KIND_RELEASE, k8s version $K8S_VERSION"
        exit 1
    fi

    echo $image > cluster-up/cluster/$PROVIDER/image
    echo $KIND_RELEASE | cut -c2- > cluster-up/cluster/$PROVIDER/version
    echo "Set $KIND_RELEASE, image: $image"
}

main "$@"
