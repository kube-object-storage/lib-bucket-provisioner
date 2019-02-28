package v1alpha1

import (
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ObjectBucketSpec defines the desired state of ObjectBucket.
// Fields defined here should be normal among all S3 providers.
type ObjectBucketSpec struct {
	// BucketName the base name of the bucket
	BucketName string `json:"bucketName"`
	// Host the host URL of the object store with
	Host string `json:"host"`
	// Region the region of the bucket within an object store
	Region string `json:"region"`
	// Port the insecure port number of the object store, if it exists
	Port int `json:"port"`
	// SecurePort the secure port number of the object store, if it exists
	SecurePort int `json:"securePort"`
	// SSL true if the connection is secured with SSL, false if it is not.
	SSL bool `json:"ssl"`
	// BucketACL
	CannedBucketACL s3.BucketCannedACL `json:"bucketAcl"`
	// Versioned true if the object store support versioned buckets, false if not
	Versioned bool `json:"versioned,omitempty"`
	// ProviderConfig is map for non-AWS providers to set non-standard configs (tenant, namespace, etc.)
	ProviderConfig map[string]interface{} `json:"providerConfig,omitempty"`
}

type ObjectBucketStatusPhase string

const (
	ObjectBucketStatusPhasePending ObjectBucketStatusPhase = "pending"
	ObjectBucketStatusPhaseBound   ObjectBucketStatusPhase = "bound"
	ObjectBucketStatusPhaseLost    ObjectBucketStatusPhase = "lost"
)

// ObjectBucketStatus defines the observed state of ObjectBucket
type ObjectBucketStatus struct {
	Controller *v1.LocalObjectReference
	Phase      ObjectBucketClaimStatusPhase
	Conditions v1.ConditionStatus
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
