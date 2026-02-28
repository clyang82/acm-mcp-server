package placement

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/cel-go/cel"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/ocm/pkg/placement/controllers/metrics"
	"open-cluster-management.io/ocm/pkg/placement/controllers/scheduling"
	"open-cluster-management.io/ocm/pkg/placement/helpers"
	"sigs.k8s.io/yaml"
)

// Handler handles placement-related MCP operations
type Handler struct {
	ClusterClient clusterclientset.Interface
	celEnv        *cel.Env // CEL environment for expression evaluation
}

// NewHandler creates a new placement handler
func NewHandler(clusterClient clusterclientset.Interface) *Handler {
	// Initialize CEL environment once for expression evaluation
	// Pass nil scoreLister since dry run doesn't need AddOnPlacementScore resources
	celEnv, err := helpers.NewEnv(nil)
	if err != nil {
		klog.ErrorS(err, "Failed to create CEL environment, CEL expressions will not be supported")
		celEnv = nil // Graceful degradation
	}

	return &Handler{
		ClusterClient: clusterClient,
		celEnv:        celEnv,
	}
}

// AddOnScoreInfo represents information about available AddOnPlacementScore resources
type AddOnScoreInfo struct {
	HasScores    bool
	ResourceName string
	ScoreNames   map[string]bool // Map of score names that are available
}

// detectAddOnPlacementScores checks if AddOnPlacementScore resources exist with CPU and memory scores
func (h *Handler) detectAddOnPlacementScores(ctx context.Context) *AddOnScoreInfo {
	info := &AddOnScoreInfo{
		HasScores:  false,
		ScoreNames: make(map[string]bool),
	}

	// Get all managed clusters to check their namespaces for AddOnPlacementScore resources
	clusters, err := h.ClusterClient.ClusterV1().ManagedClusters().List(ctx, metav1.ListOptions{
		Limit: 1, // Just check one cluster to see if the score exists
	})
	if err != nil || len(clusters.Items) == 0 {
		klog.V(4).InfoS("No clusters found or error listing clusters, using BuiltIn prioritizers", "error", err)
		return info
	}

	// Check the first available cluster's namespace for AddOnPlacementScore
	clusterNamespace := clusters.Items[0].Name

	// Try common resource names
	resourceNames := []string{"resource-usage-score", "default"}

	for _, resourceName := range resourceNames {
		score, err := h.ClusterClient.ClusterV1alpha1().AddOnPlacementScores(clusterNamespace).Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			continue
		}

		// Check if the score has the metrics we're looking for
		hasCPU := false
		hasMem := false
		for _, item := range score.Status.Scores {
			if item.Name == "cpuClusterAvailable" {
				hasCPU = true
				info.ScoreNames["cpuClusterAvailable"] = true
			}
			if item.Name == "memClusterAvailable" {
				hasMem = true
				info.ScoreNames["memClusterAvailable"] = true
			}
		}

		if hasCPU && hasMem {
			info.HasScores = true
			info.ResourceName = resourceName
			klog.V(4).InfoS("Found AddOnPlacementScore with CPU and memory scores", "resourceName", resourceName, "namespace", clusterNamespace)
			return info
		}
	}

	klog.V(4).InfoS("No AddOnPlacementScore resources found with required scores, using BuiltIn prioritizers")
	return info
}

// GeneratePlacementParams represents parameters for generating placement
type GeneratePlacementParams struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Description string `json:"description"`
}

// DryRunPlacementParams represents parameters for dry-running placement
type DryRunPlacementParams struct {
	PlacementYAML string `json:"placementYAML,omitempty"`
	Description   string `json:"description,omitempty"`
	Name          string `json:"name,omitempty"`
	Namespace     string `json:"namespace,omitempty"`
}

// DryRunPlacementResult represents the result of a placement dry run
// This mimics the PlacementDecision structure from OCM
type DryRunPlacementResult struct {
	Decisions    []ClusterDecision `json:"decisions"`
	TotalMatched int               `json:"totalMatched"`
	Summary      string            `json:"summary"`
}

