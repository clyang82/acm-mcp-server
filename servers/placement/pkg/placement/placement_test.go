package placement

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"sigs.k8s.io/yaml"
)

// TestCELExpressionSupport tests that CEL expressions are properly evaluated
func TestCELExpressionSupport(t *testing.T) {
	// Create test clusters
	cluster1 := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
			Labels: map[string]string{
				"environment":       "production",
				"region":            "us-east-1",
				"openshiftVersion":  "4.18.0",
			},
		},
		Status: clusterv1.ManagedClusterStatus{
			ClusterClaims: []clusterv1.ManagedClusterClaim{
				{
					Name:  "region.open-cluster-management.io",
					Value: "us-east-1",
				},
				{
					Name:  "platform.open-cluster-management.io",
					Value: "AWS",
				},
			},
		},
	}

	cluster2 := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster2",
			Labels: map[string]string{
				"environment":      "production",
				"region":           "us-west-2",
				"openshiftVersion": "4.16.0",
			},
		},
		Status: clusterv1.ManagedClusterStatus{
			ClusterClaims: []clusterv1.ManagedClusterClaim{
				{
					Name:  "region.open-cluster-management.io",
					Value: "us-west-2",
				},
				{
					Name:  "platform.open-cluster-management.io",
					Value: "AWS",
				},
			},
		},
	}

	cluster3 := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster3",
			Labels: map[string]string{
				"environment":      "staging",
				"region":           "us-east-1",
				"openshiftVersion": "4.19.0",
			},
		},
		Status: clusterv1.ManagedClusterStatus{
			ClusterClaims: []clusterv1.ManagedClusterClaim{
				{
					Name:  "region.open-cluster-management.io",
					Value: "us-east-1",
				},
			},
		},
	}

	// Create fake client with test clusters
	objs := []runtime.Object{cluster1, cluster2, cluster3}
	clusterClient := clusterfake.NewSimpleClientset(objs...)

	// Create handler
	handler := NewHandler(clusterClient)

	tests := []struct {
		name           string
		placement      *clusterv1beta1.Placement
		expectedCount  int
		expectedNames  []string
		shouldHaveError bool
	}{
		{
			name: "CEL label selector",
			placement: &clusterv1beta1.Placement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cel-labels",
					Namespace: "default",
				},
				Spec: clusterv1beta1.PlacementSpec{
					Predicates: []clusterv1beta1.ClusterPredicate{
						{
							RequiredClusterSelector: clusterv1beta1.ClusterSelector{
								CelSelector: clusterv1beta1.ClusterCelSelector{
									CelExpressions: []string{
										`managedCluster.metadata.labels["environment"] == "production"`,
										`managedCluster.metadata.labels["region"] == "us-east-1"`,
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 1,
			expectedNames: []string{"cluster1"},
		},
		{
			name: "CEL semver comparison",
			placement: &clusterv1beta1.Placement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cel-semver",
					Namespace: "default",
				},
				Spec: clusterv1beta1.PlacementSpec{
					Predicates: []clusterv1beta1.ClusterPredicate{
						{
							RequiredClusterSelector: clusterv1beta1.ClusterSelector{
								CelSelector: clusterv1beta1.ClusterCelSelector{
									CelExpressions: []string{
										`semver(managedCluster.metadata.labels["openshiftVersion"]).isGreaterThan(semver("4.17.0"))`,
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 2,
			expectedNames: []string{"cluster1", "cluster3"},
		},
		{
			name: "CEL cluster claims with exists",
			placement: &clusterv1beta1.Placement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cel-claims",
					Namespace: "default",
				},
				Spec: clusterv1beta1.PlacementSpec{
					Predicates: []clusterv1beta1.ClusterPredicate{
						{
							RequiredClusterSelector: clusterv1beta1.ClusterSelector{
								CelSelector: clusterv1beta1.ClusterCelSelector{
									CelExpressions: []string{
										`managedCluster.status.clusterClaims.exists(c, c.name == "platform.open-cluster-management.io" && c.value == "AWS")`,
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 2,
			expectedNames: []string{"cluster1", "cluster2"},
		},
		{
			name: "Combined label, claim, and CEL selectors",
			placement: &clusterv1beta1.Placement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-combined",
					Namespace: "default",
				},
				Spec: clusterv1beta1.PlacementSpec{
					Predicates: []clusterv1beta1.ClusterPredicate{
						{
							RequiredClusterSelector: clusterv1beta1.ClusterSelector{
								LabelSelector: metav1.LabelSelector{
									MatchLabels: map[string]string{
										"environment": "production",
									},
								},
								ClaimSelector: clusterv1beta1.ClusterClaimSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "region.open-cluster-management.io",
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{"us-east-1"},
										},
									},
								},
								CelSelector: clusterv1beta1.ClusterCelSelector{
									CelExpressions: []string{
										`semver(managedCluster.metadata.labels["openshiftVersion"]).isGreaterThan(semver("4.17.0"))`,
									},
								},
							},
						},
					},
				},
			},
			expectedCount: 1,
			expectedNames: []string{"cluster1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert placement to DryRunPlacementParams
			yamlBytes, err := yaml.Marshal(tt.placement)
			if err != nil {
				t.Fatalf("Failed to encode placement: %v", err)
			}

			params := DryRunPlacementParams{
				PlacementYAML: string(yamlBytes),
			}

			// Run dry run
			result, err := handler.DryRunPlacement(context.Background(), params)

			if tt.shouldHaveError && err == nil {
				t.Errorf("Expected error but got none")
				return
			}
			if !tt.shouldHaveError && err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result.TotalMatched != tt.expectedCount {
				t.Errorf("Expected %d matched clusters, got %d", tt.expectedCount, result.TotalMatched)
			}

			// Check cluster names
			matchedNames := make(map[string]bool)
			for _, decision := range result.Decisions {
				matchedNames[decision.ClusterName] = true
			}

			for _, expectedName := range tt.expectedNames {
				if !matchedNames[expectedName] {
					t.Errorf("Expected cluster %s to match, but it didn't", expectedName)
				}
			}
		})
	}
}

// TestInvalidCELExpression tests that invalid CEL expressions produce clear error messages
func TestInvalidCELExpression(t *testing.T) {
	cluster1 := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
			Labels: map[string]string{
				"environment": "production",
			},
		},
	}

	objs := []runtime.Object{cluster1}
	clusterClient := clusterfake.NewSimpleClientset(objs...)
	handler := NewHandler(clusterClient)

	// Test with invalid CEL expression
	placement := &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-invalid-cel",
			Namespace: "default",
		},
		Spec: clusterv1beta1.PlacementSpec{
			Predicates: []clusterv1beta1.ClusterPredicate{
				{
					RequiredClusterSelector: clusterv1beta1.ClusterSelector{
						CelSelector: clusterv1beta1.ClusterCelSelector{
							CelExpressions: []string{
								`managedCluster.invalid.field == "value"`,
							},
						},
					},
				},
			},
		},
	}

	yamlBytes, _ := yaml.Marshal(placement)
	params := DryRunPlacementParams{
		PlacementYAML: string(yamlBytes),
	}

	result, err := handler.DryRunPlacement(context.Background(), params)
	if err != nil {
		t.Fatalf("DryRunPlacement should not return error, but got: %v", err)
	}

	// Should have 0 matches because CEL expression is invalid
	if result.TotalMatched != 0 {
		t.Errorf("Expected 0 matched clusters due to invalid CEL, got %d", result.TotalMatched)
	}
}
