package provisioner

import (
	"flag"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/klog"

	"k8s.io/apimachinery/pkg/api/meta"

	"k8s.io/client-go/rest"
	"k8s.io/klog/klogr"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
	claimReconciler "github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/reconciler/claim-reconciler"
)

// Controller is the first iteration of our internal provisioning
// controller.  The passed-in bucket provisioner, coded by the user of the
// library, is stored for later Provision and Delete calls.
type Controller struct {
	Manager     manager.Manager
	Name        string
	Provisioner api.Provisioner
}

var (
	log  logr.Logger
	logD logr.InfoLogger
)

func initLoggers() {
	log = klogr.New().WithName(api.Domain + "/provisioner-manager")
	logD = log.V(1)
}

func initFlags() {
	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	flag.CommandLine.VisitAll(func(f *flag.Flag) {
		kflag := klogFlags.Lookup(f.Name)
		if kflag != nil {
			val := f.Value.String()
			kflag.Value.Set(val)
		}
	})
	if !flag.Parsed() {
		flag.Parse()
	}
}

// NewProvisioner should be called by importers of this library to
// instantiate a new provisioning controller. This controller will
// respond to Add / Update / Delete events by calling the passed-in
// provisioner's Provisioner and Delete methods.
// The Provisioner will be restrict to operating only to the namespace given
func NewProvisioner(
	cfg *rest.Config,
	provisionerName string,
	provisioner api.Provisioner,
	namespace string,
) (*Controller, error) {

	initFlags()
	initLoggers()

	log.Info("constructing new Provisioner", "name", provisionerName)
	ctrl := &Controller{
		Provisioner: provisioner,
		Name:        provisionerName,
	}

	// TODO manage.Options.SyncPeriod may be worth looking at
	//  This determines the minimum period of time objects are synced
	//  This is especially interesting for ObjectBuckets should we decide they should sync with the underlying bucket.
	//  For instance, if the actual bucket is deleted,
	//  we may want to annotate this in the OB after some time
	log.Info("generating controller manager")
	mgr, err := manager.New(cfg, manager.Options{Namespace: namespace})
	if err != nil {
		return nil, fmt.Errorf("error creating new Manager: %v", err)
	}
	ctrl.Manager = mgr

	log.Info("adding schemes to manager")
	if err = apis.AddToScheme(ctrl.Manager.GetScheme()); err != nil {
		return nil, fmt.Errorf("error adding api resources to scheme: %v", err)
	}

	log.Info("constructing new ObjectBucketClaimReconciler")
	reconciler := claimReconciler.NewObjectBucketClaimReconciler(
		ctrl.Manager.GetClient(),
		ctrl.Manager.GetScheme(),
		provisionerName,
		provisioner,
		claimReconciler.Options{})

	skipUpdate := predicate.Funcs{
		CreateFunc: func(createEvent event.CreateEvent) bool {
			logD.Info("event: Create() ", "Kind", createEvent.Object.GetObjectKind().GroupVersionKind(), "Name", createEvent.Meta.GetName())
			return true
		},
		UpdateFunc: func(updateEvent event.UpdateEvent) bool {
			logD.Info("event: Update (ignored)", "Kind", updateEvent.ObjectNew.GetObjectKind().GroupVersionKind().String(), "Name", updateEvent.MetaNew.GetName())
			return false
		},
		DeleteFunc: func(deleteEvent event.DeleteEvent) bool {
			logD.Info("event: Delete()", "Kind", deleteEvent.Object.GetObjectKind().GroupVersionKind(), "Name", deleteEvent.Meta.GetName())
			return true
		},
		GenericFunc: func(genericEvent event.GenericEvent) bool {
			logD.Info("event: Generic() (ignored)", "Kind", genericEvent.Object.GetObjectKind().GroupVersionKind(), "Name", genericEvent.Meta.GetName())
			return false
		},
	}

	// Init ObjectBucketClaim controller.
	// Events for child ConfigMaps and Secrets trigger Reconcile of parent ObjectBucketClaim
	log.Info("building controller manager")
	err = builder.ControllerManagedBy(ctrl.Manager).
		For(&v1alpha1.ObjectBucketClaim{}).
		WithEventFilter(skipUpdate).
		Complete(reconciler)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return nil, fmt.Errorf("no operator CRDs detected. Create the CRDs before launching provisioner. Error: %v", err)
		}
		return nil, fmt.Errorf("error building controller: %v", err)
	}
	return ctrl, nil
}

// Run starts the claim and bucket controllers.
func (p *Controller) Run() {
	defer klog.Flush()
	log.Info("Starting manager", "provisioner", p.Name)
	go p.Manager.Start(signals.SetupSignalHandler())
}
