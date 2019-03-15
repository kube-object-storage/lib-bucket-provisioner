package provisioner

import (
	"time"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/reconciler/util"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
	bucketReconciler "github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/reconciler/bucket-reconciler"
	claimReconciler "github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/reconciler/claim-reconciler"
)

// ProvisionerController is the first iteration of our internal provisioning
// controller.  The passed-in bucket provisioner, coded by the user of the
// library, is stored for later Provision and Delete calls.
type ProvisionerController struct {
	Manager     manager.Manager
	Name        string
	Provisioner api.Provisioner
}

type ProvisionerOptions struct {
	// ProvisionBaseInterval the initial time interval before retrying
	ProvisionBaseInterval time.Duration

	// ProvisionRetryTimeout the maximum amount of time to attempt bucket provisioning.
	// Once reached, the claim key is dropped and re-queued
	ProvisionRetryTimeout time.Duration

	// ProvisionRetryBackoff the base interval multiplier, applied each iteration
	ProvisionRetryBackoff int
}

// NewProvisioner should be called by importers of this library to
// instantiate a new provisioning controller. This controller will
// respond to Add / Update / Delete events by calling the passed-in
// provisioner's Provisioner and Delete methods.
func NewProvisioner(
	cfg *rest.Config,
	provisionerName string,
	provisioner api.Provisioner,
	options *ProvisionerOptions,
) *ProvisionerController {

	klog.Info("Constructing new provisioner: %s", provisionerName)

	var err error

	ctrl := &ProvisionerController{
		Provisioner: provisioner,
		Name:        provisionerName,
	}

	// TODO manage.Options.SyncPeriod may be worth looking at
	//  This determines the minimum period of time objects are synced
	//  This is especially interesting for ObjectBuckets should we decide they should sync with the underlying bucket.
	//  For instance, if the actual bucket is deleted,
	//  we may want to annotate this in the OB after some time
	klog.V(util.DebugLogLvl).Infof("generating controller manager")
	ctrl.Manager, err = manager.New(cfg, manager.Options{})
	if err != nil {
		klog.Fatalf("Error creating controller manager: %v", err)
	}

	if err = apis.AddToScheme(ctrl.Manager.GetScheme()); err != nil {
		klog.Fatalf("Error adding api resources to scheme")
	}

	rc, err := client.New(cfg, client.Options{})
	if err != nil {
		klog.Fatalf("Error generating new client: %v", err)
	}

	// Init ObjectBucketClaim controller.
	// Events for child ConfigMaps and Secrets trigger Reconcile of parent ObjectBucketClaim
	err = builder.ControllerManagedBy(ctrl.Manager).
		For(&v1alpha1.ObjectBucketClaim{}).
		Owns(&v1.ConfigMap{}).
		Owns(&v1.Secret{}).
		Complete(claimReconciler.NewObjectBucketClaimReconciler(rc, provisionerName, provisioner, claimReconciler.Options{
			RetryInterval: options.ProvisionBaseInterval,
			RetryBackoff:  options.ProvisionRetryBackoff,
			RetryTimeout:  options.ProvisionRetryTimeout,
		}))
	if err != nil {
		klog.Fatalf("Error creating ObjectBucketClaim controller: %v", err)
	}

	// TODO retrieve client generated in manager instead of expecting one to be passed in

	// Init ObjectBucket controller
	// TODO I put this here after we decided that OBs should
	//  be Reconciled independently, similar to PVs.  This may
	//  not be what we ultimately want.
	if err = builder.ControllerManagedBy(ctrl.Manager).
		For(&v1alpha1.ObjectBucket{}).
		Complete(&bucketReconciler.ObjectBucketReconciler{Client: rc}); err != nil {
		klog.Fatalf("Error creating ObjectBucket controller: %v", err)
	}

	return ctrl

}

// Run starts the claim and bucket controllers.
func (p *ProvisionerController) Run() {
	klog.Infof("Starting controller for %s provisioner", p.Name)
	go p.Manager.Start(signals.SetupSignalHandler())
}
