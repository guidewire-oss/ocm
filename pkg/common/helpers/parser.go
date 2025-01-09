package helpers

import "strings"

func GetAwsAccountIdAndClusterName(clusterArn string) (string, string) {
	clusterStringParts := strings.Split(clusterArn, ":")
	clusterName := ""
	awsAccountId := ""
	if len(clusterStringParts) > 5 {
		clusterName = strings.Split(clusterStringParts[5], "/")[1]
	}
	if len(clusterStringParts) > 4 {
		awsAccountId = clusterStringParts[4]
	}
	return awsAccountId, clusterName
}
