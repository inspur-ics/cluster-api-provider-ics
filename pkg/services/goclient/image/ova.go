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
package image

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"

	basetypv1 "github.com/inspur-ics/ics-go-sdk/client/types"
	basestv1 "github.com/inspur-ics/ics-go-sdk/storage"
	basevmv1 "github.com/inspur-ics/ics-go-sdk/vm"

	"github.com/inspur-ics/cluster-api-provider-ics/pkg/session"
)

type ovfContext interface {
	context.Context
	GetLogger() logr.Logger
	GetSession() *session.Session
}

func FindOvaImageByName(ctx ovfContext, imageName string) (*basetypv1.ImageFileInfo, error) {
	ctx.GetLogger().V(6).Info("find template by name", "name", imageName)
	imageStorageService := basestv1.NewStorageService(ctx.GetSession().Client)
	ova, err := imageStorageService.GetImageFileInfoByName(ctx, imageName)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to find ova image by name %q", imageName)
	}
	return ova, nil
}

func GetVMForm(ctx ovfContext, ovaFilePath string, hostUUID string, imageHostUUID string) (*basetypv1.VirtualMachine, error) {
	virtualMachineService := basevmv1.NewVirtualMachineService(ctx.GetSession().Client)
	vmForm, err := virtualMachineService.GetOvaConfig(ctx, ovaFilePath, hostUUID, imageHostUUID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get the vm form by name %q", ovaFilePath)
	}
	return vmForm, nil
}
