/*
Copyright 2024 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package constants

import "time"

const (
	// DefaultEnableKeepAlive is false by default.
	DefaultEnableKeepAlive = false

	// KeepaliveDuration unit minutes.
	DefaultKeepAliveDuration = time.Minute * 5

	NodeLabelPrefix = "node.cluster.x-k8s.io"

	INodeInfoLabel = NodeLabelPrefix + "/inode"
)
