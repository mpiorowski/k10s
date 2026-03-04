package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

type LogEntry struct {
	PodName    string
	Message    string
	RawMessage string
	IsError    bool
	Timestamp  time.Time
}

// ClusterStatus holds the aggregated data for a single cluster
type ClusterStatus struct {
	ContextName    string
	Version        string
	NodesReady     int
	NodesTotal     int
	PodsRunning    int
	PodsFailed     int
	PodsPending    int
	PodsTotal      int
	CpuUsage       int64 // in millicores
	CpuCapacity    int64 // in millicores
	MemUsage       int64 // in bytes
	MemCapacity    int64 // in bytes
	RecentLogs     []LogEntry
	Error          error
	LastUpdate     time.Time
}

// ClientManager handles connections to multiple Kubernetes clusters
type ClientManager struct {
	clients        map[string]*kubernetes.Clientset
	metricsClients map[string]*metricsv.Clientset
}

// NewClientManager creates clients for the specified contexts
func NewClientManager(kubeconfigPath string, contexts []string) (*ClientManager, error) {
	cm := &ClientManager{
		clients:        make(map[string]*kubernetes.Clientset),
		metricsClients: make(map[string]*metricsv.Clientset),
	}

	for _, ctxName := range contexts {
		// Create a config overriding the current context
		loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
		configOverrides := &clientcmd.ConfigOverrides{CurrentContext: ctxName}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

		restConfig, err := kubeConfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("could not create rest config for context %s: %w", ctxName, err)
		}

		// Lower timeouts so a dead cluster doesn't block forever
		restConfig.Timeout = 5 * time.Second

		clientset, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			return nil, fmt.Errorf("could not create clientset for context %s: %w", ctxName, err)
		}

		mClientset, err := metricsv.NewForConfig(restConfig)
		if err == nil {
			cm.metricsClients[ctxName] = mClientset
		}

		cm.clients[ctxName] = clientset
	}

	return cm, nil
}

// GetDeployments returns deployment names across given contexts
func (cm *ClientManager) GetDeployments(ctx context.Context, contexts []string) ([]string, error) {
	var res []string
	for _, ctxName := range contexts {
		clientset, ok := cm.clients[ctxName]
		if !ok {
			continue
		}
		deps, err := clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
		if err == nil {
			for _, d := range deps.Items {
				res = append(res, fmt.Sprintf("[%s] %s/%s", ctxName, d.Namespace, d.Name))
			}
		}
	}
	return res, nil
}

