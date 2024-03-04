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
	utilconversion "sigs.k8s.io/cluster-api/util/conversion"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	infrav1beta1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
)

// ConvertTo converts this ICSVM to the Hub version (v1beta1).
func (src *ICSVM) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta1.ICSVM)
	if err := Convert_v1alpha4_ICSVM_To_v1beta1_ICSVM(src, dst, nil); err != nil {
		return err
	}

	// Manually restore data.
	restored := &infrav1beta1.ICSVM{}
	if ok, err := utilconversion.UnmarshalData(src, restored); err != nil || !ok {
		return err
	}

	return nil
}

// ConvertFrom converts from the Hub version (v1beta1) to this ICSVM.
func (dst *ICSVM) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*infrav1beta1.ICSVM)
	if err := Convert_v1beta1_ICSVM_To_v1alpha4_ICSVM(src, dst, nil); err != nil {
		return err
	}

	// Preserve Hub data on down-conversion.
	if err := utilconversion.MarshalData(src, dst); err != nil {
		return err
	}

	return nil
}

// ConvertTo converts this ICSVMList to the Hub version (v1beta1).
func (src *ICSVMList) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta1.ICSVMList)
	return Convert_v1alpha4_ICSVMList_To_v1beta1_ICSVMList(src, dst, nil)
}

// ConvertFrom converts this ICSVM to the Hub version (v1beta1).
func (dst *ICSVMList) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*infrav1beta1.ICSVMList)
	return Convert_v1beta1_ICSVMList_To_v1alpha4_ICSVMList(src, dst, nil)
}
