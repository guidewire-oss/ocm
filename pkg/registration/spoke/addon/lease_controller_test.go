package addon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	clocktesting "k8s.io/utils/clock/testing"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonfake "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

var now = time.Now()

func TestQueueKeyFunc(t *testing.T) {
	cases := []struct {
		name             string
		addOns           []runtime.Object
		lease            runtime.Object
		expectedQueueKey string
	}{
		{
			name:             "no addons",
			addOns:           []runtime.Object{},
			lease:            testinghelpers.NewAddOnLease("test", "test", time.Now()),
			expectedQueueKey: "",
		},
		{
			name: "no install namespace",
			addOns: []runtime.Object{&addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{Namespace: testinghelpers.TestManagedClusterName, Name: "test"},
			}},
			lease:            testinghelpers.NewAddOnLease("test", "test", time.Now()),
			expectedQueueKey: "",
		},
		{
			name: "different install namespace",
			addOns: []runtime.Object{&addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      "test",
				},
				Spec: addonv1alpha1.ManagedClusterAddOnSpec{
					InstallNamespace: "other",
				},
			}},
			lease:            testinghelpers.NewAddOnLease("test", "test", time.Now()),
			expectedQueueKey: "",
		},
		{
			name: "an addon lease",
			addOns: []runtime.Object{&addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      "test",
				},
				Spec: addonv1alpha1.ManagedClusterAddOnSpec{
					InstallNamespace: "test",
				},
			}},
			lease:            testinghelpers.NewAddOnLease("test", "test", time.Now()),
			expectedQueueKey: "test/test",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addOnClient := addonfake.NewSimpleClientset(c.addOns...)
			addOnInformerFactory := addoninformers.NewSharedInformerFactory(addOnClient, time.Minute*10)
			addOnStroe := addOnInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
			for _, addOn := range c.addOns {
				if err := addOnStroe.Add(addOn); err != nil {
					t.Fatal(err)
				}
			}

			ctrl := &managedClusterAddOnLeaseController{
				clusterName: testinghelpers.TestManagedClusterName,
				addOnLister: addOnInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
			}
			actualQueueKey := ctrl.queueKeyFunc(c.lease)
			if actualQueueKey != c.expectedQueueKey {
				t.Errorf("expected queue key %q, but got %q", c.expectedQueueKey, actualQueueKey)
			}
		})
	}
}

