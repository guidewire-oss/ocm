package aws_irsa

import (
	"context"
	"fmt"
	"log"
	"regexp"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

type AwsIrsaAcceptor struct {
	AutoApprovalAwsPatterns []string
}

func (a *AwsIrsaAcceptor) allowAccept(ctx context.Context, cluster *clusterv1.ManagedCluster) (bool, error) {
	managedClusterArn := cluster.Annotations["agent.open-cluster-management.io/managed-cluster-arn"]
	managedClusterIAMRoleSuffix := cluster.Annotations["agent.open-cluster-management.io/managed-cluster-iam-role-suffix"]
	if managedClusterIAMRoleSuffix != "" && managedClusterArn != "" {
		for _, AutoApprovalAwsPattern := range a.AutoApprovalAwsPatterns {
			re, err := regexp.Compile(AutoApprovalAwsPattern)
			if err != nil {
				fmt.Println("Failed to process the approved arn pattern for aws irsa auto approval", err)
				return false, err
			}
			if re.MatchString(managedClusterArn) {
				return true, nil
				// TODO
			} else {
				log.Println("Managed cluster does not match any allowed patterns")
			}
		}
	}
	return false, nil
}

func (a *AwsIrsaAcceptor) Accept(ctx context.Context, cluster *clusterv1.ManagedCluster) (bool, error) {
	if a.AutoApprovalAwsPatterns == nil {
		log.Println("managed cluster auto accept as no patterns present.")
		return true, nil
	}
	allowAccept, err := a.allowAccept(ctx, cluster)
	if err != nil {
		fmt.Println("Failed to process the approved arn pattern for aws irsa auto approval", err)
		return false, err
	}
	if err == nil && allowAccept {
		log.Println("managed cluster auto accepted after pattern matching.")
		return true, nil
	}
	log.Println("managed cluster not auto accepted.")
	return false, nil
}

func NewAwsIrsaAcceptor(AutoApprovalAwsPatterns []string) register.Acceptor {
	return &AwsIrsaAcceptor{
		AutoApprovalAwsPatterns: AutoApprovalAwsPatterns,
	}
}
