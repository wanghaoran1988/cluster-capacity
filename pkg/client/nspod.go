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

package client

import (
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/resource"
	clientset "k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	"k8s.io/kubernetes/pkg/labels"
)

// Retrieve a namespace pod constructed from the namespace limitations.
// Limitations cover pod resource limits and node selector if available
func RetrieveNamespacePod(client clientset.Interface, namespace string) (*api.Pod, error) {
	ns, err := client.Core().Namespaces().Get(namespace)
	if err != nil {
		return nil, fmt.Errorf("Namespace %v not found: %v", namespace, err)
	}

	// Iterate through all limit ranges and pick the minimum of all related to pod constraints
	limits, err := client.Core().LimitRanges(namespace).List(api.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("Could not retrieve limit ranges for %v namespaces: %v", namespace, err)
	}

	resources := make(map[api.ResourceName]*resource.Quantity)

	// TODO(jchaloup): extend the list of considered resources with other types
	resources[api.ResourceMemory] = nil
	resources[api.ResourceCPU] = nil
	resources[api.ResourceNvidiaGPU] = nil

	for _, limit := range limits.Items {
		for _, item := range limit.Spec.Limits {
			if item.Type != api.LimitTypePod {
				continue
			}

			for resourceType := range resources {
				amount, ok := item.Max[resourceType]
				if !ok {
					continue
				}
				if resources[resourceType] == nil || resources[resourceType].Cmp(amount) == 1 {
					resources[resourceType] = &amount
				}
			}
		}
	}

	nonzero := false
	for _, quantity := range resources {
		if quantity == nil {
			continue
		}

		if !quantity.IsZero() {
			nonzero = true
			break
		}
	}

	if !nonzero {
		return nil, fmt.Errorf("No resource limit set for pod in %v namespace", namespace)
	}

	limitsResourceList := make(map[api.ResourceName]resource.Quantity)
	requestsResourceList := make(map[api.ResourceName]resource.Quantity)
	for key, val := range resources {
		if val == nil {
			continue
		}
		limitsResourceList[key] = *val
		requestsResourceList[key] = *val
	}

	namespacePod := api.Pod{
		ObjectMeta: api.ObjectMeta{
			Name:      "cluster-capacity-stub-container",
			Namespace: namespace,
		},
		Spec: api.PodSpec{
			Containers: []api.Container{
				api.Container{
					Name:            "cluster-capacity-stub-container",
					Image:           "gcr.io/google_containers/pause:2.0",
					ImagePullPolicy: api.PullAlways,
					Resources: api.ResourceRequirements{
						Limits:   limitsResourceList,
						Requests: requestsResourceList,
					},
				},
			},
			RestartPolicy: api.RestartPolicyOnFailure,
			DNSPolicy:     api.DNSDefault,
		},
	}

	annotations := ns.GetAnnotations()
	if key, ok := annotations["openshift.io/node-selector"]; ok {
		nodeSelector, err := labels.ConvertSelectorToLabelsMap(key)
		if err != nil {
			return nil, fmt.Errorf("Unable to parse openshift.io/node-selector in %v namespace: %v", key, err)
		}
		namespacePod.Spec.NodeSelector = nodeSelector
	}

	return &namespacePod, nil
}
