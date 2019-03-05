package provisioner

import (
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	reconciler "github.com/yard-turkey/lib-bucket-provisioner/provisioner/provisioner/claim-reconciler"
	"k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type S3AccessKeys struct {
	AccessKey, SecretKey string
}

// Provisioner the interface to be implemented by users of this
// library and executed by the Reconciler
type Provisioner interface {
	// Provision should be implemented to handle bucket creation
	// for the target object store
	Provision(*v1alpha1.ObjectBucketClaim) (*v1alpha1.ObjectBucketClaim, *S3AccessKeys, error)
	// Delete should be implemented to handle bucket deletion
	// for the target object store
	Delete(claim *v1alpha1.ObjectBucketClaim) error
}

// bucketProvisionerController is the first iteration of our internal
// provisioning controller.  The passed-in bucket provisioner,
// coded by the user of the library, is stored for later
// Provision and Delete calls.
type bucketProvisionerController struct {
	manager     manager.Manager
	client      client.Client
	provisioner Provisioner
}

// NewProvisioner should be called by importers of this library to
// instantiate a new provisioning controller. This controller will
// respond to Add / Update / Delete events by calling the passed-in
// provisioner's Provisioner and Delete methods.
func NewProvisioner(client client.Client, cfg *rest.Config, provisioner Provisioner) (*bucketProvisionerController, error) {

	c := &bucketProvisionerController{
		provisioner: provisioner,
		client:      client,
	}

	// TODO manage.Options.SyncPeriod may be worth looking at
	//  This determines the minimum period of time objects are synced
	//  This is especially interesting for ObjectBuckets should we decide they should sync with the underlying bucket.
	//  For instance, if the actual bucket is deleted,
	//  we may want to annotate this in the OB after some time
	mgr, err := manager.New(cfg, manager.Options{})
	if err != nil {
		return nil, err
	}

	// TODO (jon) I'm PRETTY sure this is necessary to enable
	//  watches of CRDs.  Needs to be verified.
	err = apis.AddToScheme(mgr.GetScheme())
	if err != nil {
		return nil, err
	}

	// Init ObjectBucketClaim controller.
	// Events for child ConfigMaps and Secrets trigger Reconcile of parent ObjectBucketClaim
	err = builder.ControllerManagedBy(mgr).
		For(&v1alpha1.ObjectBucketClaim{}).
		Owns(&v1.ConfigMap{}).
		Owns(&v1.Secret{}).
		Complete(&reconciler.ObjectBucketClaimReconciler{})

	// Init ObjectBucket controller
	// TODO I put this here after we decided that OBs should
	//  be Reconciled independently, similar to PVs.  This may
	//  not be what we ultimately want.
	err = builder.ControllerManagedBy(mgr).
		For(&v1alpha1.ObjectBucket{}).Complete()

	c.manager = mgr

	return c, nil

}

func (p *bucketProvisionerController) Run() error {
	err := p.manager.Start(signals.SetupSignalHandler())
	if err != nil {
		return err
	}
	return nil
}
