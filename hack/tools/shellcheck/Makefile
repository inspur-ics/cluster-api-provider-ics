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

all: build

IMAGE_VERSION ?= v0.6.0
IMAGE_NAME ?= gcr.io/cluster-api-provider-ics/extra/shellcheck
IMAGE_TAG ?= $(IMAGE_NAME):$(IMAGE_VERSION)

build:
	docker build --build-arg SHELLCHECK_VERSION=$(IMAGE_VERSION) -t $(IMAGE_TAG) .
.PHONY: build

push:
	docker push $(IMAGE_TAG)
.PHONY: push
