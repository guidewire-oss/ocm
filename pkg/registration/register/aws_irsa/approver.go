package aws_irsa

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	v1 "open-cluster-management.io/api/cluster/v1"
	clustermanagerv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/ocm/manifests"
	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

type AwsIrsaApprover struct {
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
	eksClient := eks.NewFromConfig(cfg)
	err = CreateIAMRolesPoliciesAndAccessEntryForAWSIRSA(ctx, registrationDrivers, cluster, iamClient, eksClient)
	if err != nil {
		fmt.Println("Failed to create IAM roles, policies and access entry for aws irsa", err)
		log.Fatal(err)
	}

	return nil
}

func CreateIAMRolesPoliciesAndAccessEntryForAWSIRSA(ctx context.Context, RegistrationDrivers []clustermanagerv1.RegistrationDriverHub, managedCluster *v1.ManagedCluster, iamClient *iam.Client, eksClient *eks.Client) error {
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
			err := fmt.Errorf("hub cluster arn provided during join is different from the current hub cluster")
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

		var getRoleOutput *iam.GetRoleOutput
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
				getRoleOutput, err = iamClient.GetRole(context.TODO(), &iam.GetRoleInput{
					RoleName: aws.String(roleName),
				})
				if err != nil {
					log.Printf("Failed to get IAM role: %v\n", err)
					return err
				}
			}
		} else {
			fmt.Printf("Role created successfully: %s\n", *createRoleOutput.Role.Arn)
		}
		var getPolicyResult *iam.GetPolicyOutput
		createPolicyResult, err := iamClient.CreatePolicy(ctx, &iam.CreatePolicyInput{
			PolicyDocument: aws.String(renderedTemplates[0]),
			PolicyName:     aws.String(roleName),
		})
		if err != nil {
			if !(strings.Contains(err.Error(), "EntityAlreadyExists")) {
				log.Printf("Failed to create IAM Policy:%s %v\n", err, roleName)
				return err
			} else {
				log.Printf("Ignore IAM policy creation error as entity already exists")
				policyArn, err := getPolicyArnByName(iamClient, roleName)
				if err != nil {
					log.Fatalf("error retrieving policy ARN: %v", err)
				}
				getPolicyResult, err = iamClient.GetPolicy(context.TODO(), &iam.GetPolicyInput{
					PolicyArn: aws.String(policyArn),
				})
				if err != nil {
					log.Printf("Failed to get IAM Policy: %v %v\n", err, roleName)
					return err
				}
			}
		} else {
			fmt.Printf("Policy created successfully: %s\n", *createPolicyResult.Policy.Arn)
		}

		var policyArn string
		if getPolicyResult != nil {
			policyArn = *getRoleOutput.Role.Arn
		} else {
			policyArn = *createRoleOutput.Role.Arn
		}

		_, err = iamClient.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
			PolicyArn: aws.String(policyArn),
			RoleName:  aws.String(roleName),
		})
		if err != nil {
			log.Printf("Couldn't attach policy %v to role %v. Here's why: %v\n", *createPolicyResult.Policy.Arn, roleName, err)
			return err
		}

		// Create Access Entry
		var principalArn string
		if getRoleOutput != nil {
			principalArn = *getRoleOutput.Role.Arn
		} else {
			principalArn = *createRoleOutput.Role.Arn
		}

		params := &eks.CreateAccessEntryInput{
			ClusterName:      aws.String(hubClusterName),
			PrincipalArn:     aws.String(principalArn),
			Username:         aws.String(managedClusterName),
			KubernetesGroups: []string{"open-cluster-management:" + managedClusterName},
		}

		createAccessEntryOutput, err := eksClient.CreateAccessEntry(ctx, params)
		if err != nil {
			log.Printf("Failed to create Access Entry: %v\n", err)
			return err
		}
		fmt.Printf("Access entry created successfully: %s\n", *createAccessEntryOutput.AccessEntry.AccessEntryArn)
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

func NewAwsIrsaApprover() (register.Approver, error) {
	awsIrsaApprover := &AwsIrsaApprover{}
	return awsIrsaApprover, nil
}

func getPolicyArnByName(client *iam.Client, policyName string) (string, error) {
	var marker *string
	for {
		// List policies in batches
		output, err := client.ListPolicies(context.TODO(), &iam.ListPoliciesInput{
			Scope:  "Local", // "Local" for customer-managed policies, "AWS" for AWS-managed
			Marker: marker,
		})
		if err != nil {
			return "", err
		}

		// Look for the policy by name
		for _, policy := range output.Policies {
			if *policy.PolicyName == policyName {
				return *policy.Arn, nil
			}
		}

		// If there's a next page, continue
		if output.Marker == nil {
			break
		}
		marker = output.Marker
	}

	return "", fmt.Errorf("policy %s not found", policyName)
}
