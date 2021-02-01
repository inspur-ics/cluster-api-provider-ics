/*
Copyright 2021 The Kubernetes Authors.

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
	"testing"

	. "github.com/onsi/gomega"
)

//nolint
func TestICSCluster_ValidateCreate(t *testing.T) {

	g := NewWithT(t)
	tests := []struct {
		name       string
		icsCluster *ICSCluster
		wantErr    bool
	}{
		{
			name:       "insecure true with empty thumbprint",
			icsCluster: createICSCluster("foo.com", true, ""),
			wantErr:    false,
		},
		{
			name:       "insecure false with non-empty thumbprint",
			icsCluster: createICSCluster("foo.com", false, "thumprint:foo"),
			wantErr:    false,
		},
		{
			name:       "insecure true with non-empty thumbprint",
			icsCluster: createICSCluster("foo.com", true, "thumprint:foo"),
			wantErr:    true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.icsCluster.ValidateCreate()
			if tc.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func createICSCluster(server string, insecure bool, thumbprint string) *ICSCluster {
	icsCluster := &ICSCluster{
		Spec: ICSClusterSpec{
			Server:     server,
			Insecure:   &insecure,
			Thumbprint: thumbprint,
		},
	}
	return icsCluster
}