// FetchStatus concurrently queries the API server for cluster health metrics
func (cm *ClientManager) FetchStatus(ctx context.Context, ctxName string, logFilters []string, jsonKeys []string) ClusterStatus {
	status := ClusterStatus{
		ContextName: ctxName,
		LastUpdate:  time.Now(),
	}

	clientset, ok := cm.clients[ctxName]
	if !ok {
		status.Error = fmt.Errorf("context not found")
		return status
	}

	// 1. Fetch Server Version
	version, err := clientset.Discovery().ServerVersion()
	if err != nil {
		status.Error = fmt.Errorf("API unreached: %v", err)
		return status
	}
	status.Version = version.String()

	// 2. Fetch Nodes
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err == nil {
		status.NodesTotal = len(nodes.Items)
		for _, node := range nodes.Items {
			for _, cond := range node.Status.Conditions {
				if cond.Type == "Ready" && cond.Status == "True" {
					status.NodesReady++
					break
				}
			}
			
			// Capacities
			cpuQty := node.Status.Capacity.Cpu()
			memQty := node.Status.Capacity.Memory()
			if cpuQty != nil {
				status.CpuCapacity += cpuQty.MilliValue()
			}
			if memQty != nil {
				status.MemCapacity += memQty.Value()
			}
		}
	} else {
		// Log the error but continue trying to get other data
		status.Error = fmt.Errorf("nodes failed: %v", err)
	}

	// 3. Fetch Pods across all namespaces
	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err == nil {
		status.PodsTotal = len(pods.Items)
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case "Running":
				status.PodsRunning++
			case "Pending":
				status.PodsPending++
			case "Failed":
				status.PodsFailed++
			}
			
			// Check for CrashLoopBackOff or other waiting container issues
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
					// We can consider this as failed/problematic for the high-level view
					status.PodsFailed++
				}
			}
		}
	} else if status.Error == nil { // Don't overwrite node error if it exists
		status.Error = fmt.Errorf("pods failed: %v", err)
	}

	// 4. Fetch Metrics
	if mClientset, ok := cm.metricsClients[ctxName]; ok {
		nodeMetrics, err := mClientset.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
		if err == nil {
			for _, nm := range nodeMetrics.Items {
				cpuUsage := nm.Usage.Cpu()
				memUsage := nm.Usage.Memory()
				if cpuUsage != nil {
					status.CpuUsage += cpuUsage.MilliValue()
				}
				if memUsage != nil {
					status.MemUsage += memUsage.Value()
				}
			}
		}
	}

	// 5. Fetch limited recent logs based on deployment filters
	if pods != nil && len(logFilters) > 0 {
		tailLines := int64(40) // Increased to fill screen
		var logs []LogEntry
		logsCount := 0
		
		for _, pod := range pods.Items {
			matched := false
			podStr := fmt.Sprintf("[%s] %s/%s", ctxName, pod.Namespace, pod.Name)
			for _, filter := range logFilters {
				// Exact match (bare pods) OR prefix with a dash (Deployments/DaemonSets/StatefulSets)
				// This prevents selecting "payment-service" from also matching "payment-service-worker"
				if podStr == filter || strings.HasPrefix(podStr, filter+"-") {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}

			if logsCount >= 20 {
				break // Limit to avoid blocking API too much
			}
			
			// Only fetch logs if running or failed
			if pod.Status.Phase == "Running" || pod.Status.Phase == "Failed" {
				req := clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
					TailLines:  &tailLines,
					Timestamps: true,
				})
				podLogs, err := req.Stream(ctx)
				if err == nil {
					buf := new(bytes.Buffer)
					_, _ = io.Copy(buf, podLogs)
					podLogs.Close()

					lines := strings.Split(buf.String(), "\n")
					for _, line := range lines {
						line = strings.TrimSpace(line)
						if line == "" {
							continue
						}

						var logTime time.Time
						parts := strings.SplitN(line, " ", 2)
						if len(parts) == 2 {
							if t, err := time.Parse(time.RFC3339Nano, parts[0]); err == nil {
								logTime = t
								line = parts[1]
							}
						}

						isErr := strings.Contains(strings.ToLower(line), "error") ||
								 strings.Contains(strings.ToLower(line), "err") ||
								 strings.Contains(strings.ToLower(line), "fail") ||
								 strings.Contains(strings.ToLower(line), "exception")

						// Naive JSON parsing for structured logs
						displayMsg := line
						if len(jsonKeys) > 0 && strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}") {
							var jsonLog map[string]interface{}
							if err := json.Unmarshal([]byte(line), &jsonLog); err == nil {
								var jsonParts []string
								for _, k := range jsonKeys {
									if val, ok := jsonLog[k]; ok {
										// format as key=value
										valStr := fmt.Sprintf("%v", val)
										// if val has spaces, quote it
										if strings.Contains(valStr, " ") {
											jsonParts = append(jsonParts, fmt.Sprintf("%s=\"%s\"", k, valStr))
										} else {
											jsonParts = append(jsonParts, fmt.Sprintf("%s=%s", k, valStr))
										}

										// Sneakily check level for error highlighting
										if strings.ToLower(k) == "level" && (strings.ToLower(valStr) == "error" || strings.ToLower(valStr) == "fatal") {
											isErr = true
										}
									}
								}
								if len(jsonParts) > 0 {
									displayMsg = strings.Join(jsonParts, " ")
								}
							}
						}

						logs = append(logs, LogEntry{
							PodName:    pod.Name,
							Message:    displayMsg,
							RawMessage: line,
							IsError:    isErr,
							Timestamp:  logTime,
						})
					}
					logsCount++
				}
			}
		}

		sort.Slice(logs, func(i, j int) bool {
			return logs[i].Timestamp.Before(logs[j].Timestamp)
		})

		// Keep only the last 100 entries
		if len(logs) > 100 {
			logs = logs[len(logs)-100:]
		}
		status.RecentLogs = logs
	}

	return status
}

// GetAllContexts reads the kubeconfig and returns a list of all available context names
func GetAllContexts(kubeconfigPath string) ([]string, error) {
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})
	rawConfig, err := kubeConfig.RawConfig()
	if err != nil {
		return nil, err
	}
	var contexts []string
	for name := range rawConfig.Contexts {
		contexts = append(contexts, name)
	}
	return contexts, nil
}
