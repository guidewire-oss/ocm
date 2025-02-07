package aws_irsa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"html/template"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	v1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/ocm/manifests"
	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

type AwsIrsaApprover struct {
	hubClusterArn string
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
func (a *AwsIrsaApprover) CreateIAMRolesAndPolicies(ctx context.Context, cluster *clusterv1.ManagedCluster, kubeclient kubernetes.Interface  ) error {

	//Creating config for aws
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		fmt.Println("Failed to load aws config", err)
		log.Fatal(err)
	}
	// Create an IAM client
	iamClient := iam.NewFromConfig(cfg)
	eksClient := eks.NewFromConfig(cfg)
	hubclusterName , managedClusterName, principalArn ,err := CreateIAMRolesPoliciesForAWSIRSA(ctx, a.hubClusterArn, cluster, iamClient)
	_,hubClusterName := commonhelpers.GetAwsAccountIdAndClusterName(a.hubClusterArn)
	describeClusterOutput,err := eksClient.DescribeCluster(context.TODO(), &eks.DescribeClusterInput{
		Name:                 aws.String(hubClusterName),
	})

	if describeClusterOutput.Cluster.AccessConfig.AuthenticationMode == types.AuthenticationModeConfigMap {
		err = patchAuthConfigMapForAWSIRSA(principalArn, managedClusterName,kubeclient )
		if err != nil {
			fmt.Println("Failed to update aws-auth configmap for aws irsa", err)
			return err
		}
	} else {
		err = createAccessEntriesForAWSIRSA(ctx, eksClient, principalArn, hubclusterName , managedClusterName)
		if err != nil {
			fmt.Println("Failed to create Access Entries for aws irsa", err)
		}
	}
	if err != nil {
		fmt.Println("Failed to create IAM roles, policies and access entry/aws-auth configmap for aws irsa", err)
		return err
	}
	return nil
}

// This function creates IAM Roles and Policies in the hub cluster which are required by AWSIRSA type of authentication and returns the role hubclustername,managedclustername and the principalArn to be used for Access Entry creation
func CreateIAMRolesPoliciesForAWSIRSA(ctx context.Context, hubClusterArn string, managedCluster *v1.ManagedCluster, iamClient *iam.Client) (string,string,string,error) {
	var managedClusterIamRoleSuffix string
	var getRoleOutput *iam.GetRoleOutput
	var createRoleOutput *iam.CreateRoleOutput
	var hubclusterName string
	var managedClusterName string
	if hubClusterArn != "" && managedCluster.Annotations["agent.open-cluster-management.io/managed-cluster-iam-role-suffix"] != "" && managedCluster.Annotations["agent.open-cluster-management.io/managed-cluster-arn"] != "" {
		managedClusterIamRoleSuffix = managedCluster.Annotations["agent.open-cluster-management.io/managed-cluster-iam-role-suffix"]
		managedClusterArn := managedCluster.Annotations["agent.open-cluster-management.io/managed-cluster-arn"]

		managedClusterAccountId, managedClusterName := commonhelpers.GetAwsAccountIdAndClusterName(managedClusterArn)
		hubAccountId, hubClusterName := commonhelpers.GetAwsAccountIdAndClusterName(hubClusterArn)
		// Define the role name and trust policy
		roleName := "ocm-hub-" + managedClusterIamRoleSuffix
		policyName := "ocm-hub-" + managedClusterIamRoleSuffix

		// Check if hash is the same
		hash := commonhelpers.Md5HashSuffix(hubAccountId, hubClusterName, managedClusterAccountId, managedClusterName)
		if hash != managedClusterIamRoleSuffix {
			err := fmt.Errorf("hub cluster arn provided during join is different from the current hub cluster")
			return "","","",err
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
			return "","","",err
		}


		createRoleOutput, err = iamClient.CreateRole(context.TODO(), &iam.CreateRoleInput{
			RoleName:                 aws.String(roleName),
			AssumeRolePolicyDocument: aws.String(renderedTemplates[1]),
		})
		if err != nil {
			// Ignore error when role already exists as we will always create the same role
			if !(strings.Contains(err.Error(), "EntityAlreadyExists")) {
				log.Printf("Failed to create IAM role: %v\n", err)
				return "","","",err
			} else {
				log.Printf("Ignore IAM role creation error as entity already exists")
				getRoleOutput, err = iamClient.GetRole(context.TODO(), &iam.GetRoleInput{
					RoleName: aws.String(roleName),
				})
				if err != nil {
					log.Printf("Failed to get IAM role: %v\n", err)
					return "","","",err
				}
			}
		} else {
			fmt.Printf("Role created successfully: %s\n", *createRoleOutput.Role.Arn)
		}

		createPolicyResult, err := iamClient.CreatePolicy(ctx, &iam.CreatePolicyInput{
			PolicyDocument: aws.String(renderedTemplates[0]),
			PolicyName:     aws.String(policyName),
		})
		var policyArn string
		if err != nil {
			if !(strings.Contains(err.Error(), "EntityAlreadyExists")) {
				log.Printf("Failed to create IAM Policy:%s %v\n", err, roleName)
				return "","","",err
			} else {
				log.Printf("Ignore IAM policy creation error as entity already exists %v", err)
				policyArn, err = getPolicyArnByName(iamClient, policyName)
				if err != nil {
					log.Fatalf("error retrieving policy ARN: %v", err)
				}
			}
		} else {
			fmt.Printf("Policy created successfully: %s\n", *createPolicyResult.Policy.Arn)
		}

		if createPolicyResult != nil {
			policyArn = *createPolicyResult.Policy.Arn
		}

		_, err = iamClient.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
			PolicyArn: aws.String(policyArn),
			RoleName:  aws.String(roleName),
		})
		if err != nil {
			log.Printf("Couldn't attach policy %v to role %v. Here's why: %v\n", policyArn, roleName, err)
			return "","","",err
		}
	}

	var principalArn string
	if getRoleOutput != nil {
		principalArn = *getRoleOutput.Role.Arn
	} else {
		principalArn = *createRoleOutput.Role.Arn
	}
	return hubclusterName,managedClusterName,principalArn,nil
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

