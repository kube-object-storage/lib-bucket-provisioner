package object_bucket_claim_reconciler

import (
	"context"
	"fmt"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/provisioner"
	. "github.com/yard-turkey/lib-bucket-provisioner/provisioner/reconciler-defaults"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"
)

const (
	provisionerFinalizer string = "object-bucket-claims.lib-bucket-provisioner/finalizer"
)

type ObjectBucketClaimReconciler struct {
	Client            client.Client
	ProvisionerName   string
	Provisioner       provisioner.Provisioner
	SyncOBCInterval   time.Duration
	SyncOBCTimeout    time.Duration
	SyncOBCMaxRetries int
}

var _ reconcile.Reconciler = &ObjectBucketClaimReconciler{}

func NewDefaultObjectBucketClaimReconciler(c client.Client, name string, provisioner provisioner.Provisioner) *ObjectBucketClaimReconciler {
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
	// TODO handle apierror.NotFound
	if err := r.Client.Get(context.TODO(), request.NamespacedName, obc); err != nil {
		return reconcile.Result{}, err
	}

	///   ///   ///   ///   ///   ///   ///
	// TODO    CAUTION! UNDER CONSTRUCTION!
	///   ///   ///   ///   ///   ///   ///
	err := wait.PollImmediate(
		r.SyncOBCInterval,
		r.SyncOBCTimeout,
		func() (done bool, err error) {
			for i := 0; i < r.SyncOBCMaxRetries, {
				ob, s3Keys, err := r.Provisioner.Provision(obc)
				if err != nil {
					// Provision returned an error, attempt to the delete the bucket if it was created, then
					// exit the wait loop.
					return true, r.Provisioner.Delete(obc)
				}
				if !s3Keys.AreValid() {
					return true, fmt.Errorf("")
				}
				result, err := controllerutil.CreateOrUpdate(context.TODO(), r.Client, ob, func(existing runtime.Object) error {
					return nil
				})
			}
			return true, nil
		})
	if err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

