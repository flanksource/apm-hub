package kubernetes

import (
	"fmt"
	"strings"

	"github.com/flanksource/apm-hub/api/logs"
	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	v1 "k8s.io/api/core/v1"
)

func NewKubernetesSearchBackend(client *Client, config *logs.KubernetesSearchBackendConfig) *KubernetesSearch {
	return &KubernetesSearch{
		client: client,
		config: config,
	}
}

type KubernetesSearch struct {
	client *Client
	config *logs.KubernetesSearchBackendConfig
}

func podNames(list *v1.PodList) []string {
	var names []string
	for _, pod := range list.Items {
		names = append(names, pod.Name)
	}
	return names
}

func (t *KubernetesSearch) MatchRoute(q *logs.SearchParams) (match bool, isAdditive bool) {
	return t.config.Routes.MatchRoute(q)
}

func (s *KubernetesSearch) Search(q *logs.SearchParams) (r logs.SearchResults, err error) {
	var resultLabels = make(map[string]string)
	namespace, name := s.GetNameNamespace(q)

	logger.Debugf("searching %s namespace=%s name=%s", q, namespace, name)
	var pods *v1.PodList
	switch {
	case strings.Contains(strings.ToLower(q.Type), "kubernetespod"):
		pods, err = s.client.GetPodsWithNameAndLabels(name, namespace, q.Labels)

	case strings.Contains(strings.ToLower(q.Type), "kubernetesnode"):
		pods, err = s.client.GetAllPodsForNode(q.Id, q.Labels)

	case strings.Contains(strings.ToLower(q.Type), "kubernetesdeployment"):
		pods, err = s.client.GetPodsForDeployment(name, namespace, q.Labels)
		resultLabels = map[string]string{
			"deployment": q.Id,
		}
	case strings.Contains(strings.ToLower(q.Type), "kubernetesservice"):
		pods, err = s.client.GetPodsForService(name, namespace, q.Labels)
		resultLabels = map[string]string{
			"service": q.Id,
		}
	}

	if err != nil {
		return r, fmt.Errorf("error fetching the pods for node %v: %v", q, err)
	}
	if pods == nil || len(pods.Items) == 0 {
		logger.Debugf("[%s] no pods found", q)
		return r, nil
	}
	logger.Tracef("[%s] searching in pods %s ", q, podNames(pods))
	r.Results = s.getLogResultsForPods(q, pods, collections.MergeMap(s.config.Labels, resultLabels))
	r.Total = len(r.Results)
	return r, nil
}

func (s *KubernetesSearch) getLogResultsForPods(q *logs.SearchParams, pods *v1.PodList, resultLabels map[string]string) []logs.Result {
	var results []logs.Result
	for _, pod := range pods.Items {
		podLogs, err := s.client.GetLogsForPod(q, pod)
		if err != nil {
			logger.Errorf("error fetching logs for pod: %v in namespace: %v, err: ", pod.Name, pod.Namespace, err)
			continue
		}
		for containerName, containerLogs := range podLogs {
			var labels = map[string]string{
				"pod":           pod.Name,
				"containerName": containerName,
				"nodeName":      pod.Spec.NodeName,
				"namespace":     pod.Namespace,
			}
			for k, v := range resultLabels {
				labels[k] = v
			}
			for _, line := range containerLogs {
				line.Labels = labels
				line = line.Process()
				if line.Message != "" {
					results = append(results, line)
				}
			}
		}
	}
	return results
}

func (s *KubernetesSearch) GetNameNamespace(q *logs.SearchParams) (namespace, name string) {
	if strings.Contains(q.Id, "/") {
		// namespace is provided as a prefix in the ID
		namespaceName := strings.Split(q.Id, "/")
		if len(namespaceName) < 2 {
			logger.Errorf("expected id in format <namespace>/<name>")
			return "", ""
		}
		return namespaceName[0], namespaceName[1]
	}
	// namespace is provided in the labels. if no label is there we just return the empty string which extends the search to all namespaces
	namespace = q.Labels["namespace"]
	// deleting namespace label from the map so it doesn't filter out the result based on the namespace label
	delete(q.Labels, "namespace")
	return namespace, q.Id
}
