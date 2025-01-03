package registration_test

import (
	"bytes"
	"fmt"
	"path"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/tools/clientcmd"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("Joining Process for aws flow", func() {
	var bootstrapKubeconfig string
	var managedClusterName string
	var hubKubeconfigSecret string
	var hubKubeconfigDir string

	ginkgo.BeforeEach(func() {
		postfix := rand.String(5)
		managedClusterName = fmt.Sprintf("joiningtest-managedcluster-%s", postfix)
		hubKubeconfigSecret = fmt.Sprintf("joiningtest-hub-kubeconfig-secret-%s", postfix)
		hubKubeconfigDir = path.Join(util.TestDir, fmt.Sprintf("joiningtest-%s", postfix), "hub-kubeconfig")
	})

	assertJoiningSucceed := func() {
		ginkgo.It("managedcluster should join successfully for aws flow", func() {
			var err error

			managedClusterArn := "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1"
			managedClusterRoleSuffix := "7f8141296c75f2871e3d030f85c35692"
			hubClusterArn := "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster1"

			// run registration agent
			agentOptions := &spoke.SpokeAgentOptions{
				RegistrationAuth:         spoke.AwsIrsaAuthType,
				HubClusterArn:            hubClusterArn,
				ManagedClusterArn:        managedClusterArn,
				ManagedClusterRoleSuffix: managedClusterRoleSuffix,
				BootstrapKubeconfig:      bootstrapKubeconfig,
				HubKubeconfigSecret:      hubKubeconfigSecret,
				ClusterHealthCheckPeriod: 1 * time.Minute,
			}
			commOptions := commonoptions.NewAgentOptions()
			commOptions.HubKubeconfigDir = hubKubeconfigDir
			commOptions.SpokeClusterName = managedClusterName

			cancel := runAgent("joiningtest", agentOptions, commOptions, spokeCfg)
			defer cancel()

			// the ManagedCluster CR should be created after bootstrap
			gomega.Eventually(func() error {
				if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// the csr should not be created for aws flow after bootstrap
			gomega.Eventually(func() error {
				if _, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.HaveOccurred())

			// simulate hub cluster admin to accept the managedcluster
			err = util.AcceptManagedCluster(clusterClient, managedClusterName)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Hour*24)
			gomega.Expect(err).To(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				secret, err := util.GetFilledAWSHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret)
				if err != nil {
					return err
				}

				hubKubeConfig, err := clientcmd.Load(secret.Data["kubeconfig"])
				if err != nil {
					return err
				}
				hubCurrentContext, ok := hubKubeConfig.Contexts[hubKubeConfig.CurrentContext]
				if !ok {
					return fmt.Errorf("context pointed to by the current-context property is missing")
				}
				hubCluster, ok := hubKubeConfig.Clusters[hubCurrentContext.Cluster]
				if !ok {
					return fmt.Errorf("cluster pointed to by the current-context is missing")
				}

				// TODO: assert user is well formed
				//hubUser, ok := hubKubeConfig.AuthInfos[hubCurrentContext.AuthInfo]
				//if !ok {
				//	return fmt.Errorf("user pointed to by the current-context is missing")
				//}

				bootstrapKubeConfig, err := clientcmd.LoadFromFile(agentOptions.BootstrapKubeconfig)
				if err != nil {
					return err
				}
				bootstrapCurrentContext, ok := bootstrapKubeConfig.Contexts[bootstrapKubeConfig.CurrentContext]
				if !ok {
					return fmt.Errorf("context pointed to by the current-context property is missing")
				}
				bootstrapCluster, ok := bootstrapKubeConfig.Clusters[bootstrapCurrentContext.Cluster]
				if !ok {
					return fmt.Errorf("cluster pointed to by the current-context is missing")
				}

				if hubCluster.Server != bootstrapCluster.Server {
					return fmt.Errorf("serverUrl mismatch in hub kubeconfig and bootstrap kubeconfig")
				}
				if !bytes.Equal(hubCluster.CertificateAuthorityData, bootstrapCluster.CertificateAuthorityData) {
					return fmt.Errorf("certificateAuthorityData mismatch in hub kubeconfig and bootstrap kubeconfig")
				}

				// TODO: assert other parts of the secret like agent-name and cluster-name similar to csr test

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// the spoke cluster should have joined condition finally
			// TODO: Revisit while implementing slice 3
			//gomega.Eventually(func() error {
			//	spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			//	if err != nil {
			//		return err
			//	}
			//	if !meta.IsStatusConditionTrue(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined) {
			//		return fmt.Errorf("cluster should be joined")
			//	}
			//	return nil
			//}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	}

	ginkgo.Context("without proxy", func() {
		ginkgo.BeforeEach(func() {
			bootstrapKubeconfig = bootstrapKubeConfigFile
		})
		assertJoiningSucceed()
	})

})
