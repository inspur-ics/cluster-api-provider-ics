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

package clustermodule

import (
	goctx "context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/identity"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/session"
)

func fetchSessionForObject(ctx *context.ClusterContext, template *infrav1.ICSMachineTemplate) (*session.Session, error) {
	params := newParams(*ctx)
	iCenter, err := identity.NewClientFromMachine(ctx, ctx.Client, template.Namespace, template.Spec.Template.Spec.CloudName, template.Spec.Template.Spec.IdentityRef)
	if err != nil {
		return nil, err
	}

	params.WithCloudName(template.Spec.Template.Spec.CloudName).
		WithServer(iCenter.ICenterURL).
		WithUserInfo(iCenter.AuthInfo.Username, iCenter.AuthInfo.Password).
		WithAPIVersion(iCenter.APIVersion)
	session, err := session.GetOrCreate(ctx, params)
	return session, err
}

func newParams(ctx context.ClusterContext) *session.Params {
	return session.NewParams().
		WithFeatures(session.Feature{
			KeepAliveDuration: ctx.KeepAliveDuration,
		})
}

func fetchSession(ctx *context.ClusterContext) (*session.Session, error) {
	iCenter, err := identity.NewClientFromCluster(ctx, ctx.Client, ctx.ICSCluster)
	if err != nil {
		return nil, err
	}

	params := session.NewParams().
		WithCloudName(ctx.ICSCluster.Spec.CloudName).
		WithServer(iCenter.ICenterURL).
		WithUserInfo(iCenter.AuthInfo.Username, iCenter.AuthInfo.Password).
		WithAPIVersion(iCenter.APIVersion)
	session, err := session.GetOrCreate(ctx, params)
	return session, err
}

func fetchTemplateRef(ctx goctx.Context, c client.Client, input Wrapper) (*corev1.ObjectReference, error) {
	obj := new(unstructured.Unstructured)
	obj.SetAPIVersion(input.GetObjectKind().GroupVersionKind().GroupVersion().String())
	obj.SetKind(input.GetObjectKind().GroupVersionKind().Kind)
	obj.SetName(input.GetName())
	key := client.ObjectKey{Name: obj.GetName(), Namespace: input.GetNamespace()}
	if err := c.Get(ctx, key, obj); err != nil {
		return nil, errors.Wrapf(err, "failed to retrieve %s external object %q/%q", obj.GetKind(), key.Namespace, key.Name)
	}

	objRef := corev1.ObjectReference{}
	if err := util.UnstructuredUnmarshalField(obj, &objRef, input.GetTemplatePath()...); err != nil && err != util.ErrUnstructuredFieldNotFound {
		return nil, err
	}
	return &objRef, nil
}

func fetchMachineTemplate(ctx *context.ClusterContext, input Wrapper, templateName string) (*infrav1.ICSMachineTemplate, error) {
	template := &infrav1.ICSMachineTemplate{}
	if err := ctx.Client.Get(ctx, client.ObjectKey{
		Name:      templateName,
		Namespace: input.GetNamespace(),
	}, template); err != nil {
		return nil, err
	}
	return template, nil
}
