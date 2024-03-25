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

package session

import (
	"context"
	"net/url"
	"regexp"
	"sync"
	"time"

	"github.com/blang/semver"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"

	basegov1 "github.com/inspur-ics/ics-go-sdk"
	basesenv1 "github.com/inspur-ics/ics-go-sdk/session"

	infrav1 "github.com/inspur-ics/cluster-api-provider-ics/api/v1beta1"
)

// global Session map against sessionKeys
// in map[sessionKey]Session.
var sessionCache sync.Map
var schemeMatch = regexp.MustCompile(`^\w+://`)

// Session is a ICS session with a configured Finder.
type Session struct {
	*basegov1.ICSConnection
}

func (s *Session) SessionIsActive(ctx context.Context, logger logr.Logger) (bool, error) {
	manager := basesenv1.NewManager(s.Client)
	if userToken, _ := manager.UserSession(ctx); userToken != nil {
		logger.V(10).Info("Valid credentials. Reuse a token %s.", s.Client.Authorization)
		return true, nil
	}
	return false, errors.New("The ICenter session has expired or timeout")
}

func (s *Session) Logout(ctx context.Context) error {
	manager := basesenv1.NewManager(s.Client)
	err := manager.Logout(ctx)
	if err != nil {
		return errors.Wrapf(err, "failed to exit ICenter client", "server", s.Hostname)
	}
	return nil
}

type Feature struct {
	KeepAliveDuration time.Duration
}

func DefaultFeature() Feature {
	return Feature{}
}

type Params struct {
	cloudName  string
	server     string
	userinfo   *url.Userinfo
	feature    Feature
	version    string
}

func NewParams() *Params {
	return &Params{
		feature: DefaultFeature(),
	}
}

func (p *Params) WithCloudName(cloudName string) *Params {
	p.cloudName = cloudName
	return p
}

func (p *Params) WithServer(server string) *Params {
	p.server = server
	return p
}

func (p *Params) WithUserInfo(username, password string) *Params {
	p.userinfo = url.UserPassword(username, password)
	return p
}

func (p *Params) WithAPIVersion(apiVersion string) *Params {
	p.version = apiVersion
	return p
}

func (p *Params) WithFeatures(feature Feature) *Params {
	p.feature = feature
	return p
}

// GetOrCreate gets a cached session or creates a new one if one does not
// already exist.
func GetOrCreate(ctx context.Context, params *Params) (*Session, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("session")

	sessionKey := params.cloudName
	if cachedSession, ok := sessionCache.Load(sessionKey); ok {
		s := cachedSession.(*Session)
		logger = logger.WithValues("server", params.cloudName)

		vimSessionActive, err := s.SessionIsActive(ctx, logger)
		if err != nil {
			logger.Error(err, "unable to check if vim session is active")
		}

		if vimSessionActive {
			logger.V(2).Info("found active cached ICS client session")
			return s, nil
		}
	}

	clearCache(logger, sessionKey)
	icenterURL, err := parseURL(params.server)
	if err != nil {
		return nil, errors.Wrapf(err, "error parsing ICenter URL %q", params.server)
	}
	if icenterURL == nil {
		return nil, errors.Errorf("error parsing ICenter URL %q", params.server)
	}

	icenterURL.User = params.userinfo
	client, err := newClient(ctx, logger, icenterURL)
	if err != nil {
		return nil, err
	}
	client.Client.Version = "6.12.0"

	session := Session{client}
	// Cache the session.
	sessionCache.Store(sessionKey, &session)

	logger.V(2).Info("cached ICS client session", "server", params.cloudName)

	return &session, nil
}

// Gets a cached session, already exist.
func Get(ctx context.Context, sessionKey string) (*Session, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("session")

	if cachedSession, ok := sessionCache.Load(sessionKey); ok {
		s := cachedSession.(*Session)
		logger = logger.WithValues("SessionKey", sessionKey)

		vimSessionActive, err := s.SessionIsActive(ctx, logger)
		if err != nil {
			logger.Error(err, "unable to check if vim session is active")
		}

		if vimSessionActive {
			logger.V(2).Info("found active cached ICS client session")
			return s, nil
		}
	}
	return nil, errors.Errorf("error found active cached ICS client session for key %s", sessionKey)
}

// ParseURL is wrapper around url.Parse, where Scheme defaults to "https"
func parseURL(s string) (*url.URL, error) {
	var err error
	var u *url.URL

	if s != "" {
		// Default the scheme to https
		if !schemeMatch.MatchString(s) {
			s = "https://" + s
		}

		u, err = url.Parse(s)
		if err != nil {
			return nil, err
		}

		if u.User == nil {
			u.User = url.UserPassword("", "")
		}
	}

	return u, nil
}

func newClient(ctx context.Context, logger logr.Logger, url *url.URL,) (*basegov1.ICSConnection, error) {
	password, _ := url.User.Password()
	c := &basegov1.ICSConnection{
		Username: url.User.Username(),
		Password: password,
		Hostname: url.Hostname(),
		Insecure: true,
		Port:     url.Port(),
	}

	if err := c.Connect(ctx); err != nil {
		logger.Error(err, "failed to new ICenter client", "server", url.Host)
		return nil, errors.Wrapf(err, "error setting up new ICenter client")
	}

	return c, nil
}

func clearCache(logger logr.Logger, sessionKey string) {
	if cachedSession, ok := sessionCache.Load(sessionKey); ok {
		s := cachedSession.(*Session)
		vimSessionActive, err := s.SessionIsActive(context.Background(), logger)
		if err != nil {
			logger.Error(err, "unable to get ICenter client session")
		} else if vimSessionActive {
			logger.V(6).Info("found active ICenter session, logging out")
			err := s.Logout(context.Background())
			if err != nil {
				logger.Error(err, "unable to logout ICenter session")
			}
		}
	}
	sessionCache.Delete(sessionKey)
}

func (s *Session) GetVersion() (infrav1.ICenterVersion, error) {
	svcVersion := s.Client.Version
	version, err := semver.New(svcVersion)
	if err != nil {
		return "", err
	}
	miniVersion, _ := semver.Make("6.12.0")
	if version.Compare(miniVersion) <= -1 {
		return "", unidentifiedICenterVersion{version: s.Client.Version}
	}
	return infrav1.NewICenterVersion(svcVersion), nil
}