// ClusterDecision represents a decision for a selected cluster
// This matches clusterapiv1beta1.ClusterDecision
type ClusterDecision struct {
	ClusterName string `json:"clusterName"`
	Reason      string `json:"reason"`
}

// GeneratePlacement generates a Placement YAML based on natural language description
func (h *Handler) GeneratePlacement(ctx context.Context, params GeneratePlacementParams) (string, error) {
	if params.Name == "" {
		return "", fmt.Errorf("placement name is required")
	}
	if params.Namespace == "" {
		return "", fmt.Errorf("placement namespace is required")
	}
	if params.Description == "" {
		return "", fmt.Errorf("description is required")
	}

	// Parse the description to extract requirements
	requirements := parseRequirements(params.Description)

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
		Spec: buildPlacementSpec(requirements, scoreInfo),
	}

	// Convert to YAML
	yamlBytes, err := yaml.Marshal(placement)
	if err != nil {
		return "", fmt.Errorf("failed to marshal placement to YAML: %v", err)
	}

	return string(yamlBytes), nil
}

// DryRunPlacement evaluates which clusters would be selected by a Placement without creating it
func (h *Handler) DryRunPlacement(ctx context.Context, params DryRunPlacementParams) (*DryRunPlacementResult, error) {
	placement, err := h.parsePlacementParams(ctx, params)
	if err != nil {
		return nil, err
	}

	// Get all clusters
	clusters, err := h.ClusterClient.ClusterV1().ManagedClusters().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list clusters: %v", err)
	}

	// Filter, score, and rank clusters using OCM scheduler
	decisions := h.evaluateClusters(clusters.Items, placement)

	return &DryRunPlacementResult{
		Decisions:    decisions,
		TotalMatched: len(decisions),
		Summary:      fmt.Sprintf("Selected %d out of %d total clusters", len(decisions), len(clusters.Items)),
	}, nil
}

// parsePlacementParams parses DryRunPlacementParams into a Placement object
func (h *Handler) parsePlacementParams(ctx context.Context, params DryRunPlacementParams) (*clusterv1beta1.Placement, error) {
	if params.PlacementYAML != "" {
		placement := &clusterv1beta1.Placement{}
		if err := yaml.Unmarshal([]byte(params.PlacementYAML), placement); err != nil {
			return nil, fmt.Errorf("failed to unmarshal placement YAML: %v", err)
		}
		return placement, nil
	}

	if params.Description != "" {
		name := params.Name
		if name == "" {
			name = "dryrun-placement"
		}
		namespace := params.Namespace
		if namespace == "" {
			namespace = "default"
		}

		// Detect AddOnPlacementScore resources for intelligent prioritizer selection
		scoreInfo := h.detectAddOnPlacementScores(ctx)

		return &clusterv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: buildPlacementSpec(parseRequirements(params.Description), scoreInfo),
		}, nil
	}

	return nil, fmt.Errorf("either placementYAML or description must be provided")
}

// emptyPlacementDecisionLister implements PlacementDecisionLister interface with empty results
// This is used for dry run to avoid nil pointer errors in plugins
type emptyPlacementDecisionLister struct{}

func (e *emptyPlacementDecisionLister) List(selector labels.Selector) (ret []*clusterv1beta1.PlacementDecision, err error) {
	return []*clusterv1beta1.PlacementDecision{}, nil
}

func (e *emptyPlacementDecisionLister) PlacementDecisions(namespace string) clusterlisterv1beta1.PlacementDecisionNamespaceLister {
	return &emptyPlacementDecisionNamespaceLister{}
}

// emptyPlacementDecisionNamespaceLister implements PlacementDecisionNamespaceLister interface with empty results
type emptyPlacementDecisionNamespaceLister struct{}

func (e *emptyPlacementDecisionNamespaceLister) List(selector labels.Selector) (ret []*clusterv1beta1.PlacementDecision, err error) {
	return []*clusterv1beta1.PlacementDecision{}, nil
}

