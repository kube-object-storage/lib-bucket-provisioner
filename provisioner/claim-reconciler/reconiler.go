package object_bucket_claim_reconciler

import (
	"context"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	. "github.com/yard-turkey/lib-bucket-provisioner/provisioner/reconciler-defaults"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"
)

const (
	provisionerFinalizer string = "object-bucket-claims.lib-bucket-provisioner/finalizer"
)

type ObjectBucketClaimReconciler struct {
	Client            client.Client
	ProvisionerName   string
	SyncOBCInterval   time.Duration
	SyncOBCTimeout    time.Duration
	SyncOBCMaxRetries int
}

func NewDefaultObjectBucketClaimReconciler(c client.Client, name string) *ObjectBucketClaimReconciler {
	return &ObjectBucketClaimReconciler{
		Client:            c,
		ProvisionerName:   name,
		SyncOBCInterval:   DefaultRetryInterval,
		SyncOBCTimeout:    DefaultRetryTimeout,
		SyncOBCMaxRetries: DefaultConditionRetryMax,
	}
}

// TODO this is the guts of our controller.
//   `request` is a 'namespace/name' of a resource.
func (r *ObjectBucketClaimReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {

	obc := &v1alpha1.ObjectBucketClaim{}

	// The controller-runtime client simplifies gets from the cache for object keys (namespace/name strings).  If the object exists, it's copied into `obc`
	// TODO i'm pretty sure this is an API client rather than a cache client, so Get is an API call.  May need to look at making a composite client.
	if err := r.Client.Get(context.TODO(), request.NamespacedName, obc); err != nil {
		return reconcile.Result{}, err
	}

	// TODO react to the event.  If `obc` is a nil struct
	//  it's probably a delete event.
	_ := wait.PollImmediate(
		r.SyncOBCInterval,
		r.SyncOBCTimeout,
		r.reconcileClaim(obc))

	return reconcile.Result{}, nil
}

func (r *ObjectBucketClaimReconciler) reconcileClaim(obc *v1alpha1.ObjectBucketClaim) func() (bool, error) {
	return func() (bool, error) {
		var done bool
		for i := 0; i < r.SyncOBCMaxRetries; i++ {

		}
		return done, nil
	}
}
