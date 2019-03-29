package util

import (
	"fmt"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
)

type FakeProvisioner struct{}

var _ api.Provisioner = &FakeProvisioner{}

func (p *FakeProvisioner) Provision(options *api.BucketOptions) (*v1alpha1.ObjectBucket, error) {
	if options == nil || options.ObjectBucketClaim == nil {
		return nil, fmt.Errorf("got nil ptr")
	}
	return &v1alpha1.ObjectBucket{}, nil
}

func (p *FakeProvisioner) Delete(ob *v1alpha1.ObjectBucket) (err error) {
	if ob == nil {
		err = fmt.Errorf("got nil object bucket pointer")
	}
	return err
}
