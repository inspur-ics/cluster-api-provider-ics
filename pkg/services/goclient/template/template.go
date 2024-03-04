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

package template

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/pkg/errors"

	basetypv1 "github.com/inspur-ics/ics-go-sdk/client/types"
	basevmv1 "github.com/inspur-ics/ics-go-sdk/vm"

	"github.com/inspur-ics/cluster-api-provider-ics/pkg/session"
)

type tplContext interface {
	context.Context
	GetLogger() logr.Logger
	GetSession() *session.Session
}

// FindTemplate finds a template based either on a UUID or name.
func FindTemplate(ctx tplContext, templateID string) (*basetypv1.VirtualMachine, error) {
	tpl, err := findTemplateByInstanceUUID(ctx, templateID)
	if err != nil {
		return nil, err
	}
	if tpl != nil {
		return tpl, nil
	}
	return findTemplateByName(ctx, templateID)
}

func findTemplateByInstanceUUID(ctx tplContext, templateID string) (*basetypv1.VirtualMachine, error) {
	if !isValidUUID(templateID) {
		return nil, nil
	}
	ctx.GetLogger().V(6).Info("find template by instance uuid", "instance-uuid", templateID)
	virtualMachineService := basevmv1.NewVirtualMachineService(ctx.GetSession().Client)
	tpl, err := virtualMachineService.GetVMTemplateByUUID(ctx, templateID)
	if err != nil {
		return nil, errors.Wrap(err, "error querying template by instance UUID")
	}
	if tpl != nil {
		return tpl, nil
	}
	return nil, nil
}

func findTemplateByName(ctx tplContext, templateName string) (*basetypv1.VirtualMachine, error) {
	ctx.GetLogger().V(6).Info("find template by name", "name", templateName)
	virtualMachineService := basevmv1.NewVirtualMachineService(ctx.GetSession().Client)
	tpl, err := virtualMachineService.GetVMTemplateByName(ctx, templateName)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to find tempate by name %q", templateName)
	}
	return tpl, nil
}

func isValidUUID(str string) bool {
	_, err := uuid.Parse(str)
	return err == nil
}
