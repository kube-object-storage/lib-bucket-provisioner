package object_bucket_claim_reconciler

import (
	"context"
	"fmt"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/provisioner"
	"k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"
	"time"
)

type objectBucketClaimReconciler struct {
	client          client.Client
	provisionerName string
	provisioner     provisioner.Provisioner
	retryInterval   time.Duration
	retryTimeout    time.Duration
	retryBackoff    int
}

var _ reconcile.Reconciler = &objectBucketClaimReconciler{}

type ReconcilerOptions struct {
	RetryInterval time.Duration
	RetryTimeout  time.Duration
	RetryBackoff  int
}

func NewObjectBucketClaimReconciler(c client.Client, name string, provisioner provisioner.Provisioner, options ReconcilerOptions) *objectBucketClaimReconciler {
	return &objectBucketClaimReconciler{
		client:          c,
		provisionerName: strings.ToLower(name),
		provisioner:     provisioner,
		retryInterval:   options.RetryInterval,
		retryTimeout:    options.RetryTimeout,
		retryBackoff:    options.RetryBackoff,
	}
}

// TODO this is the guts of our controller.
//   `request` is a 'namespace/name' of a resource.
func (r *objectBucketClaimReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// /   ///   ///   ///   ///   ///   ///
	// TODO    CAUTION! UNDER CONSTRUCTION!
	// /   ///   ///   ///   ///   ///   ///
	// This process should basically
	// 1. get the claim resource
	// 2. get the class for the claim
	// 3. verify the class.provisioner matches this provisioner name
	// 4. attempt to provision the bucket
	// 5. create the OB returned from Provision()
	// 6. create the secret and config map in the OBC namespace

	obc := &v1alpha1.ObjectBucketClaim{}

	if err := r.client.Get(context.TODO(), request.NamespacedName, obc); err != nil {
		return reconcile.Result{}, err
	}

	claimClass := obc.Spec.StorageClass

	if !r.shouldProvision(claimClass) {
		return reconcile.Result{}, fmt.Errorf("Unrecognized storageClass.provisioner name")
	}

	// TODO wrap in retry logic
	ob, s3keys, err := r.provisioner.Provision(obc)
	if err != nil {
		return reconcile.Result{}, err
	}
	if s3keys.AreEmpty() {
		msg := "s3 access key or secret key is nil"
		err = r.provisioner.Delete(obc)
		if err != nil {
			msg = fmt.Sprintf("%s, error deleting bucket: %v", msg, err)
		}
		return reconcile.Result{}, fmt.Errorf("%v", err)
	}

	// TODO wap in retry logic
	if err = r.client.Create(context.TODO(), ob); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// A simplistic check on whether this obc is a concern for this provisioner.  Likely to need
// a more complex check down the road
func (r *objectBucketClaimReconciler) shouldProvision(class string) bool {
	return class == "" && class != r.provisionerName
}

func (r *objectBucketClaimReconciler) classFromClaim(obc *v1alpha1.ObjectBucketClaim) (*v1.StorageClass, error) {
	sc := &v1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: obc.Spec.StorageClass,
		},
	}
	scKey, err := client.ObjectKeyFromObject(sc)
	if err != nil {
		return nil, fmt.Errorf("could not get storage class key: %v", err)
	}
	err = r.client.Get(context.TODO(), scKey, sc)
	if err != nil {
		return nil, fmt.Errorf("could not get storage class: %v", err)
	}
	return sc, nil
}
