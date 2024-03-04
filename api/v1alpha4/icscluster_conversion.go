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

package v1alpha4

import (
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	infrav1beta1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
)


// ConvertTo converts this ICSCluster to the Hub version (v1beta1).
func (src *ICSCluster) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta1.ICSCluster)
	return Convert_v1alpha4_ICSCluster_To_v1beta1_ICSCluster(src, dst, nil)
}

// ConvertFrom converts from the Hub version (v1beta1) to this ICSCluster.
func (dst *ICSCluster) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*infrav1beta1.ICSCluster)
	return Convert_v1beta1_ICSCluster_To_v1alpha4_ICSCluster(src, dst, nil)
}

// ConvertTo converts this ICSClusterList to the Hub version (v1beta1).
func (src *ICSClusterList) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta1.ICSClusterList)
	return Convert_v1alpha4_ICSClusterList_To_v1beta1_ICSClusterList(src, dst, nil)
}

// ConvertFrom converts this ICSVM to the Hub version (v1beta1).
func (dst *ICSClusterList) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*infrav1beta1.ICSClusterList)
	return Convert_v1beta1_ICSClusterList_To_v1alpha4_ICSClusterList(src, dst, nil)
}