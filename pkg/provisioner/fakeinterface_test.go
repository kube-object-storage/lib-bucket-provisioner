package provisioner

import (
	"fmt"
	// "sigs.k8s.io/Controller-runtime/pkg/client/fake"

	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/provisioner/api"
)

type fakeProvisioner struct{}

var _ api.Provisioner = &fakeProvisioner{}

// Provision provides a simple method for testing purposes
func (p *fakeProvisioner) Provision(options *api.BucketOptions) (*v1alpha1.ObjectBucket, error) {
	if options == nil || options.ObjectBucketClaim == nil {
		return nil, fmt.Errorf("got nil ptr")
	}
	return &v1alpha1.ObjectBucket{}, nil
}

// Grant provides a simple method for testing purposes
func (p *fakeProvisioner) Grant(options *api.BucketOptions) (*v1alpha1.ObjectBucket, error) {
	if options == nil || options.ObjectBucketClaim == nil {
		return nil, fmt.Errorf("got nil ptr")
	}
	return &v1alpha1.ObjectBucket{}, nil
}

// Delete provides a simple method for testing purposes
func (p *fakeProvisioner) Delete(ob *v1alpha1.ObjectBucket) (err error) {
	if ob == nil {
		err = fmt.Errorf("got nil object bucket pointer")
	}
	return err
}

// Revoke provides a simple method for testing purposes
func (p *fakeProvisioner) Revoke(ob *v1alpha1.ObjectBucket) (err error) {
	if ob == nil {
		err = fmt.Errorf("got nil object bucket pointer")
	}
	return err
}
