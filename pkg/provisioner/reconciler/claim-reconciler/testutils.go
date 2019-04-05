package reconciler

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
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

func buildFakeInternalClient(t *testing.T, initObjs ...runtime.Object) *internalClient {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Errorf("error adding core/v1 scheme: %v", err)
	}
	if err := storagev1.AddToScheme(scheme); err != nil {
		t.Errorf("error adding storage/v1 scheme: %v", err)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Errorf("error adding storage/v1 scheme: %v", err)
	}
	return &internalClient{
		Ctx:    context.Background(),
		Client: fake.NewFakeClientWithScheme(scheme, initObjs...),
		Scheme: scheme,
	}
}