func (e *emptyPlacementDecisionNamespaceLister) Get(name string) (*clusterv1beta1.PlacementDecision, error) {
	return nil, fmt.Errorf("not found")
}

// evaluateClusters filters and scores clusters based on placement spec using OCM scheduler
func (h *Handler) evaluateClusters(clusters []clusterv1.ManagedCluster, placement *clusterv1beta1.Placement) []ClusterDecision {
	// Convert []clusterv1.ManagedCluster to []*clusterv1.ManagedCluster for scheduler
	clusterPtrs := make([]*clusterv1.ManagedCluster, 0, len(clusters))
	for i := range clusters {
		// Skip clusters in terminating state
		if !clusters[i].DeletionTimestamp.IsZero() {
			continue
		}
		clusterPtrs = append(clusterPtrs, &clusters[i])
	}

	// Check if placement has prioritizers configured
	hasPrioritizers := placement.Spec.PrioritizerPolicy.Mode != "" || len(placement.Spec.PrioritizerPolicy.Configurations) > 0

	// If no prioritizers and no numberOfClusters limit, use simple predicate-only evaluation for better performance
	if !hasPrioritizers && placement.Spec.NumberOfClusters == nil {
		return h.evaluateClustersPredicateOnly(clusterPtrs, placement)
	}

	// Create scheduler handler with minimal dependencies (dry run doesn't need all components)
	// Create a metrics recorder for the scheduler (required even for dry run)
	metricsRecorder := metrics.NewScheduleMetrics(clock.RealClock{})

	// Create empty lister to avoid nil pointer errors in plugins
	// The taint toleration plugin requires a non-nil placementDecisionLister
	emptyLister := &emptyPlacementDecisionLister{}

	handle := scheduling.NewSchedulerHandler(
		h.ClusterClient,
		emptyLister,     // Empty lister for dry run
		nil,             // scoreLister - not needed for dry run without AddOn prioritizers
		nil,             // clusterLister - not needed, we already have the clusters
		nil,             // eventsRecorder - not needed for dry run
		metricsRecorder, // metricsRecorder - required to avoid nil pointer errors
	)

	// Create the plugin scheduler (includes filters + prioritizers)
	scheduler := scheduling.NewPluginScheduler(handle)

	// Run the full scheduling logic
	result, status := scheduler.Schedule(context.Background(), placement, clusterPtrs)
	if status.IsError() {
		klog.V(4).InfoS("Scheduler returned error, falling back to predicate-only evaluation", "error", status.Message())
		// Fallback to predicate-only logic
		return h.evaluateClustersPredicateOnly(clusterPtrs, placement)
	}

	// Convert scheduled decisions to ClusterDecision format with scores
	decisions := []ClusterDecision{}
	prioritizerScores := result.PrioritizerScores()

	for _, cluster := range result.Decisions() {
		score := prioritizerScores[cluster.Name]
		reason := fmt.Sprintf("Score: %d", score)

		// Add detailed score breakdown if available
		if len(result.PrioritizerResults()) > 0 {
			scoreDetails := []string{}
			for _, pr := range result.PrioritizerResults() {
				if clusterScore, exists := pr.Scores[cluster.Name]; exists {
					scoreDetails = append(scoreDetails, fmt.Sprintf("%s=%d*%d", pr.Name, clusterScore, pr.Weight))
				}
			}
			if len(scoreDetails) > 0 {
				reason = fmt.Sprintf("Total score: %d (%s)", score, strings.Join(scoreDetails, ", "))
			}
		}

		decisions = append(decisions, ClusterDecision{
			ClusterName: cluster.Name,
			Reason:      reason,
		})
	}

	return decisions
}

