package v1alpha1

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ReclaimPolicy string

const (
	ReclaimPolicyDelete ReclaimPolicy = "delete"
	ReclaimPolicyRetain ReclaimPolicy = "retain"
)

type mapper interface {
	toMap() map[string]string
}

const (
	AwsKeyField    = "AWS_ACCESS_KEY_ID"
	AwsSecretField = "AWS_SECRET_ACCESS_KEY"
)

// AccessKeys is an Authentication type for passing AWS S3 style
// key pairs from the provisioner to the reconciler.
type AccessKeys struct {
	AccessKeyId     string `json:"-"`
	SecretAccessKey string `json:"-"`
}

func (ak *AccessKeys) toMap() map[string]string {
	return map[string]string{
		AwsKeyField:    ak.AccessKeyId,
		AwsSecretField: ak.SecretAccessKey,
	}
}

// Authentication wraps all supported auth types.  The design choice enables expansion of supported
// types while protecting backwards compatability.
type Authentication struct {
	AccessKeys *AccessKeys `json:"-"`
}

// ToMap converts the any defined authentication type into a map[string]string for writing to
// a Secret.StringData field
func (a *Authentication) ToMap() map[string]string {
	if a == nil {
		return map[string]string{}
	}
	if a.AccessKeys != nil {
		return a.AccessKeys.toMap()
	}
	return map[string]string{}
}

// Endpoint contains all connection relevant data that an app may require for accessing
// the bucket
type Endpoint struct {
	BucketHost string `json:"bucketHost"`
	BucketPort int    `json:"bucketPort"`
	BucketName string `json:"bucketName"`
	Region     string `json:"region"`
	SubRegion  string `json:"subRegion"`
	SSL        bool   `json:"ssl"`
}

// Connection encapsulates Endpoint and Authentication data to simplify the expected return values of the
// Provision() interface method.  This makes it more clear to library consumers what specific values
// they should return from their Provisioner interface implementation.
type Connection struct {
	Endpoint       *Endpoint       `json:"endpoint"`
	Authentication *Authentication `json:"-"`
}

// ObjectBucketSpec defines the desired state of ObjectBucket.
// Fields defined here should be normal among all providers.
// Authentication must be of a type defined in this package to
// pass type checks in reconciler
type ObjectBucketSpec struct {
	StorageClassName string              `json:"storageClassName"`
	ReclaimPolicy    ReclaimPolicy       `json:"reclaimPolicy"`
	ClaimRef         *v1.ObjectReference `json:"claimRef"`
	*Connection      `json:"Connection"`
}

type ObjectBucketStatusPhase string

const (
	ObjectBucketStatusPhaseBound    ObjectBucketStatusPhase      = "bound"
	ObjectBucketStatusPhaseReleased ObjectBucketStatusPhase      = "released"
	ObjectBucketStatusPhaseFailed   ObjectBucketClaimStatusPhase = "failed"
)

// ObjectBucketStatus defines the observed state of ObjectBucket
type ObjectBucketStatus struct {
	Phase      ObjectBucketClaimStatusPhase `json:"phase"`
	Conditions v1.ConditionStatus           `json:"conditions"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true

// ObjectBucket is the Schema for the objectbuckets API
type ObjectBucket struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ObjectBucketSpec   `json:"spec,omitempty"`
	Status ObjectBucketStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ObjectBucketList contains a list of ObjectBucket
type ObjectBucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ObjectBucket `json:"items"`
}
