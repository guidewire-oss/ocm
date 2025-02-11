package aws_irsa

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	v1 "open-cluster-management.io/api/cluster/v1"
	clustermanagerv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/ocm/manifests"
	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

type AwsIrsaApprover struct {
	arnPatterns []string
}

// Cleanup implements register.Approver.
func (a *AwsIrsaApprover) Cleanup(ctx context.Context, cluster *v1.ManagedCluster) error {
	// noop
	return nil
}

// Run implements register.Approver.
func (a *AwsIrsaApprover) Run(ctx context.Context, workers int) {
	// noop
	return
}

// AutoApprove Checks if the managed cluster arn matches any approved arn pattern and sets the HubAcceptsClient = true
// if there is a match
func (a *AwsIrsaApprover) AutoApprove(ctx context.Context, managedClusterArn string, cluster *clusterv1.ManagedCluster) error {
	if cluster.Annotations["agent.open-cluster-management.io/managed-cluster-iam-role-suffix"] != "" && cluster.Annotations["agent.open-cluster-management.io/managed-cluster-arn"] != "" {
		for _, arnPattern := range a.arnPatterns {
			re, err := regexp.Compile(arnPattern)
			if err != nil {
				fmt.Println("Failed to process the approved arn pattern for aws irsa auto approval", err)
				return err
			}

			if re.MatchString(managedClusterArn) {
				// TODO
			}
		}

	}
	return nil
}

// Cleanup is run when the cluster is deleting or hubAcceptClient is set false

// CreateIAMRolesAndPolicies implements register.Approver.
func (a *AwsIrsaApprover) CreateIAMRolesAndPolicies(ctx context.Context, cluster *clusterv1.ManagedCluster, clusterManager *clustermanagerv1.ClusterManager) error {
	registrationDrivers := clusterManager.Spec.RegistrationConfiguration.RegistrationDrivers

	//Creating config for aws
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		fmt.Println("Failed to load aws config", err)
		log.Fatal(err)
	}
	// Create an IAM client
	iamClient := iam.NewFromConfig(cfg)
	err = CreateIAMRolesAndPoliciesForAWSIRSA(ctx, registrationDrivers, cluster, iamClient)
	if err != nil {
		fmt.Println("Failed to create IAM roles and policies for aws irsa", err)
		log.Fatal(err)
	}

	return nil
}

func CreateIAMRolesAndPoliciesForAWSIRSA(ctx context.Context, RegistrationDrivers []clustermanagerv1.RegistrationDriverHub, managedCluster *v1.ManagedCluster, iamClient *iam.Client) error {
	var hubClusterArn string
	var managedClusterIamRoleSuffix string
	// Iterate through RegistrationDrivers and retrieve the HubClusterArn
	for _, registrationDriver := range RegistrationDrivers {
		if registrationDriver.AuthType == "awsirsa" {
			hubClusterArn = registrationDriver.HubClusterArn
		}
	}
	if hubClusterArn != "" && managedCluster.Annotations["agent.open-cluster-management.io/managed-cluster-iam-role-suffix"] != "" && managedCluster.Annotations["agent.open-cluster-management.io/managed-cluster-arn"] != "" {
		managedClusterIamRoleSuffix = managedCluster.Annotations["agent.open-cluster-management.io/managed-cluster-iam-role-suffix"]
		managedClusterArn := managedCluster.Annotations["agent.open-cluster-management.io/managed-cluster-arn"]

		managedClusterAccountId, managedClusterName := commonhelpers.GetAwsAccountIdAndClusterName(managedClusterArn)
		hubAccountId, hubClusterName := commonhelpers.GetAwsAccountIdAndClusterName(hubClusterArn)
		// Define the role name and trust policy
		roleName := "ocm-hub-" + managedClusterIamRoleSuffix

		// Check if hash is the same
		hash := commonhelpers.Md5HashSuffix(hubAccountId, hubClusterName, managedClusterAccountId, managedClusterName)
		if hash != managedClusterIamRoleSuffix {
			err := fmt.Errorf("Hub Cluster arn provided during join is different from the current hub cluster")
			return err
		}

		templateFiles := []string{"managed-cluster-policy/AccessPolicy.tmpl", "managed-cluster-policy/TrustPolicy.tmpl"}
		data := map[string]interface{}{
			"hubClusterArn":               hubClusterArn,
			"managedClusterAccountId":     managedClusterAccountId,
			"managedClusterIamRoleSuffix": managedClusterIamRoleSuffix,
			"hubAccountId":                hubAccountId,
			"hubClusterName":              hubClusterName,
			"managedClusterName":          managedClusterName,
		}
		renderedTemplates, err := renderTemplates(templateFiles, data)
		if err != nil {
			fmt.Println("Failed to render templates while creating IAM role and policy", err)
			return err
		}

		createRoleOutput, err := iamClient.CreateRole(context.TODO(), &iam.CreateRoleInput{
			RoleName:                 aws.String(roleName),
			AssumeRolePolicyDocument: aws.String(renderedTemplates[1]),
		})
		if err != nil {
			// Ignore error when role already exists as we will always create the same role
			if !(strings.Contains(err.Error(), "EntityAlreadyExists")) {
				log.Printf("Failed to create IAM role: %v\n", err)
				return err
			} else {
				log.Printf("Ignore IAM role creation error as entity already exists")
			}
		}
		fmt.Printf("Role created successfully: %s\n", *createRoleOutput.Role.Arn)

		createPolicyResult, err := iamClient.CreatePolicy(ctx, &iam.CreatePolicyInput{
			PolicyDocument: aws.String(renderedTemplates[0]),
			PolicyName:     aws.String(roleName),
		})

		if err != nil {
			log.Printf("Failed to create IAM Policy: %v\n", err)
			return err
		}
		fmt.Printf("Policy created successfully: %s\n", *createPolicyResult.Policy.Arn)

		_, err = iamClient.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
			PolicyArn: aws.String(*createPolicyResult.Policy.Arn),
			RoleName:  aws.String(roleName),
		})
		if err != nil {
			log.Printf("Couldn't attach policy %v to role %v. Here's why: %v\n", *createPolicyResult.Policy.Arn, roleName, err)
		}
		return err
	}
	return nil
}

func renderTemplates(argTemplates []string, data interface{}) (args []string, err error) {
	var t *template.Template
	var filebytes []byte
	for _, arg := range argTemplates {
		filebytes, err = manifests.ManagedClusterPolicyManifestFiles.ReadFile(arg)
		if err != nil {
			args = nil
			return
		}
		contents := string(filebytes)
		t, err = template.New(contents).Parse(contents)
		if err != nil {
			args = nil
			return
		}

		buf := &bytes.Buffer{}
		err = t.Execute(buf, data)
		if err != nil {
			args = nil
			return
		}
		args = append(args, buf.String())
	}

	return
}

func NewAwsIrsaApprover(arnPatterns []string) (register.Approver, error) {
	awsIrsaApprover := &AwsIrsaApprover{
		arnPatterns: arnPatterns,
	}
	return awsIrsaApprover, nil
}