// evaluateClustersPredicateOnly filters clusters based on predicates without scoring
// This is used as a fallback or when no prioritizers are configured
func (h *Handler) evaluateClustersPredicateOnly(clusters []*clusterv1.ManagedCluster, placement *clusterv1beta1.Placement) []ClusterDecision {
	decisions := []ClusterDecision{}

	for _, cluster := range clusters {
		// Check if cluster taints are tolerated
		if tolerated, _ := isClusterTolerated(cluster, placement.Spec.Tolerations); !tolerated {
			continue
		}

		// If no predicates, select all tolerated clusters
		if len(placement.Spec.Predicates) == 0 {
			decisions = append(decisions, ClusterDecision{
				ClusterName: cluster.Name,
				Reason:      "No predicates specified, all clusters match",
			})
			continue
		}

		// Match clusters against predicates (predicates are ORed)
		if matched, reason := h.matchClusterWithPredicates(cluster, placement.Spec.Predicates); matched {
			decisions = append(decisions, ClusterDecision{
				ClusterName: cluster.Name,
				Reason:      reason,
			})
		}
	}

	// Apply numberOfClusters limit if specified
	if placement.Spec.NumberOfClusters != nil {
		limit := int(*placement.Spec.NumberOfClusters)
		if len(decisions) > limit {
			decisions = decisions[:limit]
		}
	}

	return decisions
}

// isClusterTolerated checks if a cluster's taints are tolerated by the placement
func isClusterTolerated(cluster *clusterv1.ManagedCluster, tolerations []clusterv1beta1.Toleration) (bool, string) {
	for _, taint := range cluster.Spec.Taints {
		// PreferNoSelect doesn't block selection
		if taint.Effect == clusterv1.TaintEffectPreferNoSelect {
			continue
		}

		// Check if NoSelect taint is tolerated
		if taint.Effect == clusterv1.TaintEffectNoSelect && !isTaintToleratedByAny(taint, tolerations) {
			return false, fmt.Sprintf("Cluster has untolerated taint: %s", taint.Key)
		}
	}

	return true, ""
}

// isTaintToleratedByAny checks if a taint is tolerated by any toleration in the list
func isTaintToleratedByAny(taint clusterv1.Taint, tolerations []clusterv1beta1.Toleration) bool {
	for _, toleration := range tolerations {
		if isTaintTolerated(taint, toleration) {
			return true
		}
	}
	return false
}

// isTaintTolerated checks if a specific taint is tolerated by a toleration
func isTaintTolerated(taint clusterv1.Taint, toleration clusterv1beta1.Toleration) bool {
	// Check effect matches if specified
	if len(toleration.Effect) > 0 && toleration.Effect != taint.Effect {
		return false
	}

	// Check key matches if specified
	if len(toleration.Key) > 0 && toleration.Key != taint.Key {
		return false
	}

	// Check operator and value
	switch toleration.Operator {
	case "", clusterv1beta1.TolerationOpEqual:
		return toleration.Value == taint.Value
	case clusterv1beta1.TolerationOpExists:
		return true
	default:
		return false
	}
}

// matchClusterWithPredicates checks if a cluster matches any predicate (predicates are ORed)
func (h *Handler) matchClusterWithPredicates(cluster *clusterv1.ManagedCluster, predicates []clusterv1beta1.ClusterPredicate) (bool, string) {
	for i, predicate := range predicates {
		if h.matchClusterWithPredicate(cluster, &predicate) {
			return true, fmt.Sprintf("Matched predicate %d", i)
		}
	}
	return false, "No predicates matched"
}

// matchClusterWithPredicate checks if a cluster matches a predicate
// All selectors within a predicate are ANDed (label, claim, and CEL selectors)
func (h *Handler) matchClusterWithPredicate(cluster *clusterv1.ManagedCluster, predicate *clusterv1beta1.ClusterPredicate) bool {
	clusterSelector, err := helpers.NewClusterSelector(
		predicate.RequiredClusterSelector,
		h.celEnv,
		nil, // no metrics for dry run
	)
	if err != nil {
		klog.V(4).InfoS("Invalid cluster selector", "error", err)
		return false
	}

	// Compile and check CEL expressions
	for i, result := range clusterSelector.Compile() {
		if result.Error != nil {
			klog.V(4).InfoS("CEL compilation failed", "expression", i, "error", result.Error.Detail)
			return false
		}
	}

	return clusterSelector.Matches(context.Background(), cluster)
}

