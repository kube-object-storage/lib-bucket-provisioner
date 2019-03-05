package claimReconciler

import (
	"context"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ObjectBucketClaimReconciler struct {
	client client.Client
}

// TODO this is the guts of our controller.
//   `request` is a 'namespace/name' of a resource.
func (r *ObjectBucketClaimReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {

	obc := &v1alpha1.ObjectBucketClaim{}

	// The controller-runtime client simplifies gets from the cache for object keys (namespace/name strings).  If the object exists, it's copied into `obc`
	if err := r.client.Get(context.TODO(), request.NamespacedName, obc); err != nil {
		return reconcile.Result{}, err
	}
	// TODO react to the event.  If `obc` is a nil struct
	//  it's probably a delete event.

	return reconcile.Result{}, nil
}
