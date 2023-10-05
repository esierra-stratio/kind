package validation

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/exp/slices"
	"sigs.k8s.io/kind/pkg/commons"
)

var gcpInstance *GCPValidator

type GCPValidator struct {
	commonValidator
}

var supportedProvisioners = []string{"pd.csi.storage.gke.io"}

var provisionersTypesGCP = []string{"pd-balanced", "pd-ssd", "pd-standard", "pd-extreme"}

func NewGCPValidator() *GCPValidator {
	if gcpInstance == nil {
		gcpInstance = new(GCPValidator)
	}
	return gcpInstance
}

func (v *GCPValidator) DescriptorFile(descriptorFile commons.DescriptorFile) {
	v.descriptor = descriptorFile
}

func (v *GCPValidator) SecretsFile(secrets commons.SecretsFile) {
	v.secrets = secrets
}

func (v *GCPValidator) Validate(fileType string) error {
	switch fileType {
	case "descriptor":
		err := v.descriptorGcpValidations((*v).descriptor)
		if err != nil {
			return err
		}
	case "secrets":
		err := secretsGcpValidations((*v).secrets)
		if err != nil {
			return err
		}
	default:
		return errors.New("Incorrect filetype validation")
	}
	return nil
}

func (v *GCPValidator) CommonsValidations() error {
	err := commonsValidations((*v).descriptor, (*v).secrets)
	if err != nil {
		return err
	}
	return nil
}

func (v *GCPValidator) descriptorGcpValidations(descriptorFile commons.DescriptorFile) error {
	err := commonsDescriptorValidation(descriptorFile)
	if err != nil {
		return err
	}
	err = v.storageClassValidation(descriptorFile)
	if err != nil {
		return err
	}
	return nil
}

func secretsGcpValidations(secretsFile commons.SecretsFile) error {
	err := commonsSecretsValidations(secretsFile)
	if err != nil {
		return err
	}
	return nil
}

func (v *GCPValidator) storageClassValidation(descriptorFile commons.DescriptorFile) error {
	if descriptorFile.StorageClass.EncryptionKey != "" {
		err := v.storageClassKeyFormatValidation(descriptorFile.StorageClass.EncryptionKey)
		if err != nil {
			return errors.New("Error in StorageClass: " + err.Error())
		}
	}

	err := v.storageClassParametersValidation(descriptorFile)
	if err != nil {
		return errors.New("Error in StorageClass: " + err.Error())
	}

	return nil
}

func (v *GCPValidator) storageClassKeyFormatValidation(key string) error {
	regex := regexp.MustCompile(`^projects/[a-zA-Z0-9-]+/locations/[a-zA-Z0-9-]+/keyRings/[a-zA-Z0-9-]+/cryptoKeys/[a-zA-Z0-9-]+$`)
	if !regex.MatchString(key) {
		return errors.New("Incorrect encryptionKey format. It must have the format projects/[PROJECT_ID]/locations/[REGION]/keyRings/[RING_NAME]/cryptoKeys/[KEY_NAME]")
	}
	return nil
}

func (v *GCPValidator) storageClassParametersValidation(descriptorFile commons.DescriptorFile) error {
	sc := descriptorFile.StorageClass
	k8s_version := descriptorFile.K8SVersion
	minor, _ := strconv.Atoi(strings.Split(k8s_version, ".")[1])
	fstypes := []string{"ext4", "ext3", "ext2", "xfs", "ntfs"}
	err := verifyFields(descriptorFile)
	if err != nil {
		return err
	}
	if sc.Parameters.Type != "" && !slices.Contains(provisionersTypesGCP, sc.Parameters.Type) {
		return errors.New("Unsupported type: " + sc.Parameters.Type)
	}
	replicationTypeRegex := regexp.MustCompile(`^(none|regional-pd)$`)
	if sc.Parameters.ReplicationType != "" && !replicationTypeRegex.MatchString(sc.Parameters.ReplicationType) {
		return errors.New("Incorrect replication_type. Supported values are none or regional-pd")
	}
	if sc.Parameters.Type == "pd-extreme" && minor < 26 {
		return errors.New("StorageClass Type pd-extreme is only supported by kubernetes versions v1.26.0 and higher")
	}
	if sc.Parameters.Type != "pd-extreme" && sc.Parameters.ProvisionedIopsOnCreate != "" {
		return errors.New("Parameter provisioned_iops_on_create only can be supported for type pd-extreme")
	}
	if sc.Parameters.FsType != "" && !slices.Contains(fstypes, sc.Parameters.FsType) {
		return errors.New("Unsupported fsType: " + sc.Parameters.FsType + ". Supported types: " + fmt.Sprint(strings.Join(fstypes, ", ")))
	}

	if sc.Parameters.ProvisionedIopsOnCreate != "" {
		_, err = strconv.Atoi(sc.Parameters.ProvisionedIopsOnCreate)
		if err != nil {
			return errors.New("Parameter provisioned_iops_on_create must be an integer")
		}
	}

	if descriptorFile.StorageClass.Parameters.DiskEncryptionKmsKey != "" {
		err := v.storageClassKeyFormatValidation(descriptorFile.StorageClass.Parameters.DiskEncryptionKmsKey)
		if err != nil {
			return errors.New("Error in StorageClass: " + err.Error())
		}
	}

	if sc.Parameters.Labels != "" {
		labels := strings.Split(sc.Parameters.Labels, ",")
		regex := regexp.MustCompile(`^(\w+|.*)=(\w+|.*)$`)
		for _, label := range labels {
			if !regex.MatchString(label) {
				return errors.New("Incorrect labels format. Labels must have the format 'key1=value1,key2=value2'")
			}
		}
	}

	return nil
}
