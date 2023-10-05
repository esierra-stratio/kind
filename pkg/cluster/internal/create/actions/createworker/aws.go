/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package createworker

import (
	"context"
	"encoding/base64"
	"io/ioutil"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"golang.org/x/exp/slices"
	"sigs.k8s.io/kind/pkg/cluster/nodes"
	"sigs.k8s.io/kind/pkg/commons"
	"sigs.k8s.io/kind/pkg/errors"
	"sigs.k8s.io/kind/pkg/exec"
)

var storageClassAWSTemplate = StorageClassDef{
	APIVersion: "storage.k8s.io/v1",
	Kind:       "StorageClass",
	Metadata: struct {
		Annotations map[string]string `yaml:"annotations,omitempty"`
		Name        string            `yaml:"name"`
	}{
		Annotations: map[string]string{
			"storageclass.kubernetes.io/is-default-class": "true",
		},
		Name: "keos",
	},
	AllowVolumeExpansion: true,
	Provisioner:          "ebs.csi.aws.com",
	Parameters:           make(map[string]interface{}),
	VolumeBindingMode:    "WaitForFirstConsumer",
}

var standardAWSParameters = commons.SCParameters{
	Type: "gp3",
}

var premiumAWSParameters = commons.SCParameters{
	Type: "io2",
	Iops: "64000",
}

type AWSBuilder struct {
	capxProvider     string
	capxVersion      string
	capxImageVersion string
	capxName         string
	capxTemplate     string
	capxEnvVars      []string
	stClassName      string
	csiNamespace     string
}

func newAWSBuilder() *AWSBuilder {
	return &AWSBuilder{}
}

func (b *AWSBuilder) setCapx(managed bool) {
	b.capxProvider = "aws"
	b.capxVersion = "v2.1.4"
	b.capxImageVersion = "2.1.4-0.4.0"
	b.capxName = "capa"
	b.stClassName = "keos"
	if managed {
		b.capxTemplate = "aws.eks.tmpl"
		b.csiNamespace = ""
	} else {
		b.capxTemplate = "aws.tmpl"
		b.csiNamespace = ""
	}
}

func (b *AWSBuilder) setCapxEnvVars(p commons.ProviderParams) {
	awsCredentials := "[default]\naws_access_key_id = " + p.Credentials["AccessKey"] + "\naws_secret_access_key = " + p.Credentials["SecretKey"] + "\nregion = " + p.Region + "\n"
	b.capxEnvVars = []string{
		"AWS_REGION=" + p.Region,
		"AWS_ACCESS_KEY_ID=" + p.Credentials["AccessKey"],
		"AWS_SECRET_ACCESS_KEY=" + p.Credentials["SecretKey"],
		"AWS_B64ENCODED_CREDENTIALS=" + base64.StdEncoding.EncodeToString([]byte(awsCredentials)),
		"CAPA_EKS_IAM=true",
	}
	if p.GithubToken != "" {
		b.capxEnvVars = append(b.capxEnvVars, "GITHUB_TOKEN="+p.GithubToken)
	}
}

func (b *AWSBuilder) getProvider() Provider {
	return Provider{
		capxProvider:     b.capxProvider,
		capxVersion:      b.capxVersion,
		capxImageVersion: b.capxImageVersion,
		capxName:         b.capxName,
		capxTemplate:     b.capxTemplate,
		capxEnvVars:      b.capxEnvVars,
		stClassName:      b.stClassName,
		csiNamespace:     b.csiNamespace,
	}
}

func (b *AWSBuilder) installCSI(n nodes.Node, k string) error {
	return nil
}

func createCloudFormationStack(n nodes.Node, envVars []string) error {
	var c string
	var err error

	eksConfigData := `
apiVersion: bootstrap.aws.infrastructure.cluster.x-k8s.io/v1beta1
kind: AWSIAMConfiguration
spec:
  bootstrapUser:
    enable: false
  eks:
    enable: true
    iamRoleCreation: false
    defaultControlPlaneRole:
        disable: false
  controlPlane:
    enableCSIPolicy: true
  nodes:
    extraPolicyAttachments:
    - arn:aws:iam::aws:policy/service-role/AmazonEBSCSIDriverPolicy`

	// Create the eks.config file in the container
	eksConfigPath := "/kind/eks.config"
	c = "echo \"" + eksConfigData + "\" > " + eksConfigPath
	_, err = commons.ExecuteCommand(n, c)
	if err != nil {
		return errors.Wrap(err, "failed to create eks.config")
	}

	// Run clusterawsadm with the eks.config file previously created (this will create or update the CloudFormation stack in AWS)
	c = "clusterawsadm bootstrap iam create-cloudformation-stack --config " + eksConfigPath
	_, err = commons.ExecuteCommand(n, c, envVars)
	if err != nil {
		return errors.Wrap(err, "failed to run clusterawsadm")
	}
	return nil
}

