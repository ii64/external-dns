package source

import (
	"context"

	log "github.com/sirupsen/logrus"
	"sigs.k8s.io/external-dns/endpoint"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

const (
	// The annotation used for figuring out which controller is responsible
	dockerEngineNetworkAnnotationKey          = "external-dns/network"
	dockerEngineComposeServiceAnnotationKey   = "com.docker.compose.service"
	dockerEngineSwarmServiceNameAnnotationKey = "com.docker.swarm.service.name"
	dockerEngineSwarmServiceIDAnnotationKey   = "com.docker.swarm.service.id"
)

type dockerEngineSource struct {
	client      *client.Client
	isSwarmMode bool
	evHandlers  []func()
}

var _ Source = (*dockerEngineSource)(nil)

func NewDockerEngineSource() (Source, error) {
	var err error
	src := &dockerEngineSource{}
	src.client, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return src, nil
}

func (src *dockerEngineSource) AddEventHandler(ctx context.Context, handler func()) {
	src.evHandlers = append(src.evHandlers, handler)
}

func (src *dockerEngineSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	var swarmServices map[string]swarm.Service
	if src.isSwarmMode {
		services, err := src.client.ServiceList(ctx, types.ServiceListOptions{})
		if err != nil {
			log.Warn(err)
		} else {
			swarmServices = map[string]swarm.Service{}
			for _, service := range services {
				swarmServices[service.ID] = service
			}
		}
	}

	containers, err := src.client.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return nil, err
	}
	endpoints, err := src.endpointsFromContainers(containers, swarmServices)
	if err != nil {
		return nil, err
	}
	return endpoints, nil
}

func (src *dockerEngineSource) endpointsFromContainers(containers []types.Container, swarmServices map[string]swarm.Service) (endpoints []*endpoint.Endpoint, err error) {
	type pendingContainerState struct {
		ttl               endpoint.TTL
		hasFallbackTarget bool
		targets           endpoint.Targets
		providerSpecific  endpoint.ProviderSpecific
		setIdentifier     string
		labels            map[string]string
	}

	pendingGroup := map[string][]pendingContainerState{}
	keyGroupOrder := []string{}
	pendingSwarmServices := map[string][]pendingContainerState{}
	keySwarmOrder := []string{}

	for _, container := range containers {
		serviceName, hasServiceName := src.getContainerServiceName(container)
		swarmServiceID, hasSwarmServiceID := src.getContainerSwarmInfo(container)

		ttl, err := getTTLFromAnnotations(container.Labels)
		if err != nil {
			log.Warn(err)
			continue
		}

		targets := getTargetsFromTargetAnnotation(container.Labels)
		// fallback to network setting
		var fallbackTarget bool
		if len(targets) == 0 {
			fallbackTargets := src.getContainerNetworkTarget(container)
			fallbackTarget = len(fallbackTargets) > 0
			targets = append(targets, fallbackTargets...)
		}
		// skip, container has no target
		if len(targets) == 0 {
			continue
		}

		providerSpecific, setIdentifier := getProviderSpecificAnnotations(container.Labels)

		// pending endpoint creation if container has docker compose service name
		// or part of a swarm service
		if hasSwarmServiceID || hasServiceName {
			state := pendingContainerState{
				ttl:               ttl,
				targets:           targets,
				providerSpecific:  providerSpecific,
				hasFallbackTarget: fallbackTarget,
				setIdentifier:     setIdentifier,
				labels:            container.Labels,
			}
			switch {
			case hasSwarmServiceID && swarmServiceID != "":
				if src.isSwarmMode {
					if _, exist := pendingSwarmServices[swarmServiceID]; !exist {
						keySwarmOrder = append(keySwarmOrder, swarmServiceID)
					}
					pendingSwarmServices[swarmServiceID] = append(pendingSwarmServices[swarmServiceID], state)
				}
				continue
			case hasServiceName && serviceName != "":
				if _, exist := pendingGroup[serviceName]; !exist {
					keyGroupOrder = append(keyGroupOrder, serviceName)
				}
				pendingGroup[serviceName] = append(pendingGroup[serviceName], state)
				continue
			}
		}

		for _, hostname := range getHostnamesFromAnnotations(container.Labels) {
			endpoints = append(endpoints, endpointsForHostname(hostname, targets, ttl, providerSpecific, setIdentifier)...)
		}
	}

	// work on pending container service group
	for _, svcName := range keyGroupOrder {
		containerStates := pendingGroup[svcName]
		// get sample
		samp := containerStates[0]
		for _, cMember := range containerStates[1:] {
			// fallback target contains container ip
			// so we don't need to check for duplicates
			if cMember.hasFallbackTarget {
				samp.targets = append(samp.targets, cMember.targets...)
			}
		}
		for _, hostname := range getHostnamesFromAnnotations(samp.labels) {
			endpoints = append(endpoints, endpointsForHostname(
				hostname, samp.targets, samp.ttl, samp.providerSpecific, samp.setIdentifier)...)
		}
	}

	// work on pending swarm service group
	for _, swarmServiceId := range keySwarmOrder {
		containerStates := pendingSwarmServices[swarmServiceId]
		serviceDesc, exist := swarmServices[swarmServiceId]
		// skip if there's no reference about service description.
		if !exist {
			continue
		}
		// get sample
		samp := containerStates[0]
		for _, cMember := range containerStates[1:] {
			samp.targets = append(samp.targets, cMember.targets...)
		}
		targets := samp.targets
		// for _, ip := range serviceDesc.Endpoint.VirtualIPs {
		// 	_ = ip
		// }
		_ = serviceDesc
		for _, hostname := range getHostnamesFromAnnotations(samp.labels) {
			endpoints = append(endpoints, endpointsForHostname(
				hostname, targets, samp.ttl, samp.providerSpecific, samp.setIdentifier)...)
		}
	}

	return
}

func (src *dockerEngineSource) getContainerNetworkTarget(container types.Container) (targets endpoint.Targets) {
	netw := container.NetworkSettings
	preferredNetworkName, exist := getNetworkFromAnnotations(container.Labels)
	// fallback network name
	if !exist {
		preferredNetworkName = "bridge"
	}
	if netMap := netw.Networks; netMap != nil {
		var netSetting *network.EndpointSettings
		if exist {
			netSetting, exist = netMap[preferredNetworkName]
		} else {
			if len(netMap) == 1 {
				for _, netSetting = range netMap {
				}
				exist = true
			}
		}
		if exist && netSetting != nil {
			ip := netSetting.IPAddress
			targets = append(targets, ip)
		}
	}
	return
}

func (src *dockerEngineSource) getContainerServiceName(container types.Container) (string, bool) {
	serviceName, exist := container.Labels[dockerEngineComposeServiceAnnotationKey]
	if !exist {
		return "", false
	}
	return serviceName, true
}

func (src *dockerEngineSource) getContainerSwarmInfo(container types.Container) (string, bool) {
	id, exist := container.Labels[dockerEngineSwarmServiceIDAnnotationKey]
	if !exist {
		return "", false
	}
	return id, true
}

func getNetworkFromAnnotations(annotations map[string]string) (networkName string, exist bool) {
	networkName, exist = annotations[dockerEngineNetworkAnnotationKey]
	return
}
