package v1alpha1

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ObjectBucketClaimSpec defines the desired state of ObjectBucketClaim
type ObjectBucketClaimSpec struct {
	StorageClass string
}

type ObjectBucketClaimStatusPhase string

const (
	ObjectBucketClaimStatusPhasePending = "pending"
	ObjectBucketClaimStatusPhaseBound   = "bound"
	ObjectBucketClaimStatusPhaseLost    = "lost"
)

// ObjectBucketClaimStatus defines the observed state of ObjectBucketClaim
type ObjectBucketClaimStatus struct {
	Phase           ObjectBucketClaimStatusPhase
	ObjectBucketRef *v1.ObjectReference
	ConfigMapRef    *v1.ObjectReference
	SecretRef       *v1.SecretReference
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ObjectBucketClaim is the Schema for the objectbucketclaims API
// +k8s:openapi-gen=true
type ObjectBucketClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ObjectBucketClaimSpec   `json:"spec,omitempty"`
	Status ObjectBucketClaimStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ObjectBucketClaimList contains a list of ObjectBucketClaim
type ObjectBucketClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ObjectBucketClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ObjectBucketClaim{}, &ObjectBucketClaimList{})
}
