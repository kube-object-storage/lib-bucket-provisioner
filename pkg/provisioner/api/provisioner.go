package api

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
)

// Provisioner the interface to be implemented by users of this
// library and executed by the Reconciler
type Provisioner interface {
	// Provision should be implemented to handle bucket creation
	// for the target object store
	Provision(options *BucketOptions) (*v1alpha1.Connection, error)
	// Delete should be implemented to handle bucket deletion
	// for the target object store
	Delete(ob *v1alpha1.ObjectBucket) error
}

type BucketOptions struct {
	// ReclaimPolicy is the reclaimPolicy of the OBC's storage class
	ReclaimPolicy *corev1.PersistentVolumeReclaimPolicy
	// ObjectBucketName is the name of the ObjectBucket API resource
	ObjectBucketName string
	// BucketName is the name of the bucket within the object store
	BucketName string
	// ObjectBucketClaim is a pointer to the initiating OBC object
	ObjectBucketClaim *v1alpha1.ObjectBucketClaim
	// Parameters is a complete copy of the OBC's storage class Parameters field
	Parameters map[string]string
}