func (b *AWSBuilder) getAzs(networks commons.Networks) ([]string, error) {
	if len(b.capxEnvVars) == 0 {
		return nil, errors.New("Insufficient credentials.")
	}
	for _, cred := range b.capxEnvVars {
		c := strings.Split(cred, "=")
		envVar := c[0]
		envValue := c[1]
		os.Setenv(envVar, envValue)
	}

	sess, err := session.NewSession(&aws.Config{})
	if err != nil {
		return nil, err
	}
	svc := ec2.New(sess)
	if networks.Subnets != nil {
		privateAZs := []string{}
		for _, subnet := range networks.Subnets {
			privateSubnetID, _ := filterPrivateSubnet(svc, &subnet.SubnetId)
			if len(privateSubnetID) > 0 {
				sid := &ec2.DescribeSubnetsInput{
					SubnetIds: []*string{&subnet.SubnetId},
				}
				ds, err := svc.DescribeSubnets(sid)
				if err != nil {
					return nil, err
				}
				for _, describeSubnet := range ds.Subnets {
					if !slices.Contains(privateAZs, *describeSubnet.AvailabilityZone) {
						privateAZs = append(privateAZs, *describeSubnet.AvailabilityZone)
					}
				}
			}
		}
		return privateAZs, nil
	} else {
		result, err := svc.DescribeAvailabilityZones(&ec2.DescribeAvailabilityZonesInput{})
		if err != nil {
			return nil, err
		}
		azs := make([]string, 3)
		for i, az := range result.AvailabilityZones {
			if i == 3 {
				break
			}
			azs[i] = *az.ZoneName
		}
		return azs, nil
	}
}

func (b *AWSBuilder) internalNginx(networks commons.Networks, credentialsMap map[string]string, ClusterID string) (bool, error) {
	if len(b.capxEnvVars) == 0 {
		return false, errors.New("Insufficient credentials.")
	}
	for _, cred := range b.capxEnvVars {
		c := strings.Split(cred, "=")
		envVar := c[0]
		envValue := c[1]
		os.Setenv(envVar, envValue)
	}

	sess, err := session.NewSession(&aws.Config{})
	if err != nil {
		return false, err
	}
	svc := ec2.New(sess)
	if networks.Subnets != nil {
		for _, subnet := range networks.Subnets {
			publicSubnetID, _ := filterPublicSubnet(svc, &subnet.SubnetId)
			if len(publicSubnetID) > 0 {
				return false, nil
			}
		}
		return true, nil
	}
	return false, nil
}

func filterPrivateSubnet(svc *ec2.EC2, subnetID *string) (string, error) {
	keyname := "association.subnet-id"
	filters := make([]*ec2.Filter, 0)
	filter := ec2.Filter{
		Name: &keyname, Values: []*string{subnetID}}
	filters = append(filters, &filter)

	drti := &ec2.DescribeRouteTablesInput{Filters: filters}
	drto, err := svc.DescribeRouteTables(drti)
	if err != nil {
		return "", err
	}

	var isPublic bool
	for _, associatedRouteTable := range drto.RouteTables {
		for i := range associatedRouteTable.Routes {
			route := associatedRouteTable.Routes[i]

			if route.DestinationCidrBlock != nil &&
				route.GatewayId != nil &&
				*route.DestinationCidrBlock == "0.0.0.0/0" &&
				strings.Contains(*route.GatewayId, "igw") {
				isPublic = true
			}
		}
	}
	if !isPublic {
		return *subnetID, nil
	} else {
		return "", nil
	}
}

func filterPublicSubnet(svc *ec2.EC2, subnetID *string) (string, error) {
	keyname := "association.subnet-id"
	filters := make([]*ec2.Filter, 0)
	filter := ec2.Filter{
		Name: &keyname, Values: []*string{subnetID}}
	filters = append(filters, &filter)

	drti := &ec2.DescribeRouteTablesInput{Filters: filters}
	drto, err := svc.DescribeRouteTables(drti)
	if err != nil {
		return "", err
	}

	var isPublic bool
	for _, associatedRouteTable := range drto.RouteTables {
		for i := range associatedRouteTable.Routes {
			route := associatedRouteTable.Routes[i]

			if route.DestinationCidrBlock != nil &&
				route.GatewayId != nil &&
				*route.DestinationCidrBlock == "0.0.0.0/0" &&
				strings.Contains(*route.GatewayId, "igw") {
				isPublic = true
			}
		}
	}
	if isPublic {
		return *subnetID, nil
	} else {
		return "", nil
	}
}

