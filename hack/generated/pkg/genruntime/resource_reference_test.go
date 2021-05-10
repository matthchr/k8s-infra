/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package genruntime_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/Azure/k8s-infra/hack/generated/pkg/genruntime"
)

var validARMIDRef = genruntime.ResourceReference{ARMID: "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/microsoft.compute/VirtualMachine/myvm"}
var validKubRef = genruntime.ResourceReference{Group: "microsoft.resources.infra.azure.com", Kind: "ResourceGroup", Namespace: "default", Name: "myrg"}
var invalidRefBothSpecified = genruntime.ResourceReference{Group: "microsoft.resources.infra.azure.com", Kind: "ResourceGroup", Namespace: "default", Name: "myrg", ARMID: "oops"}
var invalidRefNeitherSpecified = genruntime.ResourceReference{}
var invalidRefIncompleteKubReference = genruntime.ResourceReference{Group: "microsoft.resources.infra.azure.com", Namespace: "default", Name: "myrg"}

func Test_ResourceReference_Validate(t *testing.T) {
	tests := []struct {
		name        string
		ref         genruntime.ResourceReference
		errExpected bool
	}{
		{
			name:        "valid ARM reference is valid",
			ref:         validARMIDRef,
			errExpected: false,
		},
		{
			name:        "valid Kubernetes reference is valid",
			ref:         validKubRef,
			errExpected: false,
		},
		{
			name:        "both ARM and Kubernetes fields filled out, reference is invalid",
			ref:         invalidRefBothSpecified,
			errExpected: true,
		},
		{
			name:        "nothing filled out, reference is invalid",
			ref:         invalidRefNeitherSpecified,
			errExpected: true,
		},
		{
			name:        "incomplete Kubernetes reference is invalid",
			ref:         invalidRefIncompleteKubReference,
			errExpected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.ref.Validate()
			if tt.errExpected {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}
		})
	}
}

func Test_ResourceReference_IsARMOrKubernetes(t *testing.T) {
	tests := []struct {
		name         string
		ref          genruntime.ResourceReference
		isARM        bool
		isKubernetes bool
	}{
		{
			name:         "valid ARM reference is ARM",
			ref:          validARMIDRef,
			isARM:        true,
			isKubernetes: false,
		},
		{
			name:         "valid Kubernetes reference is Kubernetes",
			ref:          validKubRef,
			isARM:        false,
			isKubernetes: true,
		},
		{
			name:         "both ARM and Kubernetes fields filled out, reference is neither",
			ref:          invalidRefBothSpecified,
			isARM:        false,
			isKubernetes: false,
		},
		{
			name:         "nothing filled out, reference is neither",
			ref:          invalidRefNeitherSpecified,
			isARM:        false,
			isKubernetes: false,
		},
		{
			name:         "incomplete Kubernetes reference is neither",
			ref:          invalidRefIncompleteKubReference,
			isARM:        false,
			isKubernetes: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			g.Expect(tt.ref.IsDirectARMReference()).To(Equal(tt.isARM))
			g.Expect(tt.ref.IsKubernetesReference()).To(Equal(tt.isKubernetes))
		})
	}
}
