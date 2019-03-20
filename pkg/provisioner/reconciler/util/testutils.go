package util

import (
	"fmt"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
)

type FakeProvisioner struct{}

func (p *FakeProvisioner) Provision(options *api.BucketOptions) (connection *v1alpha1.Connection, err error) {
	if options == nil || options.ObjectBucketClaim == nil {
		return nil, fmt.Errorf("got nil ptr")
	}
	return &v1alpha1.Connection{
		Endpoint: &v1alpha1.Endpoint{
			BucketHost: "www.test.com",
			BucketPort: 11111,
			BucketName: options.BucketName,
			Region:     "",
			SubRegion:  "",
			SSL:        false,
		},
		Authentication: &v1alpha1.Authentication{},
	}, nil
}

func (p *FakeProvisioner) Delete(ob *v1alpha1.ObjectBucket) (err error) {
	if ob == nil {
		err = fmt.Errorf("got nil object bucket pointer")
	}
	return err
}
