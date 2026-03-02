package placement

import (
	"testing"

	"sigs.k8s.io/yaml"
	kubevirtv1 "kubevirt.io/api/core/v1"
)

func TestExtractVMRequirements(t *testing.T) {
	vmYAML := `
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: demo-vm
  namespace: default
spec:
  running: true
  template:
    metadata:
      labels:
        kubevirt.io/domain: demo-vm
    spec:
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchLabels:
                  kubevirt.io/domain: demo-vm
              topologyKey: kubernetes.io/hostname
      nodeSelector:
        env: prod
      domain:
        cpu:
          cores: 2
        resources:
          requests:
            memory: 4Gi
        devices:
          disks:
            - name: rootdisk
              disk:
                bus: virtio
          interfaces:
            - name: default
              masquerade: {}
      networks:
        - name: default
          pod: {}
      volumes:
        - name: rootdisk
          containerDisk:
            image: quay.io/kubevirt/cirros-container-disk-demo
`

	var vm kubevirtv1.VirtualMachine
	if err := yaml.Unmarshal([]byte(vmYAML), &vm); err != nil {
		t.Fatalf("Failed to parse VM YAML: %v", err)
	}

	req := extractVMRequirements(&vm)

	// Verify CPU cores
	if req.CPUCores != 2 {
		t.Errorf("Expected CPUCores=2, got %d", req.CPUCores)
	}

	// Verify memory
	if req.MemoryGi != 4 {
		t.Errorf("Expected MemoryGi=4, got %d", req.MemoryGi)
	}

	// Verify node selectors
	if req.NodeSelectors["env"] != "prod" {
		t.Errorf("Expected env=prod in nodeSelectors, got %v", req.NodeSelectors)
	}

	// Verify anti-affinity
	if !req.HasAntiAffinity {
		t.Error("Expected HasAntiAffinity=true")
	}

	if req.AntiAffinityKey != "kubernetes.io/hostname" {
		t.Errorf("Expected AntiAffinityKey=kubernetes.io/hostname, got %s", req.AntiAffinityKey)
	}

	// Verify numberOfClusters (should be 1 for hostname anti-affinity)
	if req.NumberOfClusters == nil || *req.NumberOfClusters != 1 {
		t.Errorf("Expected NumberOfClusters=1 for hostname anti-affinity, got %v", req.NumberOfClusters)
	}
}

func TestBuildPlacementSpecFromVM(t *testing.T) {
	req := &VMRequirements{
		CPUCores:  2,
		MemoryGi:  4,
		NodeSelectors: map[string]string{
			"env": "prod",
		},
		HasAntiAffinity: true,
		AntiAffinityKey: "kubernetes.io/hostname",
	}
	one := int32(1)
	req.NumberOfClusters = &one

	// Test without AddOn scores
	spec := buildPlacementSpecFromVM(req, nil)

	// Verify numberOfClusters
	if spec.NumberOfClusters == nil || *spec.NumberOfClusters != 1 {
		t.Errorf("Expected NumberOfClusters=1, got %v", spec.NumberOfClusters)
	}

	// Verify predicates exist
	if len(spec.Predicates) == 0 {
		t.Error("Expected predicates to be set")
	}

	// Verify CEL expressions for CPU and memory
	if len(spec.Predicates[0].RequiredClusterSelector.CelSelector.CelExpressions) < 2 {
		t.Errorf("Expected at least 2 CEL expressions (CPU and memory), got %d",
			len(spec.Predicates[0].RequiredClusterSelector.CelSelector.CelExpressions))
	}

	// Verify prioritizers exist (CPU, Memory, Steady)
	if len(spec.PrioritizerPolicy.Configurations) != 3 {
		t.Errorf("Expected 3 prioritizer configurations, got %d",
			len(spec.PrioritizerPolicy.Configurations))
	}

	// Verify BuiltIn prioritizers are used when no AddOn scores available
	for _, config := range spec.PrioritizerPolicy.Configurations {
		if config.ScoreCoordinate.Type == "" {
			t.Error("ScoreCoordinate type should not be empty")
		}
	}
}

func TestBuildPlacementSpecFromVMWithAddOnScores(t *testing.T) {
	req := &VMRequirements{
		CPUCores: 2,
		MemoryGi: 4,
	}

	scoreInfo := &AddOnScoreInfo{
		HasScores:    true,
		ResourceName: "resource-usage-score",
		CPUScoreName: "cpuAvailable",
		MemScoreName: "memAvailable",
	}

	spec := buildPlacementSpecFromVM(req, scoreInfo)

	// Verify prioritizers use AddOn scores
	foundCPUAddOn := false
	foundMemAddOn := false

	for _, config := range spec.PrioritizerPolicy.Configurations {
		if config.ScoreCoordinate.Type == "AddOn" {
			if config.ScoreCoordinate.AddOn.ScoreName == "cpuAvailable" {
				foundCPUAddOn = true
			}
			if config.ScoreCoordinate.AddOn.ScoreName == "memAvailable" {
				foundMemAddOn = true
			}
		}
	}

	if !foundCPUAddOn {
		t.Error("Expected CPU AddOn prioritizer to be configured")
	}
	if !foundMemAddOn {
		t.Error("Expected Memory AddOn prioritizer to be configured")
	}
}
