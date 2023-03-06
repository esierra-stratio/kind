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
	"os"

	"gopkg.in/yaml.v3"
	"sigs.k8s.io/kind/pkg/cluster/internal/create/actions/cluster"
)

type KEOSDescriptor struct {
	ExternalRegistry struct {
		AuthRequired bool   `yaml:"auth_required"`
		Type         string `yaml:"type"`
		URL          string `yaml:"url"`
	} `yaml:"external_registry"`
	AWS struct {
		Enabled bool `yaml:"enabled"`
		EKS     bool `yaml:"eks"`
	} `yaml:"aws,omitempty"`
	GCP struct {
		Enabled bool `yaml:"enabled"`
		GKE     bool `yaml:"gke"`
	} `yaml:"gcp,omitempty"`
	Keos struct {
		Calico struct {
			Ipip                 bool   `yaml:"ipip"`
			Pool                 string `yaml:"pool"`
			DeployTigeraOperator bool   `yaml:"deploy_tigera_operator"`
		} `yaml:"calico,omitempty"`
		ClusterID string `yaml:"cluster_id"`
		Dns       struct {
			ExternalDns struct {
				Enabled *bool `yaml:"enabled"`
			} `yaml:"external_dns,omitempty"`
		} `yaml:"dns,omitempty"`
		Domain          string `yaml:"domain"`
		ExternalDomain  string `yaml:"external_domain"`
		Flavour         string `yaml:"flavour"`
		K8sInstallation bool   `yaml:"k8s_installation"`
		Storage         struct {
			DefaultStorageClass string   `yaml:"default_storage_class"`
			Providers           []string `yaml:"providers"`
		} `yaml:"storage"`
	} `yaml:"keos"`
}

func createKEOSDescriptor(descriptorFile cluster.DescriptorFile, storageClass string) error {

	var keosDescriptor KEOSDescriptor
	var err error

	// External registry
	keosDescriptor.ExternalRegistry.URL = descriptorFile.ExternalRegistry.URL
	keosDescriptor.ExternalRegistry.AuthRequired = descriptorFile.ExternalRegistry.AuthRequired
	keosDescriptor.ExternalRegistry.Type = descriptorFile.ExternalRegistry.Type

	// AWS
	if descriptorFile.InfraProvider == "aws" {
		keosDescriptor.AWS.Enabled = true
		keosDescriptor.AWS.EKS = descriptorFile.ControlPlane.Managed
	}

	// GCP
	if descriptorFile.InfraProvider == "gcp" {
		keosDescriptor.GCP.Enabled = true
		keosDescriptor.GCP.GKE = descriptorFile.ControlPlane.Managed
	}

	// Keos
	keosDescriptor.Keos.ClusterID = descriptorFile.ClusterID
	keosDescriptor.Keos.K8sInstallation = false
	keosDescriptor.Keos.Domain = descriptorFile.Keos.Domain
	if descriptorFile.ExternalDomain == "" {
		keosDescriptor.Keos.ExternalDomain = descriptorFile.ClusterID + ".ext"
	} else {
		keosDescriptor.Keos.ExternalDomain = descriptorFile.ExternalDomain
	}
	keosDescriptor.Keos.Flavour = descriptorFile.Keos.Flavour

	// Keos - Calico
	if !descriptorFile.ControlPlane.Managed {
		keosDescriptor.Keos.Calico.Ipip = true
		keosDescriptor.Keos.Calico.Pool = "192.168.0.0/16"
		keosDescriptor.Keos.Calico.DeployTigeraOperator = false
	}

	// Keos - Storage
	keosDescriptor.Keos.Storage.DefaultStorageClass = storageClass
	keosDescriptor.Keos.Storage.Providers = []string{"custom"}

	// Keos - External dns
	if !descriptorFile.Dns.HostedZones {
		keosDescriptor.Keos.Dns.ExternalDns.Enabled = &descriptorFile.Dns.HostedZones
	}

	keosYAMLData, err := yaml.Marshal(keosDescriptor)
	if err != nil {
		return err
	}

	// Write file to disk
	err = os.WriteFile("keos.yaml", []byte(keosYAMLData), 0644)
	if err != nil {
		return err
	}

	return nil
}
