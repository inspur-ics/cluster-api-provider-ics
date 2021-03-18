/*
Copyright 2019 The Kubernetes Authors.

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

package session

import (
	"context"
	"k8s.io/klog"
	"strings"
	"sync"

	ics "github.com/inspur-ics/ics-go-sdk"
	token "github.com/inspur-ics/ics-go-sdk/session"
	"github.com/pkg/errors"
)

var sessionCache = map[string]Session{}
var sessionMU sync.Mutex

// Session is a ics session with a configured Finder.
type Session struct {
	*ics.ICSConnection
}

// GetOrCreate gets a cached session or creates a new one if one does not
// already exist.
func GetOrCreate(
	ctx context.Context,
	server, datacenter, username, password string) (*Session, error) {

	sessionMU.Lock()
	defer sessionMU.Unlock()

	sessionKey := server + username + datacenter
	if session, ok := sessionCache[sessionKey]; ok {
		m := token.NewManager(session.Client)
		if userToken, _ := m.UserSession(ctx); userToken != nil {
			klog.V(5).Info("Valid credentials. Reuse a token %s.", session.Client.Authorization)
			return &session, nil
		}
	}

	hostName := server
	port := "443"
	if strings.Contains(server, ":") {
		iCenterAddress := strings.Split(server, ":")
		if len(iCenterAddress) > 2 {
			klog.Errorf("iCenter Address Configuration error, server %s", server)
		}
		hostName = iCenterAddress[0]
		if len(iCenterAddress) == 2 {
			port = iCenterAddress[1]
		}
	}

	icsConn := ics.ICSConnection{
		Username:          username,
		Password:          password,
		Hostname:          hostName,
		Insecure:          true,
		Port:              port,
	}

	if err := icsConn.Connect(ctx); err != nil {
		return nil, errors.Wrapf(err, "error setting up new ics client")
	}

	session := Session{&icsConn}

	// Cache the session.
	sessionCache[sessionKey] = session

	return &session, nil
}
