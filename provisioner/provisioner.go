package provisioner

import (
	"fmt"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	claimReconciler "github.com/yard-turkey/lib-bucket-provisioner/provisioner/claim-reconciler"
	bucketReconciler "github.com/yard-turkey/lib-bucket-provisioner/provisioner/object-bucket-reconciler"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"strings"
	"time"
)

const (
	ProvisionerNamePrefix    = "object-bucket-claims.lib-bucket-provisioner/"
	DefaultRetryBaseInterval = time.Second * 10
	DefaultRetryTimeout      = time.Second * 360
	DefaultRetryBackOff      = 1
	DefaultThreadiness       = 1
)

// provisionerController is the first iteration of our internal provisioning
// controller.  The passed-in bucket provisioner, coded by the user of the
// library, is stored for later Provision and Delete calls.
type provisionerController struct {
	manager     manager.Manager
	provisioner Provisioner
	threads     int
}

type ProvisionerOptions struct {
	// Threadiness is the amount of Reconciler routines per Controller
	Threadiness int

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
func NewProvisioner(cfg *rest.Config, provisionerName string, provisioner Provisioner, kubeVersion string, options *ProvisionerOptions) (*provisionerController, error) {

	var err error

	if nameErrors := isValidProvisionerName(provisionerName, field.NewPath("provisioner")); len(nameErrors) > 0 {
		return nil, fmt.Errorf("invalid provisioner name %q: %v", provisionerName, nameErrors)
	}

	if options.Threadiness < DefaultThreadiness {
		options.Threadiness = DefaultThreadiness
	}
	if options.ProvisionBaseInterval < DefaultRetryBaseInterval {
		options.ProvisionBaseInterval = DefaultRetryBaseInterval
	}
	if options.ProvisionRetryTimeout < DefaultRetryTimeout {
		options.ProvisionRetryTimeout = DefaultRetryTimeout
	}
	if options.ProvisionRetryBackoff < DefaultRetryBackOff {
		options.ProvisionRetryBackoff = DefaultRetryBackOff
	}

	ctrl := &provisionerController{
		provisioner: provisioner,
		threads:     options.Threadiness,
	}

	// TODO manage.ReconcilerOptions.SyncPeriod may be worth looking at
	//  This determines the minimum period of time objects are synced
	//  This is especially interesting for ObjectBuckets should we decide they should sync with the underlying bucket.
	//  For instance, if the actual bucket is deleted,
	//  we may want to annotate this in the OB after some time
	ctrl.manager, err = manager.New(cfg, manager.Options{})
	if err != nil {
		return nil, err
	}

	// TODO (jon) I'm PRETTY sure this is necessary to enable
	//  watches of CRDs.  Needs to be verified.
	if err = apis.AddToScheme(ctrl.manager.GetScheme()); err != nil {
		return nil, err
	}

	rc, err := client.New(cfg, client.Options{})
	if err != nil {
		return nil, err
	}

	// Init ObjectBucketClaim controller.
	// Events for child ConfigMaps and Secrets trigger Reconcile of parent ObjectBucketClaim
	err = builder.ControllerManagedBy(ctrl.manager).
		For(&v1alpha1.ObjectBucketClaim{}).
		Owns(&v1.ConfigMap{}).
		Owns(&v1.Secret{}).
		Complete(claimReconciler.NewObjectBucketClaimReconciler(rc, provisionerName, provisioner, claimReconciler.ReconcilerOptions{
			RetryInterval: options.ProvisionBaseInterval,
			RetryBackoff:  options.ProvisionRetryBackoff,
			RetryTimeout:  options.ProvisionRetryTimeout,
		}))
	if err != nil {
		return nil, err
	}

	// Init ObjectBucket controller
	// TODO I put this here after we decided that OBs should
	//  be Reconciled independently, similar to PVs.  This may
	//  not be what we ultimately want.
	if err = builder.ControllerManagedBy(ctrl.manager).
		For(&v1alpha1.ObjectBucket{}).
		Complete(&bucketReconciler.ObjectBucketReconciler{rc}); err != nil {
		return nil, err
	}

	return ctrl, nil

}

// Run starts the claim and bucket controllers.
func (p *provisionerController) Run() {
	// TODO this seems like it's too high level to start the go thread but I don't see
	//  how to do it within the manager or controller.
	for i := 0; i < p.threads; i++ {
		go p.manager.Start(signals.SetupSignalHandler())
	}
}

func isValidProvisionerName(n string, path *field.Path) field.ErrorList {
	errList := field.ErrorList{}
	if n == "" {
		errList = append(errList, field.Required(path, "Name"))
	}
	for _, err := range validation.IsQualifiedName(strings.ToLower(n)) {
		errList = append(errList, field.Invalid(path, n, err))
	}
	return errList
}
