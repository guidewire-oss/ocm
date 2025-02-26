package csr

import (
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/ocm/pkg/features"
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

	kubeClient := kubefake.NewClientset()
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 3*time.Minute)
	recorder := eventstesting.NewTestingEventRecorder(t)
	utilruntime.Must(features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates))
	csrHubDriver, err := NewCSRHubDriver(kubeClient, informerFactory, []string{}, []string{}, recorder)

	if err != nil {
		t.Error(err)
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isAccepted := csrHubDriver.Accept(c.cluster)
			if c.isAccepted != isAccepted {
				t.Errorf("expect %t, but %t", c.isAccepted, isAccepted)
			}
		},
		)
	}
}
