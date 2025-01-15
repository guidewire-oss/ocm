package helpers

import "strings"

func GetAwsAccountIdAndClusterName(clusterArn string) (string, string) {
	clusterStringParts := strings.Split(clusterArn, ":")
	clusterName := ""
	awsAccountId := ""
	if len(clusterStringParts) > 5 {
		clusterDetails := strings.Split(clusterStringParts[5], "/")
		if len(clusterDetails) > 1 {
			clusterName = clusterDetails[1]
		}
	}
	if len(clusterStringParts) > 4 {
		awsAccountId = clusterStringParts[4]
	}
	return awsAccountId, clusterName
}
