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
	apiconversion "k8s.io/apimachinery/pkg/conversion"
	clusterv1a4 "sigs.k8s.io/cluster-api/api/v1alpha4"
	clusterv1b1 "sigs.k8s.io/cluster-api/api/v1beta1"
	utilconversion "sigs.k8s.io/cluster-api/util/conversion"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	infrav1beta1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
)

// ConvertTo.
func (src *ICSMachineTemplate) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta1.ICSMachineTemplate) //nolint:forcetypeassert
	if err := Convert_v1alpha4_ICSMachineTemplate_To_v1beta1_ICSMachineTemplate(src, dst, nil); err != nil {
		return err
	}

	// Manually restore data.
	restored := &infrav1beta1.ICSMachineTemplate{}
	if ok, err := utilconversion.UnmarshalData(src, restored); err != nil || !ok {
		return err
	}

	return nil
}

func (dst *ICSMachineTemplate) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*infrav1beta1.ICSMachineTemplate) //nolint:forcetypeassert
	if err := Convert_v1beta1_ICSMachineTemplate_To_v1alpha4_ICSMachineTemplate(src, dst, nil); err != nil {
		return err
	}

	// Preserve Hub data on down-conversion.
	if err := utilconversion.MarshalData(src, dst); err != nil {
		return err
	}

	return nil
}

func (src *ICSMachineTemplateList) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta1.ICSMachineTemplateList) //nolint:forcetypeassert
	return Convert_v1alpha4_ICSMachineTemplateList_To_v1beta1_ICSMachineTemplateList(src, dst, nil)
}

func (dst *ICSMachineTemplateList) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*infrav1beta1.ICSMachineTemplateList) //nolint:forcetypeassert
	return Convert_v1beta1_ICSMachineTemplateList_To_v1alpha4_ICSMachineTemplateList(src, dst, nil)
}

//nolint
func Convert_v1alpha4_ObjectMeta_To_v1beta1_ObjectMeta(in *clusterv1a4.ObjectMeta, out *clusterv1b1.ObjectMeta, s apiconversion.Scope) error {
	// wrapping the conversion func to avoid having compile errors due to compileErrorOnMissingConversion()
	// more details at https://github.com/kubernetes/kubernetes/issues/98380
	return clusterv1a4.Convert_v1alpha4_ObjectMeta_To_v1beta1_ObjectMeta(in, out, s)
}

//nolint
func Convert_v1beta1_ObjectMeta_To_v1alpha4_ObjectMeta(in *clusterv1b1.ObjectMeta, out *clusterv1a4.ObjectMeta, s apiconversion.Scope) error {
	// wrapping the conversion func to avoid having compile errors due to compileErrorOnMissingConversion()
	// more details at https://github.com/kubernetes/kubernetes/issues/98380
	return clusterv1a4.Convert_v1beta1_ObjectMeta_To_v1alpha4_ObjectMeta(in, out, s)
}