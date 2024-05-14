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

package context

import (
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/cluster-api/util/patch"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
	"github.com/inspur-ics/cluster-api-provider-ics/pkg/session"
)

// IPAddressContext is a Go context used with a IPAddress.
type IPAddressContext struct {
	*ControllerContext
	IPAddress      *infrav1.IPAddress
	PatchHelper    *patch.Helper
	Logger         logr.Logger
	Session        *session.Session
}

// String returns IPAddressGroupVersionKind IPAddressNamespace/IPAddressName.
func (c *IPAddressContext) String() string {
	return fmt.Sprintf("%s %s/%s", c.IPAddress.GroupVersionKind(), c.IPAddress.Namespace, c.IPAddress.Name)
}

// Patch updates the object and its status on the API server.
func (c *IPAddressContext) Patch() error {
	return c.PatchHelper.Patch(c, c.IPAddress)
}

// GetLogger returns this context's logger.
func (c *IPAddressContext) GetLogger() logr.Logger {
	return c.Logger
}

// GetSession returns this context's session.
func (c *IPAddressContext) GetSession() *session.Session {
	return c.Session
}
