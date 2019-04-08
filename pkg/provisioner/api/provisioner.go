package api

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
)

// All provisioners must implement the Provisioner interface which defines the
// methods used to create and delete new buckets, and to grant or revoke access
// to buckets within the object store.
type Provisioner interface {
	// Provision should be implemented to handle bucket creation
	Provision(options *BucketOptions) (*v1alpha1.ObjectBucket, error)
	// Grant should be implemented to handle access to existing buckets
	Grant(options *BucketOptions) (*v1alpha1.ObjectBucket, error)
	// Delete should be implemented to handle bucket deletion
	Delete(ob *v1alpha1.ObjectBucket) error
	// Revoke should be implemented to handle removing bucket access
	Revoke(ob *v1alpha1.ObjectBucket) error
}

// BucketOptions wraps all pertinent data that the Provisioner requires to create a
// bucket and the Reconciler requires to abstract that bucket in kubernetes
type BucketOptions struct {
	// ReclaimPolicy is the reclaimPolicy of the OBC's storage class
	ReclaimPolicy *corev1.PersistentVolumeReclaimPolicy
	// BucketName is the name of the bucket within the object store
	BucketName string
	// ObjectBucketClaim is a copy of the reconciler's OBC
	ObjectBucketClaim *v1alpha1.ObjectBucketClaim
	// Parameters is a complete copy of the OBC's storage class Parameters field
	Parameters map[string]string
}