func getEcrToken(p commons.ProviderParams) (string, error) {
	customProvider := credentials.NewStaticCredentialsProvider(
		p.Credentials["AccessKey"], p.Credentials["SecretKey"], "",
	)
	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithCredentialsProvider(customProvider),
		config.WithRegion(p.Region),
	)
	if err != nil {
		return "", err
	}

	svc := ecr.NewFromConfig(cfg)
	token, err := svc.GetAuthorizationToken(context.TODO(), &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return "", err
	}
	authData := token.AuthorizationData[0].AuthorizationToken
	data, err := base64.StdEncoding.DecodeString(*authData)
	if err != nil {
		return "", err
	}
	parts := strings.SplitN(string(data), ":", 2)
	return parts[1], nil
}

func (b *AWSBuilder) configureStorageClass(n nodes.Node, k string, sc commons.StorageClass) error {
	var c string
	var err error
	var cmd exec.Cmd

	// Remove annotation from default storage class
	c = "kubectl --kubeconfig " + k + " get sc | grep '(default)' | awk '{print $1}'"
	output, err := commons.ExecuteCommand(n, c)
	if err != nil {
		return errors.Wrap(err, "failed to get default storage class")
	}
	if strings.TrimSpace(output) != "" && strings.TrimSpace(output) != "No resources found" {
		c = "kubectl --kubeconfig " + k + " annotate sc " + strings.TrimSpace(output) + " " + defaultScAnnotation + "-"
		_, err = commons.ExecuteCommand(n, c)
		if err != nil {
			return errors.Wrap(err, "failed to remove annotation from default storage class")
		}
	}

	params := b.getParameters(sc)

	storageClass, err := insertParameters(storageClassAWSTemplate, params)
	if err != nil {
		return err
	}

	storageClass = strings.ReplaceAll(storageClass, "fsType", "csi.storage.k8s.io/fstype")

	cmd = n.Command("kubectl", "--kubeconfig", k, "apply", "-f", "-")
	if err = cmd.SetStdin(strings.NewReader(storageClass)).Run(); err != nil {
		return errors.Wrap(err, "failed to create default storage class")
	}

	return nil
}

func (b *AWSBuilder) getParameters(sc commons.StorageClass) commons.SCParameters {
	if sc.EncryptionKey != "" {
		sc.Parameters.Encrypted = "true"
		sc.Parameters.KmsKeyId = sc.EncryptionKey
	}
	switch class := sc.Class; class {
	case "standard":
		return mergeSCParameters(sc.Parameters, standardAWSParameters)
	case "premium":
		return mergeSCParameters(sc.Parameters, premiumAWSParameters)
	default:
		return mergeSCParameters(sc.Parameters, standardAWSParameters)
	}
}

func (b *AWSBuilder) getOverrideVars(descriptor commons.DescriptorFile, credentialsMap map[string]string) (map[string][]byte, error) {
	overrideVars := map[string][]byte{}
	InternalNginxOVPath, InternalNginxOVValue, err := b.getInternalNginxOverrideVars(descriptor.Networks, credentialsMap, descriptor.ClusterID)
	if err != nil {
		return nil, err
	}
	pvcSizeOVPath, pvcSizeOVValue, err := b.getPvcSizeOverrideVars(descriptor.StorageClass)
	if err != nil {
		return nil, err
	}
	overrideVars = addOverrideVar(InternalNginxOVPath, InternalNginxOVValue, overrideVars)
	overrideVars = addOverrideVar(pvcSizeOVPath, pvcSizeOVValue, overrideVars)

	return overrideVars, nil
}

func (b *AWSBuilder) getPvcSizeOverrideVars(sc commons.StorageClass) (string, []byte, error) {
	if (sc.Class == "premium" && sc.Parameters.Type == "") || sc.Parameters.Type == "io2" || sc.Parameters.Type == "io1" {
		return "storage-class.yaml", []byte("storage_class_pvc_size: 4Gi"), nil
	}
	if sc.Parameters.Type == "st1" || sc.Parameters.Type == "sc1" {
		return "storage-class.yaml", []byte("storage_class_pvc_size: 125Gi"), nil
	}
	return "", []byte(""), nil
}

func (b *AWSBuilder) getInternalNginxOverrideVars(networks commons.Networks, credentialsMap map[string]string, ClusterID string) (string, []byte, error) {
	requiredInternalNginx, err := b.internalNginx(networks, credentialsMap, ClusterID)
	if err != nil {
		return "", nil, err
	}

	if requiredInternalNginx {
		internalIngressFilePath := "files/" + b.capxProvider + "/internal-ingress-nginx.yaml"
		internalIngressFile, err := internalIngressFiles.Open(internalIngressFilePath)
		if err != nil {
			return "", nil, errors.Wrap(err, "error opening the internal ingress nginx file")
		}
		defer internalIngressFile.Close()

		internalIngressContent, err := ioutil.ReadAll(internalIngressFile)
		if err != nil {
			return "", nil, errors.Wrap(err, "error reading the internal ingress nginx file")
		}
		return "ingress-nginx.yaml", internalIngressContent, nil
	}
	return "", []byte(""), nil
}
