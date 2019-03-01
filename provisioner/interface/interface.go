package _interface

import (
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/provisioner/auth"
)

// Provisioner the interface to be implemented by users of this
// library and executed by the Reconciler
type Provisioner interface {
	// Provision should be implemented to handle bucket creation
	// for the target object store
	Provision(*v1alpha1.ObjectBucketClaim) (*v1alpha1.ObjectBucket, *auth.S3AccessKeys, error)
	// Delete should be implemented to handle bucket deletion
	// for the target object store
	Delete(claim *v1alpha1.ObjectBucketClaim) error
}
