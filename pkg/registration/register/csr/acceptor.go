package csr

import (
	"context"
	"log"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

type CsrAcceptor struct {
	AutoApprovalCsrUsers []string
}

// accept implements register.Acceptor.
func (c *CsrAcceptor) Accept(ctx context.Context, cluster *clusterv1.ManagedCluster) (bool, error) {
	allowAccept, err := c.allowAccept(ctx, cluster)
	if err == nil && allowAccept {
		return true, nil
	}
	return false, err
}

func (c *CsrAcceptor) allowAccept(ctx context.Context, cluster *clusterv1.ManagedCluster) (bool, error) {
	if cluster.Annotations["agent.open-cluster-management.io/managed-cluster-iam-role-suffix"] == "" && cluster.Annotations["agent.open-cluster-management.io/managed-cluster-arn"] == "" {
		log.Println("managed cluster auto accepted for csr.")
		return true, nil
	}
	log.Println("managed cluster not auto accepted.")
	return false, nil
}

func NewCsrAcceptor(AutoApprovalCsrUsers []string) register.Acceptor {
	return &CsrAcceptor{
		AutoApprovalCsrUsers: AutoApprovalCsrUsers,
	}
}
