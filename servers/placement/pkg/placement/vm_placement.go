package placement

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"sigs.k8s.io/yaml"
	kubevirtv1 "kubevirt.io/api/core/v1"
)

// VMPlacementParams represents parameters for generating placement from VM
type VMPlacementParams struct {
	VMYAML    string `json:"vmYAML"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// GeneratePlacementFromVM generates a Placement YAML based on VirtualMachine requirements
func (h *Handler) GeneratePlacementFromVM(ctx context.Context, params VMPlacementParams) (string, error) {
	if params.VMYAML == "" {
		return "", fmt.Errorf("VM YAML is required")
	}
	if params.Name == "" {
		return "", fmt.Errorf("placement name is required")
	}
	if params.Namespace == "" {
		return "", fmt.Errorf("placement namespace is required")
	}

	// Parse the VM YAML using official kubevirt API
	var vm kubevirtv1.VirtualMachine
	if err := yaml.Unmarshal([]byte(params.VMYAML), &vm); err != nil {
		return "", fmt.Errorf("failed to parse VM YAML: %v", err)
	}

	// Extract requirements from VM spec
	requirements := extractVMRequirements(&vm)

	// Detect AddOnPlacementScore resources for intelligent prioritizer selection
	scoreInfo := h.detectAddOnPlacementScores(ctx)

	// Build the Placement object
	placement := &clusterv1beta1.Placement{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cluster.open-cluster-management.io/v1beta1",
			Kind:       "Placement",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      params.Name,
			Namespace: params.Namespace,
		},
		Spec: buildPlacementSpecFromVM(requirements, scoreInfo),
	}

	// Convert to YAML
	yamlBytes, err := yaml.Marshal(placement)
	if err != nil {
		return "", fmt.Errorf("failed to marshal placement to YAML: %v", err)
	}

	return string(yamlBytes), nil
}

// VMRequirements represents extracted requirements from a VirtualMachine
type VMRequirements struct {
	CPUCores         uint32
	MemoryGi         int64
	NodeSelectors    map[string]string
	HasAntiAffinity  bool
	AntiAffinityKey  string // e.g., "kubernetes.io/hostname" means spread across nodes
	NumberOfClusters *int32 // Derived from anti-affinity requirements
}

// extractVMRequirements extracts placement requirements from a VirtualMachine spec
func extractVMRequirements(vm *kubevirtv1.VirtualMachine) *VMRequirements {
	req := &VMRequirements{
		NodeSelectors: make(map[string]string),
	}

	if vm.Spec.Template == nil {
		return req
	}

	spec := &vm.Spec.Template.Spec

	// Extract CPU cores
	if spec.Domain.CPU != nil && spec.Domain.CPU.Cores != 0 {
		req.CPUCores = spec.Domain.CPU.Cores
	}

	// Extract memory from domain resources
	if spec.Domain.Resources.Requests != nil {
		if memory, ok := spec.Domain.Resources.Requests["memory"]; ok {
			// Parse memory quantity (e.g., "4Gi" -> 4)
			memStr := memory.String()
			memStr = strings.ToLower(memStr)
			memStr = strings.TrimSuffix(memStr, "gi")
			memStr = strings.TrimSuffix(memStr, "g")
			if val, err := strconv.ParseInt(memStr, 10, 64); err == nil {
				req.MemoryGi = val
			}
		}
	}

	// Extract node selectors
	if spec.NodeSelector != nil {
		for k, v := range spec.NodeSelector {
			req.NodeSelectors[k] = v
		}
	}

	// Check for anti-affinity (spread across nodes/clusters)
	if spec.Affinity != nil && spec.Affinity.PodAntiAffinity != nil {
		rules := spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution
		if len(rules) > 0 {
			req.HasAntiAffinity = true
			req.AntiAffinityKey = rules[0].TopologyKey

			// If anti-affinity is for hostname, suggest selecting 1 cluster
			// (spreading across nodes within a single cluster)
			if req.AntiAffinityKey == "kubernetes.io/hostname" {
				one := int32(1)
				req.NumberOfClusters = &one
			}
		}
	}

	return req
}

// buildPlacementSpecFromVM builds a PlacementSpec from VM requirements
func buildPlacementSpecFromVM(req *VMRequirements, scoreInfo *AddOnScoreInfo) clusterv1beta1.PlacementSpec {
	spec := clusterv1beta1.PlacementSpec{
		NumberOfClusters: req.NumberOfClusters,
	}

	predicate := clusterv1beta1.ClusterPredicate{
		RequiredClusterSelector: clusterv1beta1.ClusterSelector{},
	}

	// Build label selector from nodeSelectors
	var labelReqs []metav1.LabelSelectorRequirement
	for key, value := range req.NodeSelectors {
		// Map common nodeSelector labels to cluster labels
		clusterLabel := mapNodeSelectorToClusterLabel(key, value)
		if clusterLabel != nil {
			labelReqs = append(labelReqs, *clusterLabel)
		}
	}

	if len(labelReqs) > 0 {
		predicate.RequiredClusterSelector.LabelSelector = metav1.LabelSelector{
			MatchExpressions: labelReqs,
		}
	}

	// Build CEL expressions for resource capacity requirements
	var celExprs []string

	// CPU capacity check
	if req.CPUCores > 0 {
		celExprs = append(celExprs,
			fmt.Sprintf(`has(managedCluster.status.capacity.cpu) && int(managedCluster.status.capacity.cpu) >= %d`, req.CPUCores))
	}

	// Memory capacity check (convert to Gi for comparison)
	if req.MemoryGi > 0 {
		celExprs = append(celExprs,
			fmt.Sprintf(`has(managedCluster.status.capacity.memory) && int(managedCluster.status.capacity.memory.replace("Gi", "").replace("Mi", "").replace("Ki", "")) >= %d`, req.MemoryGi))
	}

	if len(celExprs) > 0 {
		predicate.RequiredClusterSelector.CelSelector = clusterv1beta1.ClusterCelSelector{
			CelExpressions: celExprs,
		}
	}

	// Add predicate if it has any selectors
	if len(labelReqs) > 0 || len(celExprs) > 0 {
		spec.Predicates = []clusterv1beta1.ClusterPredicate{predicate}
	}

	// Add prioritizers to select clusters with best available resources
	// This ensures VMs land on clusters with sufficient capacity
	prioritizers := buildVMPrioritizers(req, scoreInfo)
	if len(prioritizers) > 0 {
		spec.PrioritizerPolicy = clusterv1beta1.PrioritizerPolicy{
			Mode:           clusterv1beta1.PrioritizerPolicyModeExact,
			Configurations: prioritizers,
		}
	}

	return spec
}

// mapNodeSelectorToClusterLabel maps VM nodeSelector labels to cluster labels
func mapNodeSelectorToClusterLabel(key, value string) *metav1.LabelSelectorRequirement {
	// Common mappings from node labels to cluster labels
	switch key {
	case "env":
		// env=prod -> environment=production OR env=prod
		if value == "prod" || value == "production" {
			// This will be handled via CEL expression for flexibility
			// Return a simple label match
			return &metav1.LabelSelectorRequirement{
				Key:      "env",
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{value},
			}
		}
		return &metav1.LabelSelectorRequirement{
			Key:      key,
			Operator: metav1.LabelSelectorOpIn,
			Values:   []string{value},
		}
	case "environment":
		return &metav1.LabelSelectorRequirement{
			Key:      key,
			Operator: metav1.LabelSelectorOpIn,
			Values:   []string{value},
		}
	case "region":
		// Node region label might map to cluster claim
		return nil // Will be handled via claims instead
	default:
		// Direct mapping for other labels
		return &metav1.LabelSelectorRequirement{
			Key:      key,
			Operator: metav1.LabelSelectorOpIn,
			Values:   []string{value},
		}
	}
}

// buildVMPrioritizers creates prioritizer configurations for VM placement
// Prioritizes clusters with best available CPU and memory
func buildVMPrioritizers(req *VMRequirements, scoreInfo *AddOnScoreInfo) []clusterv1beta1.PrioritizerConfig {
	var configs []clusterv1beta1.PrioritizerConfig

	// For VMs, we want to select clusters with good CPU and memory availability
	// Equal weighting for both resources
	cpuWeight := int32(5)
	memoryWeight := int32(5)

	useAddOnScores := scoreInfo != nil && scoreInfo.HasScores

	// Add CPU prioritizer
	if req.CPUCores > 0 {
		if useAddOnScores && scoreInfo.CPUScoreName != "" {
			configs = append(configs, clusterv1beta1.PrioritizerConfig{
				ScoreCoordinate: &clusterv1beta1.ScoreCoordinate{
					Type: clusterv1beta1.ScoreCoordinateTypeAddOn,
					AddOn: &clusterv1beta1.AddOnScore{
						ResourceName: scoreInfo.ResourceName,
						ScoreName:    scoreInfo.CPUScoreName,
					},
				},
				Weight: cpuWeight,
			})
		} else {
			configs = append(configs, clusterv1beta1.PrioritizerConfig{
				ScoreCoordinate: &clusterv1beta1.ScoreCoordinate{
					Type:    clusterv1beta1.ScoreCoordinateTypeBuiltIn,
					BuiltIn: "ResourceAllocatableCPU",
				},
				Weight: cpuWeight,
			})
		}
	}

	// Add Memory prioritizer
	if req.MemoryGi > 0 {
		if useAddOnScores && scoreInfo.MemScoreName != "" {
			configs = append(configs, clusterv1beta1.PrioritizerConfig{
				ScoreCoordinate: &clusterv1beta1.ScoreCoordinate{
					Type: clusterv1beta1.ScoreCoordinateTypeAddOn,
					AddOn: &clusterv1beta1.AddOnScore{
						ResourceName: scoreInfo.ResourceName,
						ScoreName:    scoreInfo.MemScoreName,
					},
				},
				Weight: memoryWeight,
			})
		} else {
			configs = append(configs, clusterv1beta1.PrioritizerConfig{
				ScoreCoordinate: &clusterv1beta1.ScoreCoordinate{
					Type:    clusterv1beta1.ScoreCoordinateTypeBuiltIn,
					BuiltIn: "ResourceAllocatableMemory",
				},
				Weight: memoryWeight,
			})
		}
	}

	// Add Steady prioritizer for stability
	if len(configs) > 0 {
		configs = append(configs, clusterv1beta1.PrioritizerConfig{
			ScoreCoordinate: &clusterv1beta1.ScoreCoordinate{
				Type:    clusterv1beta1.ScoreCoordinateTypeBuiltIn,
				BuiltIn: "Steady",
			},
			Weight: 2,
		})
	}

	return configs
}
