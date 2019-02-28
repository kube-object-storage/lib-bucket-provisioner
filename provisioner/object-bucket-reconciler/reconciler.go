package reconciler

import (
	"context"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ObjectBucketReconciler struct {
	client client.Client
}

// TODO if we decide that OBs should have their own Reconiler, then we can work out
//  the logic for that here.  If not, this package can be deleted.
func (r ObjectBucketReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {

	ob := &v1alpha1.ObjectBucket{}

	if err := r.client.Get(context.TODO(), request.NamespacedName, ob); err != nil {
		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, nil
}