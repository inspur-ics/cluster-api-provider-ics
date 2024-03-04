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

package icenter


// AuthType respresents a valid method of authentication.
type AuthType string

const (
	// AuthPassword defines an unknown version of the password
	AuthPassword AuthType = "password"
	// AuthAccessKey defined an unknown version of the accesskey
	AuthAccessKey AuthType = "accesskey"
)

type Clouds struct {
	Clouds map[string]ICenter `yaml:"clouds" json:"clouds"`
}

// Cloud represents an entry in a clouds.yaml/secure.yaml file.
type ICenter struct {
	Cloud      string    `yaml:"cloud,omitempty" json:"cloud,omitempty"`
	AuthInfo   *AuthInfo `yaml:"auth,omitempty" json:"auth,omitempty"`
	AuthType   AuthType  `yaml:"auth_type,omitempty" json:"auth_type,omitempty"`

	// ICenterURL is the iCenter endpoint URL.
	ICenterURL string `yaml:"url,omitempty" json:"url,omitempty"`

	APIVersion string `yaml:"api_version,omitempty" json:"api_version,omitempty"`

	// Verify whether or not SSL API requests should be verified.
	Verify *bool `yaml:"verify,omitempty" json:"verify,omitempty"`

	// CACertFile a path to a CA Cert bundle that can be used as part of
	// verifying SSL API requests.
	CACertFile string `yaml:"cacert,omitempty" json:"cacert,omitempty"`

	// ClientCertFile a path to a client certificate to use as part of the SSL
	// transaction.
	ClientCertFile string `yaml:"cert,omitempty" json:"cert,omitempty"`

	// ClientKeyFile a path to a client key to use as part of the SSL
	// transaction.
	ClientKeyFile string `yaml:"key,omitempty" json:"key,omitempty"`
}

// AuthInfo represents the auth section of a icenter entry or
// auth options entered explicitly in ClientOpts.
type AuthInfo struct {
	// Token is a pre-generated authentication token.
	Token string `yaml:"token,omitempty" json:"token,omitempty"`

	// Username is the username of the user.
	Username string `yaml:"username,omitempty" json:"username,omitempty"`

	// Password is the password of the user.
	Password string `yaml:"password,omitempty" json:"password,omitempty"`

	// Credential ID to login with.
	CredentialId string `yaml:"credential_id,omitempty" json:"credential_id,omitempty"`

	// Credential secret to login with.
	CredentialSecret string `yaml:"credential_secret,omitempty" json:"credential_secret,omitempty"`

	// Reauth secret for delete options.
	ReauthSecret string `yaml:"reauth_secret,omitempty" json:"reauth_secret,omitempty"`

	// DomainName is the name of a domain which can be used to identify the
	// user domain.
	DomainName string `yaml:"domain,omitempty" json:"domain,omitempty"`

	// DefaultDomain is the domain ID to fall back on if no other domain has
	// been specified and a domain is required for scope.
	DefaultDomain string `yaml:"default_domain,omitempty" json:"default_domain,omitempty"`

	// Locale is the language of the user.
	Locale string `yaml:"locale,omitempty" json:"locale,omitempty"`

	// DefaultLocale is the default language of the user.
	DefaultLocale string `yaml:"default_locale,omitempty" json:"default_locale,omitempty"`
}


// clouds:
//   10.48.51.30:
//     auth:
//       username: "admin"
//       password: "inspur1!"
//       domain: "internal"
//       locale: "cn"
//     auth_type: "password"
//     url: "https://10.48.51.30:443"
//     verify: false
//     api_version: "5.8"