// Requirements represents parsed placement requirements
type Requirements struct {
	Environment      string   // e.g., "production", "staging"
	Regions          []string // e.g., ["us-east", "us-west"]
	MinCPU           string   // e.g., "8"
	MinMemory        string   // e.g., "16Gi"
	MinDisk          string   // e.g., "500Gi", "1Ti"
	MinVersion       string   // e.g., "4.16"
	CloudProvider    string   // e.g., "AWS", "Azure", "GCP"
	CustomLabels     map[string]string
	CustomClaims     map[string]string
	NumberOfClusters *int32
	// Prioritization hints
	PrioritizeCPU    bool // true if "highest CPU", "most CPU", "best CPU"
	PrioritizeMemory bool // true if "highest memory", "most memory", "best memory"
	PrioritizeBest   bool // true if "best" without specific resource
}

// parseRequirements parses natural language description into structured requirements
func parseRequirements(description string) Requirements {
	req := Requirements{
		CustomLabels: make(map[string]string),
		CustomClaims: make(map[string]string),
	}

	desc := strings.ToLower(description)

	// Parse environment
	if strings.Contains(desc, "production") || strings.Contains(desc, "prod") {
		req.Environment = "production"
	} else if strings.Contains(desc, "staging") || strings.Contains(desc, "stage") {
		req.Environment = "staging"
	} else if strings.Contains(desc, "development") || strings.Contains(desc, "dev") {
		req.Environment = "development"
	}

	// Parse regions - use regex to capture full region including AZ suffix
	regionRegex := regexp.MustCompile(`\b(us-(?:east|west|central)(?:-\d)?|eu-(?:west|central|north)(?:-\d)?|ap-(?:southeast|northeast|south)(?:-\d)?)\b`)
	if matches := regionRegex.FindAllString(desc, -1); len(matches) > 0 {
		// Remove duplicates
		seen := make(map[string]bool)
		for _, region := range matches {
			if !seen[region] {
				req.Regions = append(req.Regions, region)
				seen[region] = true
			}
		}
	}

	// Parse cloud provider
	if strings.Contains(desc, "aws") || strings.Contains(desc, "amazon") {
		req.CloudProvider = "AWS"
	} else if strings.Contains(desc, "azure") {
		req.CloudProvider = "Azure"
	} else if strings.Contains(desc, "gcp") || strings.Contains(desc, "google") {
		req.CloudProvider = "GCP"
	}

	// Parse OpenShift version (e.g., "OpenShift ≥ 4.16" or "OpenShift >= 4.16")
	versionRegex := regexp.MustCompile(`openshift\s*[≥>=]+\s*(\d+\.\d+)`)
	if matches := versionRegex.FindStringSubmatch(desc); len(matches) > 1 {
		req.MinVersion = matches[1]
	}

	// Parse capacity requirements
	cpuRegex := regexp.MustCompile(`(\d+)\s*(?:cpu|cores|cpus)`)
	if matches := cpuRegex.FindStringSubmatch(desc); len(matches) > 1 {
		req.MinCPU = matches[1]
	}

	memoryRegex := regexp.MustCompile(`(\d+)\s*(?:gi|gb|gib)\s+memory`)
	if matches := memoryRegex.FindStringSubmatch(desc); len(matches) > 1 {
		req.MinMemory = matches[1] + "Gi"
	}

	// Parse disk/storage requirements (e.g., "500GB disk", "1TB storage")
	diskRegex := regexp.MustCompile(`(\d+)\s*(?:gi|gb|gib|ti|tb|tib)\s+(?:disk|storage)`)
	if matches := diskRegex.FindStringSubmatch(desc); len(matches) > 1 {
		// Normalize to Gi or Ti
		sizeStr := matches[1]
		if strings.Contains(strings.ToLower(matches[0]), "ti") || strings.Contains(strings.ToLower(matches[0]), "tb") {
			req.MinDisk = sizeStr + "Ti"
		} else {
			req.MinDisk = sizeStr + "Gi"
		}
	}

	// Parse number of clusters
	// Matches patterns like: "1 cluster", "1 production cluster", "select 1 cluster", etc.
	numRegex := regexp.MustCompile(`(\d+)\s+(?:\w+\s+)*clusters?`)
	if matches := numRegex.FindStringSubmatch(desc); len(matches) > 1 {
		if num, err := strconv.ParseInt(matches[1], 10, 32); err == nil {
			n := int32(num)
			req.NumberOfClusters = &n
		}
	}

	// Parse prioritization keywords
	// "highest CPU", "most CPU", "best CPU", "high CPU availability"
	if regexp.MustCompile(`(highest|most|best|high|maximum|max).*cpu`).MatchString(desc) {
		req.PrioritizeCPU = true
	}
	// "highest memory", "most memory", "best memory", "high memory availability"
	if regexp.MustCompile(`(highest|most|best|high|maximum|max).*memory`).MatchString(desc) {
		req.PrioritizeMemory = true
	}
	// "best cluster" or just "best" - prioritize both CPU and memory
	if regexp.MustCompile(`\bbest\b`).MatchString(desc) && !req.PrioritizeCPU && !req.PrioritizeMemory {
		req.PrioritizeBest = true
	}

	return req
}

