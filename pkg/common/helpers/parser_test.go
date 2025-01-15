package helpers

import (
	"fmt"
	"testing"
)

func TestGetAwsAccountIdAndClusterName(t *testing.T) {

	awsAccountId, clusterName := GetAwsAccountIdAndClusterName("")
	if awsAccountId != "" && clusterName != "" {
		fmt.Errorf("awsAccountId and cluster id are not valid")
	}

	awsAccountId, clusterName = GetAwsAccountIdAndClusterName("arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster")
	if awsAccountId != "123456789012" && clusterName != "hub-cluster" {
		fmt.Errorf("awsAccountId and cluster id are not valid")
	}

	awsAccountId, clusterName = GetAwsAccountIdAndClusterName("arn:aws:eks:us-west-2:123456789012")
	if awsAccountId != "123456789012" && clusterName != "" {
		fmt.Errorf("awsAccountId and cluster id are not valid")
	}

	awsAccountId, clusterName = GetAwsAccountIdAndClusterName("arn:aws:eks:us-west-2:123456789012:cluster")
	if awsAccountId != "123456789012" {
		fmt.Errorf("awsAccountId and cluster id are not valid")
	}

}