func TestSync(t *testing.T) {
	cases := []struct {
		name             string
		queueKey         string
		addOns           []runtime.Object
		hubLeases        []runtime.Object
		managementLeases []runtime.Object
		spokeLeases      []runtime.Object
		validateActions  func(t *testing.T, ctx *testingcommon.FakeSyncContext, actions []clienttesting.Action)
	}{
		{
			name:        "bad queue key",
			queueKey:    "test/test/test",
			addOns:      []runtime.Object{},
			hubLeases:   []runtime.Object{},
			spokeLeases: []runtime.Object{},
			validateActions: func(t *testing.T, ctx *testingcommon.FakeSyncContext, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:        "no addons",
			queueKey:    "test/test",
			addOns:      []runtime.Object{},
			spokeLeases: []runtime.Object{},
			hubLeases:   []runtime.Object{},
			validateActions: func(t *testing.T, ctx *testingcommon.FakeSyncContext, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:     "no addon leases",
			queueKey: "test/test",
			addOns: []runtime.Object{&addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      "test",
				},
				Spec: addonv1alpha1.ManagedClusterAddOnSpec{
					InstallNamespace: "test",
				},
			}},
			hubLeases:   []runtime.Object{},
			spokeLeases: []runtime.Object{},
			validateActions: func(t *testing.T, ctx *testingcommon.FakeSyncContext, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				addOn := &addonv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				addOnCond := meta.FindStatusCondition(addOn.Status.Conditions, "Available")
				if addOnCond == nil {
					t.Errorf("expected addon available condition, but failed")
					return
				}
				if addOnCond.Status != metav1.ConditionUnknown {
					t.Errorf("expected addon available condition is unknown, but failed")
				}
			},
		},
		{
			name:     "addon stop to update its lease",
			queueKey: "test/test",
			addOns: []runtime.Object{&addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      "test",
				},
				Spec: addonv1alpha1.ManagedClusterAddOnSpec{
					InstallNamespace: "test",
				},
			}},
			hubLeases: []runtime.Object{},
			spokeLeases: []runtime.Object{
				testinghelpers.NewAddOnLease("test", "test", now.Add(-5*time.Minute)),
			},
			validateActions: func(t *testing.T, ctx *testingcommon.FakeSyncContext, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				addOn := &addonv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				addOnCond := meta.FindStatusCondition(addOn.Status.Conditions, "Available")
				if addOnCond == nil {
					t.Errorf("expected addon available condition, but failed")
					return
				}
				if addOnCond.Status != metav1.ConditionFalse {
					t.Errorf("expected addon available condition is unavailable, but failed")
				}
			},
		},
		{
			name:     "addon update its lease constantly",
			queueKey: "test/test",
			addOns: []runtime.Object{&addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      "test",
				},
				Spec: addonv1alpha1.ManagedClusterAddOnSpec{
					InstallNamespace: "test",
				},
			}},
			hubLeases: []runtime.Object{},
			spokeLeases: []runtime.Object{
				testinghelpers.NewAddOnLease("test", "test", now),
			},
			validateActions: func(t *testing.T, ctx *testingcommon.FakeSyncContext, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				addOn := &addonv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				addOnCond := meta.FindStatusCondition(addOn.Status.Conditions, "Available")
				if addOnCond == nil {
					t.Errorf("expected addon available condition, but failed")
					return
				}
				if addOnCond.Status != metav1.ConditionTrue {
					t.Errorf("expected addon available condition is available, but failed")
				}
			},
		},
		{
			name:     "addon status is not changed",
			queueKey: "test/test",
			addOns: []runtime.Object{&addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      "test",
				},
				Spec: addonv1alpha1.ManagedClusterAddOnSpec{
					InstallNamespace: "test",
				},
				Status: addonv1alpha1.ManagedClusterAddOnStatus{
					Conditions: []metav1.Condition{
						{
							Type:    "Available",
							Status:  metav1.ConditionTrue,
							Reason:  "ManagedClusterAddOnLeaseUpdated",
							Message: "test add-on is available.",
						},
					},
				},
			}},
			hubLeases: []runtime.Object{},
			spokeLeases: []runtime.Object{
				testinghelpers.NewAddOnLease("test", "test", now),
			},
			validateActions: func(t *testing.T, ctx *testingcommon.FakeSyncContext, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:     "sync all addons",
			queueKey: "key",
			addOns: []runtime.Object{
				&addonv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testinghelpers.TestManagedClusterName,
						Name:      "test1",
					},
					Spec: addonv1alpha1.ManagedClusterAddOnSpec{
						InstallNamespace: "test",
					},
				},
				&addonv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testinghelpers.TestManagedClusterName,
						Name:      "test2",
					},
				},
			},
			hubLeases: []runtime.Object{},
			spokeLeases: []runtime.Object{
				testinghelpers.NewAddOnLease("test1", "test1", now.Add(-5*time.Minute)),
			},
			validateActions: func(t *testing.T, ctx *testingcommon.FakeSyncContext, actions []clienttesting.Action) {
				if ctx.Queue().Len() != 2 {
					t.Errorf("expected two addons in queue, but failed")
				}
			},
		},
		{
			name:     "addon update its lease constantly (on management cluster)",
			queueKey: "test/test",
			addOns: []runtime.Object{&addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      "test",
					Annotations: map[string]string{
						addonv1alpha1.HostingClusterNameAnnotationKey: "cluster1",
					},
				},
				Spec: addonv1alpha1.ManagedClusterAddOnSpec{
					InstallNamespace: "test",
				},
			}},
			hubLeases: []runtime.Object{},
			managementLeases: []runtime.Object{
				testinghelpers.NewAddOnLease("test", "test", now),
			},
			validateActions: func(t *testing.T, ctx *testingcommon.FakeSyncContext, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				addOn := &addonv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				addOnCond := meta.FindStatusCondition(addOn.Status.Conditions, "Available")
				if addOnCond == nil {
					t.Errorf("expected addon available condition, but failed")
					return
				}
				if addOnCond.Status != metav1.ConditionTrue {
					t.Errorf("expected addon available condition is available, but failed")
				}
			},
		},
		{
			name:     "addon has customized health check",
			queueKey: "test/test",
			addOns: []runtime.Object{&addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      "test",
				},
				Status: addonv1alpha1.ManagedClusterAddOnStatus{
					HealthCheck: addonv1alpha1.HealthCheck{
						Mode: addonv1alpha1.HealthCheckModeCustomized,
					},
				},
			}},
			hubLeases:   []runtime.Object{},
			spokeLeases: []runtime.Object{},
			validateActions: func(t *testing.T, ctx *testingcommon.FakeSyncContext, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addOnClient := addonfake.NewSimpleClientset(c.addOns...)
			addOnInformerFactory := addoninformers.NewSharedInformerFactory(addOnClient, time.Minute*10)
			addOnStroe := addOnInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
			for _, addOn := range c.addOns {
				if err := addOnStroe.Add(addOn); err != nil {
					t.Fatal(err)
				}
			}

			managementLeaseClient := kubefake.NewClientset(c.managementLeases...)
			spokeLeaseClient := kubefake.NewClientset(c.spokeLeases...)

			ctrl := &managedClusterAddOnLeaseController{
				clusterName: testinghelpers.TestManagedClusterName,
				clock:       clocktesting.NewFakeClock(time.Now()),
				patcher: patcher.NewPatcher[
					*addonv1alpha1.ManagedClusterAddOn, addonv1alpha1.ManagedClusterAddOnSpec, addonv1alpha1.ManagedClusterAddOnStatus](
					addOnClient.AddonV1alpha1().ManagedClusterAddOns(testinghelpers.TestManagedClusterName)),
				addOnLister:           addOnInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				managementLeaseClient: managementLeaseClient.CoordinationV1(),
				spokeLeaseClient:      spokeLeaseClient.CoordinationV1(),
			}
			syncCtx := testingcommon.NewFakeSyncContext(t, c.queueKey)
			syncErr := ctrl.sync(context.TODO(), syncCtx)
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, syncCtx, addOnClient.Actions())
		})
	}
}
