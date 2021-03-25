/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package controllers_test

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"

	network "github.com/Azure/k8s-infra/hack/generated/_apis/microsoft.network/v1alpha1api20200501"
	"github.com/Azure/k8s-infra/hack/generated/pkg/genruntime"
	"github.com/Azure/k8s-infra/hack/generated/pkg/testcommon"
)

func Test_LoadBalancer_CRUD(t *testing.T) {
	t.Parallel()

	g := NewGomegaWithT(t)
	ctx := context.Background()
	testContext, err := testContext.ForTest(t)
	g.Expect(err).ToNot(HaveOccurred())
	rg, err := testContext.CreateNewTestResourceGroup(testcommon.WaitForCreation)
	g.Expect(err).ToNot(HaveOccurred())

	// TODO: This was stolen from the publicip test -- find a way to avoid code duplication?
	// Public IP Address
	// TODO: Type name is wrong -- should be PublicIPAddress
	sku := network.PublicIPAddressSkuNameStandard
	publicIPAddress := &network.PublicIPAddresses{
		ObjectMeta: testContext.MakeObjectMetaWithName(testContext.Namer.GenerateName("publicip")),
		Spec: network.PublicIPAddresses_Spec{
			Location: testContext.AzureRegion,
			Owner:    testcommon.AsOwner(rg.ObjectMeta),
			Sku: &network.PublicIPAddressSku{
				Name: &sku,
			},
			Properties: network.PublicIPAddressPropertiesFormat{
				PublicIPAllocationMethod: network.PublicIPAddressPropertiesFormatPublicIPAllocationMethodStatic,
			},
		},
	}

	err = testContext.KubeClient.Create(ctx, publicIPAddress)
	g.Expect(err).ToNot(HaveOccurred())
	// It should be created in Kubernetes
	g.Eventually(publicIPAddress).Should(testContext.Match.BeProvisioned(ctx))
	g.Expect(publicIPAddress.Status.Id).ToNot(BeNil())

	// LoadBalancer
	loadBalancerSku := network.LoadBalancerSkuNameStandard
	lbName := testContext.Namer.GenerateName("loadbalancer")
	lbFrontendName := "LoadBalancerFrontend"
	loadBalancer := &network.LoadBalancer{
		ObjectMeta: testContext.MakeObjectMetaWithName(lbName),
		Spec: network.LoadBalancers_Spec{
			Location: testContext.AzureRegion,
			Owner:    testcommon.AsOwner(rg.ObjectMeta),
			Sku: &network.LoadBalancerSku{
				Name: &loadBalancerSku,
			},
			Properties: network.LoadBalancerPropertiesFormat{
				FrontendIPConfigurations: []network.FrontendIPConfiguration{
					{
						Name: lbFrontendName,
						Properties: &network.FrontendIPConfigurationPropertiesFormat{
							PublicIPAddress: &network.SubResource{
								Reference: genruntime.ResourceReference{
									Group: publicIPAddress.GroupVersionKind().Group,
									Kind: publicIPAddress.GroupVersionKind().Kind,
									Namespace: publicIPAddress.Namespace,
									Name: publicIPAddress.Name,
								},
							},
						},
					},
				},
				// TODO: The below stuff isn't really necessary for LB CRUD but is required for VMSS...
				InboundNatPools: []network.InboundNatPool{
					{
						Name: "MyFancyNatPool",
						Properties: &network.InboundNatPoolPropertiesFormat{
							FrontendIPConfiguration: network.SubResource{
								Reference: genruntime.ResourceReference{
									// TODO: This is still really awkward
									ARMID: testContext.MakeARMId(rg.Name, "Microsoft.Network", "loadBalancers", lbName, "frontendIPConfigurations", lbFrontendName),
								},
							},
							Protocol:               network.InboundNatPoolPropertiesFormatProtocolTcp,
							FrontendPortRangeStart: 50000,
							FrontendPortRangeEnd:   51000,
							BackendPort:            22,
						},
					},
				},
			},
		},
	}

	err = testContext.KubeClient.Create(ctx, loadBalancer)
	g.Expect(err).ToNot(HaveOccurred())

	// It should be created in Kubernetes
	g.Eventually(loadBalancer).Should(testContext.Match.BeProvisioned(ctx))
	g.Expect(loadBalancer.Status.Id).ToNot(BeNil())
	armId := *loadBalancer.Status.Id

	// Delete LoadBalancer
	err = testContext.KubeClient.Delete(ctx, loadBalancer)
	g.Expect(err).ToNot(HaveOccurred())
	g.Eventually(loadBalancer).Should(testContext.Match.BeDeleted(ctx))

	// Ensure that the resource was really deleted in Azure
	exists, retryAfter, err := testContext.AzureClient.HeadResource(ctx, armId, "2020-05-01")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(retryAfter).To(BeZero())
	g.Expect(exists).To(BeFalse())

	// Delete Public IP
	err = testContext.KubeClient.Delete(ctx, publicIPAddress)
	g.Expect(err).ToNot(HaveOccurred())
	g.Eventually(publicIPAddress).Should(testContext.Match.BeDeleted(ctx))
}
