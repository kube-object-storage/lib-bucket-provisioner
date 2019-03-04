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

// ObjectBucketSpec defines the desired state of ObjectBucket.
// Fields defined here should be normal among all S3 providers.
type ObjectBucketSpec struct {
	StorageClassName string
	ClaimRef         *v1.ObjectReference
	ReclaimPolicy    ReclaimPolicy
	BucketHost       string
	BucketPort       int
}

type ObjectBucketStatusPhase string

const (
	ObjectBucketStatusPhasePending  ObjectBucketStatusPhase      = "pending"
	ObjectBucketStatusPhaseBound    ObjectBucketStatusPhase      = "bound"
	ObjectBucketStatusPhaseReleased ObjectBucketStatusPhase      = "released"
	ObjectBucketStatusPhaseFailed   ObjectBucketClaimStatusPhase = "failed"
)

// ObjectBucketStatus defines the observed state of ObjectBucket
type ObjectBucketStatus struct {
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
