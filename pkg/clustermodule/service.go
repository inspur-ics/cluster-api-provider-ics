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

	basetypv1 "github.com/inspur-ics/ics-go-sdk/client/types"
	basecluv1 "github.com/inspur-ics/ics-go-sdk/cluster"

	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/services/goclient/cluster"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/session"
)

const validMachineTemplate = "ICSMachineTemplate"

type service struct{}

func NewService() Service {
	return service{}
}

func (s service) Create(ctx *context.ClusterContext, wrapper Wrapper) (string, error) {
	logger := ctx.Logger.WithValues("object", wrapper.GetName(), "namespace", wrapper.GetNamespace())

	templateRef, err := fetchTemplateRef(ctx, ctx.Client, wrapper)
	if err != nil {
		logger.V(4).Error(err, "error fetching template for object")
		return "", errors.Wrapf(err, "error fetching machine template for object %s/%s", wrapper.GetNamespace(), wrapper.GetName())
	}
	if templateRef.Kind != validMachineTemplate {
		// since this is a heterogeneous cluster, we should skip cluster module creation for non ICSMachine objects
		logger.V(4).Info("skipping module creation for object")
		return "", nil
	}

	template, err := fetchMachineTemplate(ctx, wrapper, templateRef.Name)
	if err != nil {
		logger.V(4).Error(err, "error fetching template")
		return "", err
	}
	if server := template.Spec.Template.Spec.CloudName; server != ctx.ICSCluster.Spec.CloudName {
		logger.V(4).Info("skipping module creation for object since template uses a different server", "server", server)
		return "", nil
	}

	iCenterSession, err := fetchSessionForObject(ctx, template)
	if err != nil {
		logger.V(4).Error(err, "error fetching session")
		return "", err
	}

	// Fetch the compute cluster resource by tracing the owner of the resource pool in use.
	computeClusterRef, err := getComputeClusterResource(ctx, iCenterSession, template.Spec.Template.Spec.Cluster)
	if err != nil {
		logger.V(4).Error(err, "error fetching compute cluster resource")
		return "", err
	}

	provider := cluster.NewProvider(iCenterSession.Client)
	moduleUUID, err := provider.CreateModule(ctx, computeClusterRef)
	if err != nil {
		logger.V(4).Error(err, "error creating cluster module")
		return "", err
	}
	logger.V(4).Info("created cluster module for object", "moduleUUID", moduleUUID)
	return moduleUUID, nil
}

func (s service) DoesExist(ctx *context.ClusterContext, wrapper Wrapper, moduleUUID string) (bool, error) {
	logger := ctx.Logger.WithValues("object", wrapper.GetName())

	templateRef, err := fetchTemplateRef(ctx, ctx.Client, wrapper)
	if err != nil {
		logger.V(4).Error(err, "error fetching template for object")
		return false, errors.Wrapf(err, "error fetching infrastructure machine template for object %s/%s", wrapper.GetNamespace(), wrapper.GetName())
	}

	template, err := fetchMachineTemplate(ctx, wrapper, templateRef.Name)
	if err != nil {
		logger.V(4).Error(err, "error fetching template")
		return false, err
	}

	iCenterSession, err := fetchSessionForObject(ctx, template)
	if err != nil {
		logger.V(4).Error(err, "error fetching session")
		return false, err
	}

	// Fetch the compute cluster resource by tracing the owner of the resource pool in use.
	computeClusterRef, err := getComputeClusterResource(ctx, iCenterSession, template.Spec.Template.Spec.Cluster)
	if err != nil {
		logger.V(4).Error(err, "error fetching compute cluster resource")
		return false, err
	}

	provider := cluster.NewProvider(iCenterSession.Client)
	return provider.DoesModuleExist(ctx, moduleUUID, computeClusterRef)
}

func (s service) Remove(ctx *context.ClusterContext, moduleUUID string) error {
	iCenterSession, err := fetchSession(ctx)
	if err != nil {
		return err
	}

	provider := cluster.NewProvider(iCenterSession.Client)
	if err := provider.DeleteModule(ctx, moduleUUID); err != nil {
		return err
	}
	return nil
}

func getComputeClusterResource(ctx goctx.Context, s *session.Session, resourcePool string) (basetypv1.ManagedObjectReference, error) {
	finder := basecluv1.NewClusterService(s.Client)
	cc, err := finder.GetClusterByName(ctx, resourcePool)
	if err != nil {
		return basetypv1.ManagedObjectReference{}, err
	}
	return basetypv1.ManagedObjectReference{
		Type: "id",
		Value: cc.Id,
	}, nil
}
