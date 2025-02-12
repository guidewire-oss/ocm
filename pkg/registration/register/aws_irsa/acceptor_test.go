package aws_irsa

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
			name: "Accept cluster when managedcluster in list of clusters.",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-cluster1",
					Annotations: map[string]string{
						"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1",
						"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "7f8141296c75f2871e3d030f85c35692",
					},
				},
			},
			isAccepted: true,
		},
		{
			name: "Reject cluster when managedcluster not in list of clusters.",
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
		{
			name: "Reject cluster when annotation not present.",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-cluster2",
				},
			},
			isAccepted: false,
		},
	}

	AwsIrsaAcceptor := NewAwsIrsaAcceptor([]string{"managed-cluster1"})

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isAccepted, err := AwsIrsaAcceptor.Accept(context.TODO(), c.cluster)
			if err == nil && c.isAccepted != isAccepted {
				t.Errorf("expect %t, but %t", c.isAccepted, isAccepted)
			}
		},
		)
	}

}
