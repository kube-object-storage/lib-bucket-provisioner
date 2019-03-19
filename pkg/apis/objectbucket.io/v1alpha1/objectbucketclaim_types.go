package v1alpha1

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type BucketCannedACL string

const (
	BucketCannedACLPrivate           BucketCannedACL = "private"
	BucketCannedACLPublicRead        BucketCannedACL = "public-read"
	BucketCannedACLPublicReadWrite   BucketCannedACL = "public-read-write"
	BucketCannedACLAuthenticatedRead BucketCannedACL = "authenticated-read"
)

// ObjectBucketClaimSpec defines the desired state of ObjectBucketClaim
type ObjectBucketClaimSpec struct {
	// StorageClass names the StorageClass object representing the desired provisioner and parameters
	StorageClassName string
	// BucketName (not recommended) the name of the bucket.  Caution!
	// In-store bucket names may collide across namespaces.  If you define
	// the name yourself, try to make it as unique as possible.
	BucketName string `json:"bucketName,omityempty"`
	// GenerateBucketName (recommended) a prefix for a bucket name to be
	// followed by a hyphen and 5 random characters. Protects against
	// in-store name collisions.
	GeneratBucketName string `json:"generateBucketName,omitempty"`
	// SSL whether connection to the bucket requires SSL Authentication or not
	SSL bool `json:"ssl"`
	// Generic predefined bucket ACLs for use by provisioners
	// Available BucketCannedACLs are:
	//    BucketCannedACLPrivate
	//    BucketCannedACLPublicRead
	//    BucketCannedACLPublicReadWrite
	//    BucketCannedACLAuthenticatedRead
	BucketCannedACL BucketCannedACL `json:"cannedBucketAcl"`
	// Versioned determines if versioning is enabled
	Versioned bool `json:"versioned"`
	// AdditionalConfig gives providers a location to set
	// proprietary config values (tenant, namespace, etc)
	AdditionalConfig map[string]string `json:"additionalConfig"`
}

type ObjectBucketClaimStatusPhase string

const (
	ObjectBucketClaimStatusPhasePending  = "pending"
	ObjectBucketClaimStatusPhaseBound    = "bound"
	ObjectBucketClaimStatusPhaseReleased = "released"
	ObjectBucketClaimStatusPhaseFailed   = "failed"
)

// ObjectBucketClaimStatus defines the observed state of ObjectBucketClaim
type ObjectBucketClaimStatus struct {
	Phase           ObjectBucketClaimStatusPhase
	ObjectBucketRef *v1.ObjectReference
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true

// ObjectBucketClaim is the Schema for the objectbucketclaims API
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
