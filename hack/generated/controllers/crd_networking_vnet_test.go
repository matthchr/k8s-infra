/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package controllers_test

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	network "github.com/Azure/k8s-infra/hack/generated/_apis/microsoft.network/v1alpha1api20200501"
	"github.com/Azure/k8s-infra/hack/generated/pkg/testcommon"
)

func Test_VirtualNetwork_CRUD(t *testing.T) {
	t.Parallel()

	g := NewGomegaWithT(t)
	ctx := context.Background()
	testContext, err := testContext.ForTest(t)
	g.Expect(err).ToNot(HaveOccurred())

	rg, err := testContext.CreateNewTestResourceGroup(testcommon.WaitForCreation)
	g.Expect(err).ToNot(HaveOccurred())

	// VNET
	vnet := &network.VirtualNetwork{
		ObjectMeta: testContext.MakeObjectMetaWithName(testContext.Namer.GenerateName("vnet")),
		Spec: network.VirtualNetworks_Spec{
			Location: testContext.AzureRegion,
			Owner:    testcommon.AsOwner(rg.ObjectMeta),
			Properties: network.VirtualNetworkPropertiesFormat{
				AddressSpace: network.AddressSpace{
					AddressPrefixes: []string{"172.16.0.0/16"},
				},
			},
		},
	}
	err = testContext.KubeClient.Create(ctx, vnet)
	g.Expect(err).ToNot(HaveOccurred())

	// It should be created in Kubernetes
	g.Eventually(vnet).Should(testContext.Match.BeProvisioned(ctx))
	g.Expect(vnet.Status.Id).ToNot(BeNil())
	armId := *vnet.Status.Id

	t.Run("Subnet CRUD", func(t *testing.T) {
		VirtualNetwork_Subnet_CRUD(t, testContext, vnet.ObjectMeta)
	})

	// Delete
	err = testContext.KubeClient.Delete(ctx, vnet)
	g.Expect(err).ToNot(HaveOccurred())
	g.Eventually(vnet).Should(testContext.Match.BeDeleted(ctx))

	// Ensure that the resource was really deleted in Azure
	exists, retryAfter, err := testContext.AzureClient.HeadResource(ctx, armId, "2020-05-01")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(retryAfter).To(BeZero())
	g.Expect(exists).To(BeFalse())
}

func VirtualNetwork_Subnet_CRUD(t *testing.T, testContext testcommon.KubePerTestContext, virtualNetwork metav1.ObjectMeta) {
	ctx := context.Background()

	g := NewGomegaWithT(t)

	subnet := &network.VirtualNetworksSubnet{
		ObjectMeta: testContext.MakeObjectMeta("subnet"),
		Spec: network.VirtualNetworksSubnets_Spec{
			Owner: testcommon.AsOwner(virtualNetwork),
			Properties: network.SubnetPropertiesFormat{
				AddressPrefix: "172.16.0.0/24",
			},
		},
	}

	// Create
	err := testContext.KubeClient.Create(ctx, subnet)
	g.Expect(err).ToNot(HaveOccurred())

	// It should be created in Kubernetes
	g.Eventually(subnet).Should(testContext.Match.BeProvisioned(ctx))
	g.Expect(subnet.Status.Id).ToNot(BeNil())
	armId := *subnet.Status.Id

	// Delete
	err = testContext.KubeClient.Delete(ctx, subnet)
	g.Expect(err).ToNot(HaveOccurred())
	g.Eventually(subnet).Should(testContext.Match.BeDeleted(ctx))

	// Ensure that the resource was really deleted in Azure
	exists, retryAfter, err := testContext.AzureClient.HeadResource(ctx, armId, "2020-05-01")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(retryAfter).To(BeZero())
	g.Expect(exists).To(BeFalse())
}