// buildCELExpressions generates CEL expressions for version and capacity constraints
func buildCELExpressions(req Requirements) []string {
	var expressions []string

	// Production environment: Check all common label combinations
	// Supports: environment=production, environment=prod, env=production, env=prod
	// Use has() to safely check if labels exist before accessing them
	if req.Environment == "production" {
		expressions = append(expressions,
			`(has(managedCluster.metadata.labels.environment) && (managedCluster.metadata.labels["environment"] == "production" || managedCluster.metadata.labels["environment"] == "prod")) || (has(managedCluster.metadata.labels.env) && (managedCluster.metadata.labels["env"] == "production" || managedCluster.metadata.labels["env"] == "prod"))`)
	} else if req.Environment != "" {
		// For non-production environments, check both "environment" and "env" keys
		expressions = append(expressions,
			fmt.Sprintf(`(has(managedCluster.metadata.labels.environment) && managedCluster.metadata.labels["environment"] == "%s") || (has(managedCluster.metadata.labels.env) && managedCluster.metadata.labels["env"] == "%s")`, req.Environment, req.Environment))
	}

	// OpenShift version: !semver(...).isLessThan(...) for >= comparison
	if req.MinVersion != "" {
		expressions = append(expressions,
			fmt.Sprintf(`has(managedCluster.metadata.labels.openshiftVersion) && !semver(managedCluster.metadata.labels["openshiftVersion"]).isLessThan(semver("%s.0"))`, req.MinVersion))
	}

	// CPU capacity
	if req.MinCPU != "" {
		expressions = append(expressions,
			fmt.Sprintf(`has(managedCluster.status.capacity.cpu) && int(managedCluster.status.capacity.cpu) >= %s`, req.MinCPU))
	}

	// Memory capacity (strip unit suffixes)
	if req.MinMemory != "" {
		memValue := strings.TrimSuffix(req.MinMemory, "Gi")
		expressions = append(expressions,
			fmt.Sprintf(`has(managedCluster.status.capacity.memory) && int(managedCluster.status.capacity.memory.replace("Gi", "").replace("Mi", "").replace("Ki", "")) >= %s`, memValue))
	}

	// Disk/Storage capacity (ephemeral-storage)
	// Convert to Ki (smallest unit) for comparison
	if req.MinDisk != "" {
		var diskValueKi int64
		if strings.HasSuffix(req.MinDisk, "Ti") {
			// Convert Ti to Ki: 1Ti = 1024 * 1024 * 1024 Ki
			tiValue := strings.TrimSuffix(req.MinDisk, "Ti")
			if val, err := strconv.ParseInt(tiValue, 10, 64); err == nil {
				diskValueKi = val * 1024 * 1024 * 1024
			}
		} else if strings.HasSuffix(req.MinDisk, "Gi") {
			// Convert Gi to Ki: 1Gi = 1024 * 1024 Ki
			giValue := strings.TrimSuffix(req.MinDisk, "Gi")
			if val, err := strconv.ParseInt(giValue, 10, 64); err == nil {
				diskValueKi = val * 1024 * 1024
			}
		}
		if diskValueKi > 0 {
			expressions = append(expressions,
				fmt.Sprintf(`"ephemeral-storage" in managedCluster.status.capacity && int(managedCluster.status.capacity["ephemeral-storage"].replace("Ki", "")) >= %d`, diskValueKi))
		}
	}

	return expressions
}

