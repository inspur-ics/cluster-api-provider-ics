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

package manager

import (
	"os"
	"strings"
	"time"

	"github.com/inspur-ics/cluster-api-provider-ics/pkg/context"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"
)

// AddToManagerFunc is a function that can be optionally specified with
// the manager's Options in order to explicitly decide what controllers and
// webhooks to add to the manager.
type AddToManagerFunc func(*context.ControllerManagerContext, ctrlmgr.Manager) error

// Options describes the options used to create a new CAPICS manager.
type Options struct {
	ctrlmgr.Options

	// EnableKeepAlive is a session feature to enable keep alive handler
	// for better load management on ICS api server
	EnableKeepAlive bool

	// MaxConcurrentReconciles the maximum number of allowed, concurrent
	// reconciles.
	//
	// Defaults to the eponymous constant in this package.
	MaxConcurrentReconciles int

	// LeaderElectionNamespace is the namespace in which the pod running the
	// controller maintains a leader election lock
	//
	// Defaults to the eponymous constant in this package.
	PodNamespace string

	// PodName is the name of the pod running the controller manager.
	//
	// Defaults to the eponymous constant in this package.
	PodName string

	// KeepAliveDuration is the idle time interval in between send() requests
	// in keepalive handler
	KeepAliveDuration time.Duration

	KubeConfig *rest.Config

	// AddToManager is a function that can be optionally specified with
	// the manager's Options in order to explicitly decide what controllers
	// and webhooks to add to the manager.
	AddToManager AddToManagerFunc
}

func (o *Options) defaults() {
	if o.Logger.GetSink() == nil {
		o.Logger = ctrllog.Log
	}

	if o.PodName == "" {
		o.PodName = DefaultPodName
	}

	if o.KubeConfig == nil {
		o.KubeConfig = config.GetConfigOrDie()
	}

	if o.Scheme == nil {
		o.Scheme = runtime.NewScheme()
	}

	if ns, ok := os.LookupEnv("POD_NAMESPACE"); ok {
		o.PodNamespace = ns
	} else if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if ns := strings.TrimSpace(string(data)); len(ns) > 0 {
			o.PodNamespace = ns
		}
	} else {
		o.PodNamespace = DefaultPodNamespace
	}
}
