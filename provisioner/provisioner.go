package provisioner

import (
	"github.com/go-logr/logr"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	claimReconciler "github.com/yard-turkey/lib-bucket-provisioner/provisioner/claim-reconciler"
	bucketReconciler "github.com/yard-turkey/lib-bucket-provisioner/provisioner/object-bucket-reconciler"
	"k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"time"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var log = logr.Logger.WithName()

// Provisioner the interface to be implemented by users of this
// library and executed by the Reconciler
type Provisioner interface {
	// Provision should be implemented to handle bucket creation
	// for the target object store
	Provision(*v1alpha1.ObjectBucketClaim) (*v1alpha1.ObjectBucket, *S3AccessKeys, error)
	// Delete should be implemented to handle bucket deletion
	// for the target object store
	Delete(claim *v1alpha1.ObjectBucketClaim) error
}

type S3AccessKeys struct {
	AccessKey, SecretKey string
}

func (k *S3AccessKeys)AreValid() bool {
	return k.AccessKey != "" && k.SecretKey != ""
}

func (k *S3AccessKeys)ToSecret(keys *S3AccessKeys) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: *metav1.ObjectMeta{}
	}
}

// TODO enable user configuration + NewDefault func + option struct
// bucketProvisionerController is the first iteration of our internal
// provisioning controller.  The passed-in bucket provisioner,
// coded by the user of the library, is stored for later
// Provision and Delete calls.
type bucketProvisionerController struct {
	// provisionerName should exactly match the `provisioner:` field of the storageClass(es)
	// respective of this controller
	provisionerName string
	manager         manager.Manager
	provisioner     Provisioner
	// threadiness is the amount of Reconciler routines per Controller
	threadiness int
	// retryBackoff the rate at which Provision and Delete retries are executed
	retryBackoff time.Duration
	// retryMax the upper limit on retries before dropping the key for re-enqueing
	retryMax int
}

// NewProvisioner should be called by importers of this library to
// instantiate a new provisioning controller. This controller will
// respond to Add / Update / Delete events by calling the passed-in
// provisioner's Provisioner and Delete methods.
func NewProvisioner(cfg *rest.Config, provisionerName string, provisioner Provisioner) (*bucketProvisionerController, error) {

	var err error

	c := &bucketProvisionerController{
		provisionerName: provisionerName,
		provisioner:     provisioner,
	}

	// TODO manage.Options.SyncPeriod may be worth looking at
	//  This determines the minimum period of time objects are synced
	//  This is especially interesting for ObjectBuckets should we decide they should sync with the underlying bucket.
	//  For instance, if the actual bucket is deleted,
	//  we may want to annotate this in the OB after some time
	c.manager, err = manager.New(cfg, manager.Options{})
	if err != nil {
		return nil, err
	}

	// TODO (jon) I'm PRETTY sure this is necessary to enable
	//  watches of CRDs.  Needs to be verified.
	if err = apis.AddToScheme(c.manager.GetScheme()); err != nil {
		return nil, err
	}

	rc, err := client.New(cfg, client.Options{})
	if err != nil {
		return nil, err
	}

	// Init ObjectBucketClaim controller.
	// Events for child ConfigMaps and Secrets trigger Reconcile of parent ObjectBucketClaim
	if err := builder.ControllerManagedBy(c.manager).
		For(&v1alpha1.ObjectBucketClaim{}).
		Owns(&v1.ConfigMap{}).
		Owns(&v1.Secret{}).
		Complete(claimReconciler.NewDefaultObjectBucketClaimReconciler(rc, provisionerName, provisioner));
		err != nil {
		return nil, err
	}

	// Init ObjectBucket controller
	// TODO I put this here after we decided that OBs should
	//  be Reconciled independently, similar to PVs.  This may
	//  not be what we ultimately want.
	if err = builder.ControllerManagedBy(c.manager).
		For(&v1alpha1.ObjectBucket{}).
		Complete(&bucketReconciler.ObjectBucketReconciler{rc}); err != nil {
		return nil, err
	}

	return c, nil

}

// Run starts the claim and bucket controllers.
func (p *bucketProvisionerController) Run() {
	// TODO this seems like it's too high level to start the go thread but I don't see
	//  how to do it within the manager or controller.
	for i := 0; i < p.threadiness; i++ {
		go p.manager.Start(signals.SetupSignalHandler())
	}
}
