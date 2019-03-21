package test

import (
	"testing"

	"github.com/spf13/pflag"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	lib "github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
)

var (
	masterURL      string
	kubeConfigPath string
)

func init() {
	pflag.StringVarP(&masterURL, "masterUrl", "u", "", "")
	pflag.StringVarP(&kubeConfigPath, "kubeconfig", "k", "", "")
	if !pflag.Parsed() {
		pflag.Parse()
	}
}

type dummyProvidioner struct {
}

func (p *dummyProvidioner) Provision(options *api.BucketOptions) (*v1alpha1.ObjectBucket, error) {
	return &v1alpha1.ObjectBucket{}, nil
}

func (p *dummyProvidioner) Delete(ob *v1alpha1.ObjectBucket) error {
	return nil
}

func TestProvisioner(t *testing.T) {

	const provName = "dummy-provisioner"

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeConfigPath)
	if err != nil {
		t.Fatalf("failed to generate config from path %q: %v", kubeConfigPath, err)
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("failed to generate client from config: %v", err)
	}

	version, err := client.ServerVersion()
	if err != nil {
		t.Fatalf("failed getting server version: %v", err)
	}

	prov := lib.NewProvisioner(cfg, provName, &dummyProvidioner{}, version.String(), &lib.ProvisionerOptions{
		ProvisionBaseInterval: 0,
		ProvisionRetryTimeout: 0,
		ProvisionRetryBackoff: 0,
	})
	var _ = prov
}
