package v1alpha1

import (
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ObjectBucketClaimSpec defines the desired state of ObjectBucketClaim
type ObjectBucketClaimSpec struct {
	// StorageClass names the StorageClass object representing the desired provisioner and parameters
	StorageClass string
	// BucketName (not recommended) the name of the bucket.  Caution!
	// In-store bucket names may collide across namespaces.  If you define
	// the name yourself, try to make it as unique as possible.
	BucketName string `json:"bucketName,omityempty"`
	// GenerateBucketName (recommended) a prefix for a bucket name to be
	// followed by a hyphen and 5 random characters. Protects against
	// in-store name collisions.
	GeneratBucketName string `json:"generatBucketName,omitempty"`
	// SSL whether connection to the bucket requires SSL authentication or not
	SSL bool `json:"ssl"`
	// AWS S3 predefined bucket ACLs.
	// Available BucketCannedACLs are:
	//    BucketCannedACLPrivate
	//    BucketCannedACLPublicRead
	//    BucketCannedACLPublicReadWrite
	//    BucketCannedACLAuthenticatedRead
	CannedBucketACL s3.BucketCannedACL `json:"cannedBucketAcl"`
	// Versioned determines if versioning is enabled
	Versioned bool `json:"versioned"`
	// AdditionalConfig gives non-AWS S3 providers a location to set
	// proprietary config values (tenant, namespace, etc)
	AdditionalConfig map[string]interface{} `json:"additionalConfig"`
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
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
// +genClient

// ObjectBucketClaim is the Schema for the objectbucketclaims API
type ObjectBucketClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ObjectBucketClaimSpec   `json:"spec,omitempty"`
	Status ObjectBucketClaimStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genClient

// ObjectBucketClaimList contains a list of ObjectBucketClaim
type ObjectBucketClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ObjectBucketClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ObjectBucketClaim{}, &ObjectBucketClaimList{})
}