// buildPlacementSpec builds a PlacementSpec from requirements
func buildPlacementSpec(req Requirements, scoreInfo *AddOnScoreInfo) clusterv1beta1.PlacementSpec {
	spec := clusterv1beta1.PlacementSpec{NumberOfClusters: req.NumberOfClusters}

	predicate := clusterv1beta1.ClusterPredicate{
		RequiredClusterSelector: clusterv1beta1.ClusterSelector{},
	}

	// Build label selector
	labelReqs := buildLabelRequirements(req)
	if len(labelReqs) > 0 {
		predicate.RequiredClusterSelector.LabelSelector = metav1.LabelSelector{
			MatchExpressions: labelReqs,
		}
	}

	// Build claim selector
	claimReqs := buildClaimRequirements(req)
	if len(claimReqs) > 0 {
		predicate.RequiredClusterSelector.ClaimSelector = clusterv1beta1.ClusterClaimSelector{
			MatchExpressions: claimReqs,
		}
	}

	// Build CEL selector
	celExprs := buildCELExpressions(req)
	if len(celExprs) > 0 {
		predicate.RequiredClusterSelector.CelSelector = clusterv1beta1.ClusterCelSelector{
			CelExpressions: celExprs,
		}
	}

	// Add predicate if it has any selectors
	if len(labelReqs) > 0 || len(claimReqs) > 0 || len(celExprs) > 0 {
		spec.Predicates = []clusterv1beta1.ClusterPredicate{predicate}
	}

	// Add prioritizers based on parsed keywords
	// Intelligently uses AddOn scores when available, falls back to BuiltIn scores
	prioritizers := buildPrioritizers(req, scoreInfo)
	if len(prioritizers) > 0 {
		spec.PrioritizerPolicy = clusterv1beta1.PrioritizerPolicy{
			Mode:           clusterv1beta1.PrioritizerPolicyModeExact,
			Configurations: prioritizers,
		}
	}

	return spec
}

