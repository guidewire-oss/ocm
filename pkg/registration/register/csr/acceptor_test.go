package csr

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func TestAccept(t *testing.T) {

	cases := []struct {
		name       string
		cluster    *clusterv1.ManagedCluster
		isAccepted bool
	}{
		{
			name: "Accept cluster when annotations not present",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-cluster1",
				},
			},
			isAccepted: true,
		},
		{
			name: "Reject cluster when aws irsa annotations are present.",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-cluster2",
					Annotations: map[string]string{
						"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster2",
						"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "7f8141296c75f2871e3d030f85c35692",
					},
				},
			},
			isAccepted: false,
		},
	}

	CsrAcceptor := NewCsrAcceptor([]string{})

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isAccepted, err := CsrAcceptor.Accept(context.TODO(), c.cluster)
			if err == nil && c.isAccepted != isAccepted {
				t.Errorf("expect %t, but %t", c.isAccepted, isAccepted)
			}
		},
		)
	}

}
