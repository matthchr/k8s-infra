/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package controllers_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	"golang.org/x/crypto/ssh"

	compute "github.com/Azure/k8s-infra/hack/generated/_apis/microsoft.compute/v1alpha1api20190701"
	network "github.com/Azure/k8s-infra/hack/generated/_apis/microsoft.network/v1alpha1api20200501"
	"github.com/Azure/k8s-infra/hack/generated/pkg/genruntime"
	"github.com/Azure/k8s-infra/hack/generated/pkg/testcommon"
)

func Test_VMSS_CRUD(t *testing.T) {
	t.Parallel()

	g := NewGomegaWithT(t)
	ctx := context.Background()
	testContext, err := testContext.ForTest(t)
	g.Expect(err).ToNot(HaveOccurred())

	sshPublicKey, err := generateSSHKey(2048)
	g.Expect(err).ToNot(HaveOccurred())

	rg, err := testContext.CreateNewTestResourceGroup(testcommon.WaitForCreation)
	g.Expect(err).ToNot(HaveOccurred())

	// TODO: The below vnet/subnet creation are lifted from the vnet/subnet tests... consider a way to avoid duplication here?
	// VNET
	vnet := &network.VirtualNetwork{
		ObjectMeta: testContext.MakeObjectMetaWithName(testContext.Namer.GenerateName("vnet")),
		Spec: network.VirtualNetworks_Spec{
			Location: testContext.AzureRegion,
			Owner:    testcommon.AsOwner(rg.ObjectMeta),
			Properties: network.VirtualNetworkPropertiesFormat{
				AddressSpace: network.AddressSpace{
					AddressPrefixes: []string{"172.16.0.0/24"},
				},
			},
		},
	}
	err = testContext.KubeClient.Create(ctx, vnet)
	g.Expect(err).ToNot(HaveOccurred())

	// It should be created in Kubernetes
	g.Eventually(vnet).Should(testContext.Match.BeProvisioned(ctx))

	subnet := &network.VirtualNetworksSubnet{
		ObjectMeta: testContext.MakeObjectMeta("subnet"),
		Spec: network.VirtualNetworksSubnets_Spec{
			Owner: testcommon.AsOwner(vnet.ObjectMeta),
			Properties: network.SubnetPropertiesFormat{
				AddressPrefix: "172.16.0.0/24",
			},
		},
	}

	// Create
	err = testContext.KubeClient.Create(ctx, subnet)
	g.Expect(err).ToNot(HaveOccurred())

	// It should be created in Kubernetes
	g.Eventually(subnet).Should(testContext.Match.BeProvisioned(ctx))
	g.Expect(subnet.Status.Id).ToNot(BeNil())

	// TODO: The below publicip creation was lifted from the publicip tests... coider a way to avoid duplication
	// Public IP Address
	publicIPAddressSku := network.PublicIPAddressSkuNameStandard
	publicIPAddress := &network.PublicIPAddresses{
		ObjectMeta: testContext.MakeObjectMetaWithName(testContext.Namer.GenerateName("publicip")),
		Spec: network.PublicIPAddresses_Spec{
			Location: testContext.AzureRegion,
			Owner:    testcommon.AsOwner(rg.ObjectMeta),
			Sku: &network.PublicIPAddressSku{
				Name: &publicIPAddressSku,
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

	// TODO: Below loadbalancer stolen from LB tests... figure a way to share code
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
								Reference: genruntime.MakeReferenceFromResource(publicIPAddress),
							},
						},
					},
				},
				InboundNatPools: []network.InboundNatPool{
					{
						Name: "MyFancyNatPool",
						Properties: &network.InboundNatPoolPropertiesFormat{
							FrontendIPConfiguration: network.SubResource{
								Reference: genruntime.ResourceReference{
									// TODO: Getting this is SUPER awkward
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

	g.Eventually(loadBalancer).Should(testContext.Match.BeProvisioned(ctx))

	// VMSS
	vmSize := "STANDARD_D1_v2"
	vmCount := 1
	faultDomainCount := 3
	singlePlacementGroup := false
	disablePasswordAuth := true
	publisher := "Canonical"
	offer := "UbuntuServer"
	sku := "18.04-lts"
	version := "latest"
	//nicPrimary := true
	upgradePolicyMode := compute.UpgradePolicyModeAutomatic
	computerNamePrefix := "computer"
	adminUsername := "adminUser"
	sshKeyPath := fmt.Sprintf("/home/%s/.ssh/authorized_keys", adminUsername)
	networkInterfacePrimary := true

	vmss := &compute.VirtualMachineScaleSet{
		ObjectMeta: testContext.MakeObjectMetaWithName(testContext.Namer.GenerateName("vmss")),
		Spec: compute.VirtualMachineScaleSets_Spec{
			Location: testContext.AzureRegion,
			Owner:    testcommon.AsOwner(rg.ObjectMeta),
			Sku: &compute.Sku{
				Name:     &vmSize,
				Capacity: &vmCount,
			},
			Properties: compute.VirtualMachineScaleSetProperties{
				PlatformFaultDomainCount: &faultDomainCount,
				SinglePlacementGroup:     &singlePlacementGroup,
				UpgradePolicy: &compute.UpgradePolicy{
					Mode: &upgradePolicyMode,
				},
				VirtualMachineProfile: &compute.VirtualMachineScaleSetVMProfile{
					StorageProfile: &compute.VirtualMachineScaleSetStorageProfile{
						ImageReference: &compute.ImageReference{
							Publisher: &publisher,
							Offer:     &offer,
							Sku:       &sku,
							Version:   &version,
						},
					},
					OsProfile: &compute.VirtualMachineScaleSetOSProfile{
						ComputerNamePrefix: &computerNamePrefix,
						AdminUsername:      &adminUsername,
						//AdminPassword: &adminPassword,
						LinuxConfiguration: &compute.LinuxConfiguration{
							DisablePasswordAuthentication: &disablePasswordAuth,
							Ssh: &compute.SshConfiguration{
								PublicKeys: []compute.SshPublicKey{
									{
										KeyData: sshPublicKey,
										Path:    &sshKeyPath,
									},
								},
							},
						},
					},
					// TODO: Need more
					NetworkProfile: &compute.VirtualMachineScaleSetNetworkProfile{
						NetworkInterfaceConfigurations: []compute.VirtualMachineScaleSetNetworkConfiguration{
							{
								Name: "mynicconfig",
								Properties: &compute.VirtualMachineScaleSetNetworkConfigurationProperties{
									Primary: &networkInterfacePrimary,
									IpConfigurations: []compute.VirtualMachineScaleSetIPConfiguration{
										{
											Name: "myipconfiguration",
											Properties: &compute.VirtualMachineScaleSetIPConfigurationProperties{
												Subnet: &compute.ApiEntityReference{
													Id: subnet.Status.Id,
												},
												LoadBalancerInboundNatPools: []compute.SubResource{
													{
														// TODO: It is the most awkward thing in the world that this is not a fully fledged resource
														Id: loadBalancer.Status.Properties.InboundNatPools[0].Id,
													},
												},
												//LoadBalancerBackendAddressPools: []compute.SubResource{
												//	{
												//		Id:
												//	},
												//},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	err = testContext.KubeClient.Create(ctx, vmss)
	g.Expect(err).ToNot(HaveOccurred())

	// It should be created in Kubernetes
	g.Eventually(vmss).Should(testContext.Match.BeProvisioned(ctx))
	g.Expect(vmss.Status.Id).ToNot(BeNil())
	armId := *vmss.Status.Id

	// TODO: Some other assertions?

	// Delete VMSS
	err = testContext.KubeClient.Delete(ctx, vmss)
	g.Expect(err).ToNot(HaveOccurred())
	g.Eventually(vmss).Should(testContext.Match.BeDeleted(ctx))

	// Ensure that the resource was really deleted in Azure
	exists, retryAfter, err := testContext.AzureClient.HeadResource(ctx, armId, "2019-07-01")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(retryAfter).To(BeZero())
	g.Expect(exists).To(BeFalse())
}

// TODO: Wondering if this should go somewhere else
func generateSSHKey(size int) (*string, error) {
	key, err := rsa.GenerateKey(rand.Reader, size)
	if err != nil {
		return nil, err
	}

	err = key.Validate()
	if err != nil {
		return nil, err
	}

	sshPublicKey, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}

	bytes := ssh.MarshalAuthorizedKey(sshPublicKey)
	result := string(bytes) // TODO: Is this right?

	return &result, nil
}