func NewAwsIrsaApprover(hubClusterArn string) (register.Approver, error) {
	awsIrsaApprover := &AwsIrsaApprover{
		hubClusterArn: hubClusterArn,
	}
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


func createAccessEntriesForAWSIRSA(ctx context.Context,eksClient *eks.Client , principalArn string , hubClusterName string , managedClusterName string) error {
	params := &eks.CreateAccessEntryInput{
		ClusterName:      aws.String(hubClusterName),
		PrincipalArn:     aws.String(principalArn),
		Username:         aws.String(managedClusterName),
		KubernetesGroups: []string{"open-cluster-management:" + managedClusterName},
		}

	createAccessEntryOutput, err := eksClient.CreateAccessEntry(ctx, params)
	if err != nil {
		if !(strings.Contains(err.Error(), "EntityAlreadyExists")) {
		log.Printf("Failed to create Access entry for the managed cluster  %v because of %v\n", managedClusterName, err)
		return err
		} else {
		log.Printf("Ignore Access entry creation error as entity already exists")
		}
	}
	fmt.Printf("Access entry created successfully: %s\n", *createAccessEntryOutput.AccessEntry.AccessEntryArn)
	return nil
}

func patchAuthConfigMapForAWSIRSA( principalArn string, managedClusterName string, kubeclient kubernetes.Interface) error {
	configMap, err := kubeclient.CoreV1().ConfigMaps("kube-system").Get(context.TODO(), "aws-auth", metav1.GetOptions{})
	if err != nil {
		log.Printf("Could not get config map aws-auth in the namespace aws-auth configmap")
		return err
	}

	var mapRoles []map[string]string
	err = json.Unmarshal([]byte(configMap.Data["mapRoles"]), &mapRoles);
	if err != nil {
		log.Printf("Failed to unmarshal mapRoles: %v", err)
		return err
	}

	newRole := map[string]string{
		"rolearn":  principalArn,
		"groups":   "system:open-cluster-management:"+managedClusterName,
	}
	mapRoles = append(mapRoles, newRole)
	jsonMap,err := json.Marshal(mapRoles)
	configMap.Data["mapRoles"] =  string(jsonMap)

	_, err = kubeclient.CoreV1().ConfigMaps("kube-system").Update(context.TODO(),configMap , metav1.UpdateOptions{})
	if err!=nil{
		log.Printf("Failed to update aws-auth configmap", err)
		return err
	}
	log.Printf("Aws-auth configmap updated.")
	return nil
}
