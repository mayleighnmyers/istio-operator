package conversion

import (
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"

	v1 "github.com/maistra/istio-operator/pkg/apis/maistra/v1"
	v2 "github.com/maistra/istio-operator/pkg/apis/maistra/v2"
	"github.com/maistra/istio-operator/pkg/controller/versions"
)

const (
	searchSuffixGlobal                  = "global"
	searchSuffixNamespaceGlobalTemplate = "{{ valueOrDefault .DeploymentMeta.Namespace \"%s\" }}.global"

	clusterDomainDefault = "cluster.local"
)

var externalRequestedNetworkRegex = regexp.MustCompile("(^|,)external(,|$)")

// populateClusterValues popluates values.yaml specific to clustering.  this
// function will also update fields in other settings that are related to
// clustering, e.g. MeshExpansionPorts on Ingress gateway and DNS search
// search suffixes for Proxy.
func populateClusterValues(in *v2.ControlPlaneSpec, namespace string, values map[string]interface{}) error {
	// Cluster settings
	cluster := in.Cluster
	if cluster == nil {
		cluster = &v2.ControlPlaneClusterConfig{}
	}

	clusterDomain := clusterDomainDefault
	if in.Proxy != nil && in.Proxy.Networking != nil && in.Proxy.Networking.ClusterDomain != "" {
		clusterDomain = in.Proxy.Networking.ClusterDomain
	}
	hasClusterName := len(cluster.Name) > 0
	hasNetworkName := len(cluster.Network) > 0
	if hasClusterName {
		if err := setHelmStringValue(values, "global.multiCluster.clusterName", cluster.Name); err != nil {
			return err
		}
	}
	if hasNetworkName {
		if err := setHelmStringValue(values, "global.network", cluster.Network); err != nil {
			return err
		}
	}

	multiClusterEnabled := false
	multiClusterOverrides := v1.NewHelmValues(make(map[string]interface{}))
	if cluster.MultiCluster != nil {
		// multi-cluster
		if cluster.MultiCluster.Enabled != nil {
			// meshExpansion is always enabled for multi-cluster
			multiClusterEnabled = *cluster.MultiCluster.Enabled
			if err := setHelmBoolValue(values, "global.multiCluster.enabled", *cluster.MultiCluster.Enabled); err != nil {
				return err
			}
		} else {
			// default to false
			if err := multiClusterOverrides.SetField("multiClusterEnabled", nil); err != nil {
				return err
			}
			if err := setHelmBoolValue(values, "global.multiCluster.enabled", false); err != nil {
				return err
			}
		}
		if hasClusterName && hasNetworkName {
			// Configure local mesh network, if not defined
			if cluster.MultiCluster.MeshNetworks == nil {
				cluster.MultiCluster.MeshNetworks = make(map[string]v2.MeshNetworkConfig)
			}
			if _, ok := cluster.MultiCluster.MeshNetworks[cluster.Network]; !ok {
				// XXX: do we need to make sure ingress gateways is configured and includes port 443?
				cluster.MultiCluster.MeshNetworks[cluster.Network] = v2.MeshNetworkConfig{
					Endpoints: []v2.MeshEndpointConfig{
						{
							FromRegistry: cluster.Name,
						},
					},
					Gateways: []v2.MeshGatewayConfig{
						{
							// XXX: should we check to see if ilb gateway is being used instead?
							// XXX: this should be the gateway namespace or the control plane namespace
							Service: getLocalNetworkService("istio-ingressgateway", namespace, clusterDomain),
							Port:    443,
						},
					},
				}
				if err := setHelmStringValue(values, "global.multiCluster.addedLocalNetwork", cluster.Network); err != nil {
					return err
				}
			}

			if meshNetworksValue, err := toValues(cluster.MultiCluster.MeshNetworks); err == nil {
				if len(meshNetworksValue) > 0 {
					if err := overwriteHelmValues(values, meshNetworksValue, strings.Split("global.meshNetworks", ".")...); err != nil {
						return err
					}
				}
			} else {
				return err
			}
		}

		if multiClusterEnabled {
			// if multicluster is enabled, so is mesh expansion, regardless of user settings
			if in.Cluster.MeshExpansion == nil {
				in.Cluster.MeshExpansion = &v2.MeshExpansionConfig{
					Enablement: v2.Enablement{
						Enabled: &multiClusterEnabled,
					},
				}
				if err := multiClusterOverrides.SetField("expansionEnabled", nil); err != nil {
					return err
				}
			} else if in.Cluster.MeshExpansion.Enabled == nil {
				in.Cluster.MeshExpansion.Enabled = &multiClusterEnabled
				if err := multiClusterOverrides.SetField("expansionEnabled", nil); err != nil {
					return err
				}
			} else if !*in.Cluster.MeshExpansion.Enabled {
				in.Cluster.MeshExpansion.Enabled = &multiClusterEnabled
				if err := multiClusterOverrides.SetField("expansionEnabled", false); err != nil {
					return err
				}
			}
			// XXX: ingress and egress gateways must be configured if multicluster is enabled
			if in.Gateways == nil {
				in.Gateways = &v2.GatewaysConfig{}
			}
			if in.Gateways.ClusterEgress == nil {
				if err := multiClusterOverrides.SetField("egressEnabled", nil); err != nil {
					return err
				}
				enabled := true
				in.Gateways.ClusterEgress = &v2.EgressGatewayConfig{
					GatewayConfig: v2.GatewayConfig{
						Enablement: v2.Enablement{
							Enabled: &enabled,
						},
					},
				}
			} else if in.Gateways.ClusterEgress.Enabled == nil {
				if err := multiClusterOverrides.SetField("egressEnabled", nil); err != nil {
					return err
				}
				enabled := true
				in.Gateways.ClusterEgress.Enabled = &enabled
			} else if !*in.Gateways.ClusterEgress.Enabled {
				if err := multiClusterOverrides.SetField("egressEnabled", *in.Gateways.ClusterEgress.Enabled); err != nil {
					return err
				}
				*in.Gateways.ClusterEgress.Enabled = true
			}
			if in.Gateways.Enabled == nil {
				if err := multiClusterOverrides.SetField("gatewaysEnabled", nil); err != nil {
					return err
				}
				enabled := true
				in.Gateways.Enabled = &enabled
			} else if !*in.Gateways.Enabled {
				if err := multiClusterOverrides.SetField("gatewaysEnabled", *in.Gateways.Enabled); err != nil {
					return err
				}
				*in.Gateways.Enabled = true
			}

			foundExternal := false
			for _, network := range in.Gateways.ClusterEgress.RequestedNetworkView {
				if network == "external" {
					foundExternal = true
					break
				}
			}
			if !foundExternal {
				if err := multiClusterOverrides.SetField("addedExternal", true); err != nil {
					return err
				}
				in.Gateways.ClusterEgress.RequestedNetworkView = append(in.Gateways.ClusterEgress.RequestedNetworkView, "external")
			}
		}
	} else {
		// multi cluster disabled by default
		if err := multiClusterOverrides.SetField("multiClusterEnabled", nil); err != nil {
			return err
		}
		if err := setHelmBoolValue(values, "global.multiCluster.enabled", false); err != nil {
			return err
		}
	}

	if cluster.MeshExpansion != nil {
		if cluster.MeshExpansion.Enabled != nil {
			if err := setHelmBoolValue(values, "global.meshExpansion.enabled", *cluster.MeshExpansion.Enabled); err != nil {
				return err
			}
		} else {
			// mesh expansion disabled by default
			if err := multiClusterOverrides.SetField("expansionEnabled", nil); err != nil {
				return err
			}
			if err := setHelmBoolValue(values, "global.meshExpansion.enabled", false); err != nil {
				return err
			}
		}

		version, err := versions.ParseVersion(in.Version)
		if err != nil {
			return err
		}

		expansionPorts := version.Strategy().GetExpansionPorts()
		if cluster.MeshExpansion.ILBGateway == nil ||
			cluster.MeshExpansion.ILBGateway.Enabled == nil || !*cluster.MeshExpansion.ILBGateway.Enabled {
			if in.Gateways.ClusterIngress == nil {
				if err := multiClusterOverrides.SetField("ingressEnabled", nil); err != nil {
					return err
				}
				if err := multiClusterOverrides.SetField("k8sIngressEnabled", nil); err != nil {
					return err
				}
				enabled := true
				in.Gateways.ClusterIngress = &v2.ClusterIngressGatewayConfig{
					IngressGatewayConfig: v2.IngressGatewayConfig{
						GatewayConfig: v2.GatewayConfig{
							Enablement: v2.Enablement{
								Enabled: &enabled,
							},
						},
					},
					IngressEnabled: &enabled,
				}
			} else {
				if in.Gateways.ClusterIngress.Enabled == nil || !*in.Gateways.ClusterIngress.Enabled {
					enabled := true
					in.Gateways.ClusterIngress.Enabled = &enabled
					if err := multiClusterOverrides.SetField("ingressEnabled", *in.Gateways.ClusterIngress.Enabled); err != nil {
						return err
					}
				}
				if in.Gateways.ClusterIngress.IngressEnabled == nil || !*in.Gateways.ClusterIngress.IngressEnabled {
					k8sIngressEnabled := true
					in.Gateways.ClusterIngress.IngressEnabled = &k8sIngressEnabled
					if err := multiClusterOverrides.SetField("k8sIngressEnabled", *in.Gateways.ClusterIngress.Enabled); err != nil {
						return err
					}
				}
			}
			addExpansionPorts(&in.Gateways.ClusterIngress.MeshExpansionPorts, expansionPorts)
			if cluster.MeshExpansion.ILBGateway == nil {
				if err := multiClusterOverrides.SetField("ilbEnabled", nil); err != nil {
					return err
				}
				disabled := false
				cluster.MeshExpansion.ILBGateway = &v2.GatewayConfig{
					Enablement: v2.Enablement{
						Enabled: &disabled,
					},
				}
			} else {
				if cluster.MeshExpansion.ILBGateway.Enabled == nil {
					if err := multiClusterOverrides.SetField("ilbEnabled", nil); err != nil {
						return err
					}
				} else if *cluster.MeshExpansion.ILBGateway.Enabled {
					if err := multiClusterOverrides.SetField("ilbEnabled", *cluster.MeshExpansion.ILBGateway.Enabled); err != nil {
						return err
					}
				}
			}
			if err := setHelmBoolValue(values, "global.meshExpansion.useILB", false); err != nil {
				return err
			}
		} else {
			if err := setHelmBoolValue(values, "global.meshExpansion.useILB", true); err != nil {
				return err
			}
			addExpansionPorts(&cluster.MeshExpansion.ILBGateway.Service.Ports, expansionPorts)
		}
		// serialize ilb gateway settings
		if cluster.MeshExpansion.ILBGateway != nil {
			if ilbGatewayValues, err := gatewayConfigToValues(cluster.MeshExpansion.ILBGateway); err == nil {
				if err := overwriteHelmValues(values, ilbGatewayValues, strings.Split("gateways.istio-ilbgateway", ".")...); err != nil {
					return err
				}
			} else {
				return err
			}
		}
	} else {
		// mesh expansion disabled by default
		if err := multiClusterOverrides.SetField("expansionEnabled", nil); err != nil {
			return err
		}
		if err := setHelmBoolValue(values, "global.meshExpansion.enabled", false); err != nil {
			return err
		}
		if err := setHelmBoolValue(values, "global.meshExpansion.useILB", false); err != nil {
			return err
		}
	}

	if multiClusterEnabled {
		// Configure DNS search suffixes for "global"
		globalIndex := -1
		deploymentMetadataIndex := -1
		addedSearchSuffixes := make([]interface{}, 0, 2)
		dns := &v2.ProxyDNSConfig{}
		if in.Proxy == nil {
			in.Proxy = &v2.ProxyConfig{}
		} else if in.Proxy.Networking != nil && in.Proxy.Networking.DNS != nil {
			dns = in.Proxy.Networking.DNS
			for index, ss := range dns.SearchSuffixes {
				if ss == searchSuffixGlobal {
					globalIndex = index
				} else if strings.Index(ss, ".DeploymentMeta.Namespace") > 0 { // greater than works here because the template must be bracketed with {{ }}
					deploymentMetadataIndex = index
				}
			}
		}
		if deploymentMetadataIndex < 0 {
			namespaceSuffix := fmt.Sprintf(searchSuffixNamespaceGlobalTemplate, namespace)
			addedSearchSuffixes = append(addedSearchSuffixes, namespaceSuffix)
			if globalIndex < 0 {
				dns.SearchSuffixes = append([]string{namespaceSuffix}, dns.SearchSuffixes...)
			} else {
				dns.SearchSuffixes = append(append(dns.SearchSuffixes[:globalIndex+1], namespaceSuffix), dns.SearchSuffixes[globalIndex+1:]...)
			}
		}
		if globalIndex < 0 {
			addedSearchSuffixes = append(addedSearchSuffixes, searchSuffixGlobal)
			dns.SearchSuffixes = append([]string{searchSuffixGlobal}, dns.SearchSuffixes...)
		}
		if len(addedSearchSuffixes) > 0 {
			if in.Proxy.Networking == nil {
				in.Proxy.Networking = &v2.ProxyNetworkingConfig{}
			}
			if in.Proxy.Networking.DNS == nil {
				in.Proxy.Networking.DNS = dns
			}
			if err := setHelmValue(values, "global.multiCluster.addedSearchSuffixes", addedSearchSuffixes); err != nil {
				return err
			}
		}
	}

	if len(multiClusterOverrides.GetContent()) > 0 {
		if err := overwriteHelmValues(values, multiClusterOverrides.GetContent(), strings.Split("global.multiCluster.multiClusterOverrides", ".")...); err != nil {
			return err
		}
	}

	return nil
}

func addExpansionPorts(in *[]corev1.ServicePort, ports []corev1.ServicePort) {
	portCount := len(*in)
PortsLoop:
	for _, port := range ports {
		for index := 0; index < portCount; index++ {
			if port.Port == (*in)[index].Port {
				continue PortsLoop
			}
			*in = append(*in, port)
		}
	}
}

func getLocalNetworkService(gatewayService, namespace, clusterDomain string) string {
	return fmt.Sprintf("%s.%s.svc.%s", gatewayService, namespace, clusterDomain)
}

func populateClusterConfig(in *v1.HelmValues, out *v2.ControlPlaneSpec) error {
	clusterConfig := &v2.ControlPlaneClusterConfig{}
	setClusterConfig := false

	if clusterName, ok, err := in.GetAndRemoveString("global.multiCluster.clusterName"); ok {
		clusterConfig.Name = clusterName
		setClusterConfig = true
	} else if err != nil {
		return err
	}
	if network, ok, err := in.GetAndRemoveString("global.network"); ok {
		clusterConfig.Network = network
		setClusterConfig = true
	} else if err != nil {
		return err
	}
	if setClusterConfig {
		out.Cluster = clusterConfig
	}
	return nil
}
