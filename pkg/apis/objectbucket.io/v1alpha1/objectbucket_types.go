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

type AuthSource struct {
	AccessKeys *AccessKeys
}

func (a *AuthSource) ToMap() map[string]string {
	if a == nil {
		return map[string]string{}
	}
	if a.AccessKeys != nil {
		return a.AccessKeys.toMap()
	}
	return map[string]string{}
}

// ObjectBucketSpec defines the desired state of ObjectBucket.
// Fields defined here should be normal among all providers.
// Authentication must be of a type defined in this package to
// pass type checks in reconciler
type ObjectBucketSpec struct {
	StorageClassName string              `json:"storageClassName"`
	ClaimRef         *v1.ObjectReference `json:"claimRef"`
	ReclaimPolicy    ReclaimPolicy       `json:"reclaimPolicy"`
	BucketHost       string              `json:"bucketHost"`
	BucketPort       int                 `json:"bucketPort"`
	Authentication   *AuthSource         `json:"-"`
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

func init() {
	SchemeBuilder.Register(&ObjectBucket{}, &ObjectBucketList{})
}
