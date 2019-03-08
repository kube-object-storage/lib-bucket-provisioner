package util

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/api/provisioner"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
)

// NewCredentailsSecret returns a secret with data appropriate to the supported authenticaion method
// Right now, this is just access keys
func NewCredentailsSecret(options *provisioner.BucketOptions, bucket *v1alpha1.ObjectBucket) *v1.Secret {

	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: options.ObjectBucketClaim.Name,
			Namespace:    options.ObjectBucketClaim.Namespace,
			Finalizers:   []string{Finalizer},
		},
	}

	// TODO as we add more authentication methods this switch as well as the functions for processing
	//  the auth data into the secret will be expanded.
	switch bucket.Spec.Authentication.(type) {
	case v1alpha1.AccessKeys:
		secret = secretFromAccessKeys(secret, bucket.Spec.Authentication.(v1alpha1.AccessKeys))
	}

	return secret
}

func secretFromAccessKeys(base *v1.Secret, key v1alpha1.AccessKeys) *v1.Secret {
	base.StringData[BucketAccessKey] = key.AccessKeyId
	base.StringData[BucketSecretKey] = key.SecretAccessKey
	return base
}
