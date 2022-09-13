/*
Copyright 2017 The Kubernetes Authors.

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

package source

import (
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/external-dns/endpoint"
)

func TestDockerEngineEndpointResults(t *testing.T) {
	createFakeContainer := func(netName, ipAddr string, labels map[string]string) types.Container {
		return types.Container{
			Labels: labels,
			NetworkSettings: &types.SummaryNetworkSettings{
				Networks: map[string]*network.EndpointSettings{
					netName: {
						Gateway:   "172.17.0.1",
						IPAddress: ipAddr,
						// TODO: need to support IPv6?
					},
				},
			},
		}
	}

	labels1 := map[string]string{
		"maintainer": "Author Name <author@example.local>",
		"external-dns.alpha.kubernetes.io/hostname": "gateway.example.local",
	}
	labels2 := map[string]string{
		"external-dns.alpha.kubernetes.io/hostname": "whoami.example.local",
		"external-dns.alpha.kubernetes.io/target":   "gateway.example.local",
		"external-dns.alpha.kubernetes.io/ttl":      "1700",
		"com.docker.compose.service":                "whoami",
	}
	labels3 := map[string]string{
		"external-dns.alpha.kubernetes.io/hostname": "whoami-beta.example.local",
		"external-dns.alpha.kubernetes.io/ttl":      "1500",
		"com.docker.compose.service":                "whoami2",
	}

	labels4 := map[string]string{
		"com.docker.swarm.node.id":      "",
		"com.docker.swarm.service.id":   "jqz4hrd4c51w164rpvi2fdbqz",
		"com.docker.swarm.service.name": "whoami-swarm",
		"com.docker.swarm.task":         "",
		"com.docker.swarm.task.id":      "6kg7xevr9t0aikjmqmuo7v4w0",
		"com.docker.swarm.task.name":    "whoami-swarm.1.6kg7xevr9t0aikjmqmuo7v4w0",
	}
	labels5 := map[string]string{
		"external-dns.alpha.kubernetes.io/hostname": "whoami-swarm.example.local",
		// "external-dns.alpha.kubernetes.io/ttl":      "1500",
		"com.docker.swarm.node.id":      "",
		"com.docker.swarm.service.id":   "2xbz9m0akcoggmuna2dajn334",
		"com.docker.swarm.service.name": "whoami-swarm2",
		"com.docker.swarm.task":         "",
		"com.docker.swarm.task.id":      "xtxtix54ryv067h0k787e1mo4",
		"com.docker.swarm.task.name":    "whoami-swarm2.1.xtxtix54ryv067h0k787e1mo4",
	}
	swarmServicePorts := []swarm.PortConfig{
		{Protocol: swarm.PortConfigProtocolTCP, TargetPort: 5000, PublishedPort: 5000, PublishMode: swarm.PortConfigPublishModeIngress},
	}
	swarmServices := map[string]swarm.Service{
		"jqz4hrd4c51w164rpvi2fdbqz": {
			Endpoint: swarm.Endpoint{
				Spec:  swarm.EndpointSpec{Mode: swarm.ResolutionModeVIP, Ports: swarmServicePorts},
				Ports: swarmServicePorts,
				VirtualIPs: []swarm.EndpointVirtualIP{
					{NetworkID: "l4iiksjxu99m6ebxbgwtosatq", Addr: "10.0.0.3/24"},
				},
			},
		},
		"2xbz9m0akcoggmuna2dajn334": {
			Endpoint: swarm.Endpoint{
				Spec:  swarm.EndpointSpec{Mode: swarm.ResolutionModeVIP, Ports: swarmServicePorts},
				Ports: swarmServicePorts,
				VirtualIPs: []swarm.EndpointVirtualIP{
					{NetworkID: "l4iiksjxu99m6ebxbgwtosatq", Addr: "10.0.0.6/24"},
				},
			},
		},
	}

	fakeContainers := []types.Container{
		createFakeContainer("bridge", "172.17.0.2", labels1),

		// ns_default
		// ns_netname-dev
		createFakeContainer("ns_default", "172.18.0.2", labels2),
		createFakeContainer("ns_default", "172.18.0.3", labels2),
		createFakeContainer("ns_default", "172.18.0.4", labels2),

		createFakeContainer("ns_default", "172.19.0.2", labels3),
		createFakeContainer("ns_default", "172.19.0.3", labels3),
		createFakeContainer("ns_default", "172.19.0.4", labels3),

		// swarm test
		createFakeContainer("ingress", "10.0.0.4", labels4),
		createFakeContainer("ingress", "10.0.0.5", labels4),

		createFakeContainer("ingress", "10.0.0.6", labels5),
		createFakeContainer("ingress", "10.0.0.7", labels5),
	}

	endpoints, err := (&dockerEngineSource{isSwarmMode: false}).
		endpointsFromContainers(fakeContainers, swarmServices)
	require.NoError(t, err)
	expected := []*endpoint.Endpoint{
		{DNSName: "gateway.example.local", Targets: endpoint.Targets{"172.17.0.2"}, RecordType: "A", SetIdentifier: "", RecordTTL: 0, Labels: endpoint.Labels{}, ProviderSpecific: endpoint.ProviderSpecific{}},
		{DNSName: "whoami.example.local", Targets: endpoint.Targets{"gateway.example.local"}, RecordType: "CNAME", SetIdentifier: "", RecordTTL: 1700, Labels: endpoint.Labels{}, ProviderSpecific: endpoint.ProviderSpecific{}},
		{DNSName: "whoami-beta.example.local", Targets: endpoint.Targets{"172.19.0.2", "172.19.0.3", "172.19.0.4"}, RecordType: "A", SetIdentifier: "", RecordTTL: 1500, Labels: endpoint.Labels{}, ProviderSpecific: endpoint.ProviderSpecific{}},
	}
	// spew.Dump(endpoints)
	require.Equal(t, expected, endpoints)

	// swarm test
	endpoints, err = (&dockerEngineSource{isSwarmMode: true}).
		endpointsFromContainers(fakeContainers, swarmServices)
	// spew.Dump(endpoints)
	require.NoError(t, err)
	expected = []*endpoint.Endpoint{
		{DNSName: "gateway.example.local", Targets: endpoint.Targets{"172.17.0.2"}, RecordType: "A", SetIdentifier: "", RecordTTL: 0, Labels: endpoint.Labels{}, ProviderSpecific: endpoint.ProviderSpecific{}},
		{DNSName: "whoami.example.local", Targets: endpoint.Targets{"gateway.example.local"}, RecordType: "CNAME", SetIdentifier: "", RecordTTL: 1700, Labels: endpoint.Labels{}, ProviderSpecific: endpoint.ProviderSpecific{}},
		{DNSName: "whoami-beta.example.local", Targets: endpoint.Targets{"172.19.0.2", "172.19.0.3", "172.19.0.4"}, RecordType: "A", SetIdentifier: "", RecordTTL: 1500, Labels: endpoint.Labels{}, ProviderSpecific: endpoint.ProviderSpecific{}},

		{DNSName: "whoami-swarm.example.local", Targets: endpoint.Targets{"10.0.0.6", "10.0.0.7"}, RecordType: "A", SetIdentifier: "", RecordTTL: 0, Labels: endpoint.Labels{}, ProviderSpecific: endpoint.ProviderSpecific{}},
	}
	require.Equal(t, expected, endpoints)
}
