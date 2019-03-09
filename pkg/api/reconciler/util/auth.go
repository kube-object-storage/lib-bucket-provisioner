package util

import (
	"fmt"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/api/provisioner"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
)

// NewCredentailsSecret returns a secret with data appropriate to the supported authenticaion method
// Right now, this is just access keys
func NewCredentailsSecret(opts *provisioner.BucketOptions, ob *v1alpha1.ObjectBucket) (*v1.Secret, error) {

	if opts == nil {
		return nil, fmt.Errorf("BucketOptions required to secret generation")
	}
	if opts.ObjectBucketClaim == nil {
		return nil, fmt.Errorf("ObjectBucketClaim required to generate secret")
	}
	if ob == nil {
		return nil, fmt.Errorf("ObjectBucket required to generate secret")
	}

	klog.V(DebugLogLvl).Infof("generating new secret for ObjectBucketClaim \"%s/%s\"", opts.ObjectBucketClaim.Namespace, opts.ObjectBucketClaim.Name)

	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:       opts.ObjectBucketClaim.Name,
			Namespace:  opts.ObjectBucketClaim.Namespace,
			Finalizers: []string{Finalizer},
		},
	}

	secret.StringData = ob.Spec.Authentication.ToMap()
	if len(secret.StringData) == 0 {
		// The provisioner may not have deliberately provided credentials, just log a warning
		klog.Warningf("objectBucket %q has no authentication credentials defined", ob.Name)
	}
	return secret, nil
}