// buildPrioritizers creates prioritizer configurations based on requirements
// It intelligently uses AddOnPlacementScore (real-time metrics) when available,
// falling back to BuiltIn prioritizers (static capacity) when not available
func buildPrioritizers(req Requirements, scoreInfo *AddOnScoreInfo) []clusterv1beta1.PrioritizerConfig {
	var configs []clusterv1beta1.PrioritizerConfig

	// Determine weights based on user intent
	cpuWeight := int32(0)
	memoryWeight := int32(0)

	if req.PrioritizeCPU {
		// User explicitly wants highest CPU
		cpuWeight = 8
		memoryWeight = 3 // Lower weight for memory
	} else if req.PrioritizeMemory {
		// User explicitly wants highest memory
		cpuWeight = 3   // Lower weight for CPU
		memoryWeight = 8
	} else if req.PrioritizeBest {
		// User wants "best" overall - equal weights
		cpuWeight = 5
		memoryWeight = 5
	}

	// Choose between AddOn scores (real-time) and BuiltIn scores (static capacity)
	useAddOnScores := scoreInfo != nil && scoreInfo.HasScores

	// Add CPU prioritizer if weight > 0
	if cpuWeight > 0 {
		if useAddOnScores && scoreInfo.ScoreNames["cpuClusterAvailable"] {
			// Use real-time CPU availability from AddOnPlacementScore
			configs = append(configs, clusterv1beta1.PrioritizerConfig{
				ScoreCoordinate: &clusterv1beta1.ScoreCoordinate{
					Type: clusterv1beta1.ScoreCoordinateTypeAddOn,
					AddOn: &clusterv1beta1.AddOnScore{
						ResourceName: scoreInfo.ResourceName,
						ScoreName:    "cpuClusterAvailable",
					},
				},
				Weight: cpuWeight,
			})
			klog.V(4).InfoS("Using AddOn score for CPU prioritization", "scoreName", "cpuClusterAvailable")
		} else {
			// Fallback to static allocatable CPU
			configs = append(configs, clusterv1beta1.PrioritizerConfig{
				ScoreCoordinate: &clusterv1beta1.ScoreCoordinate{
					Type:    clusterv1beta1.ScoreCoordinateTypeBuiltIn,
					BuiltIn: "ResourceAllocatableCPU",
				},
				Weight: cpuWeight,
			})
			klog.V(4).InfoS("Using BuiltIn score for CPU prioritization", "scoreName", "ResourceAllocatableCPU")
		}
	}

	// Add Memory prioritizer if weight > 0
	if memoryWeight > 0 {
		if useAddOnScores && scoreInfo.ScoreNames["memClusterAvailable"] {
			// Use real-time memory availability from AddOnPlacementScore
			configs = append(configs, clusterv1beta1.PrioritizerConfig{
				ScoreCoordinate: &clusterv1beta1.ScoreCoordinate{
					Type: clusterv1beta1.ScoreCoordinateTypeAddOn,
					AddOn: &clusterv1beta1.AddOnScore{
						ResourceName: scoreInfo.ResourceName,
						ScoreName:    "memClusterAvailable",
					},
				},
				Weight: memoryWeight,
			})
			klog.V(4).InfoS("Using AddOn score for memory prioritization", "scoreName", "memClusterAvailable")
		} else {
			// Fallback to static allocatable memory
			configs = append(configs, clusterv1beta1.PrioritizerConfig{
				ScoreCoordinate: &clusterv1beta1.ScoreCoordinate{
					Type:    clusterv1beta1.ScoreCoordinateTypeBuiltIn,
					BuiltIn: "ResourceAllocatableMemory",
				},
				Weight: memoryWeight,
			})
			klog.V(4).InfoS("Using BuiltIn score for memory prioritization", "scoreName", "ResourceAllocatableMemory")
		}
	}

	// Add Steady prioritizer if any resource prioritizers were added
	// This helps maintain stable placement decisions
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

// buildLabelRequirements creates label selector requirements
func buildLabelRequirements(req Requirements) []metav1.LabelSelectorRequirement {
	var reqs []metav1.LabelSelectorRequirement

	// Environment is now handled via CEL expressions to support multiple label key variations
	// (environment=production, env=prod, etc.)

	if req.CloudProvider != "" {
		reqs = append(reqs, metav1.LabelSelectorRequirement{
			Key:      "cloud",
			Operator: metav1.LabelSelectorOpIn,
			Values:   []string{req.CloudProvider},
		})
	}

	for k, v := range req.CustomLabels {
		reqs = append(reqs, metav1.LabelSelectorRequirement{
			Key:      k,
			Operator: metav1.LabelSelectorOpIn,
			Values:   []string{v},
		})
	}

	return reqs
}

// buildClaimRequirements creates claim selector requirements
func buildClaimRequirements(req Requirements) []metav1.LabelSelectorRequirement {
	var reqs []metav1.LabelSelectorRequirement

	if len(req.Regions) > 0 {
		reqs = append(reqs, metav1.LabelSelectorRequirement{
			Key:      "region.open-cluster-management.io",
			Operator: metav1.LabelSelectorOpIn,
			Values:   req.Regions,
		})
	}

	for k, v := range req.CustomClaims {
		reqs = append(reqs, metav1.LabelSelectorRequirement{
			Key:      k,
			Operator: metav1.LabelSelectorOpIn,
			Values:   []string{v},
		})
	}

	return reqs
}

// MarshalResult marshals placement data into MCP content block format
func MarshalResult(data interface{}) (json.RawMessage, error) {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	result, err := json.Marshal(map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": string(dataJSON),
			},
		},
	})
	return result, err
}
