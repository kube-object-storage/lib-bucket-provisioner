package reconciler

import (
	"context"
	"fmt"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/provisioner/auth"
	"github.com/yard-turkey/lib-bucket-provisioner/provisioner/provisioner"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strconv"
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

const (
	defaultRetryBaseInterval = time.Second * 10
	defaultRetryTimeout      = time.Second * 360
	defaultRetryBackOff      = 1

	finalizer = "objectbucket.io/finalizer"

	// Fields Names
	bucketName      = "S3_BUCKET_NAME"
	bucketHost      = "S3_BUCKET_HOST"
	bucketPort      = "S3_BUCKET_PORT"
	bucketAccessKey = "S3_BUCKET_ACCESS_KEY_ID"
	bucketSecretKey = "S3_BUCKET_SECRET_KEY"
	bucketURL       = "S3_BUCKET_URL"
)

type Options struct {
	RetryInterval time.Duration
	RetryTimeout  time.Duration
	RetryBackoff  int
}

func NewObjectBucketClaimReconciler(c client.Client, name string, provisioner provisioner.Provisioner, options Options) *objectBucketClaimReconciler {
	if options.RetryInterval < defaultRetryBaseInterval {
		options.RetryInterval = defaultRetryBaseInterval
	}
	if options.RetryTimeout < defaultRetryTimeout {
		options.RetryTimeout = defaultRetryTimeout
	}
	if options.RetryBackoff < defaultRetryBackOff {
		options.RetryBackoff = defaultRetryBackOff
	}
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
//   `request` is a 'namespace/name' object key.
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

	claimClass, err := r.classFromClaim(obc)
	if err != nil {
		return reconcile.Result{}, err
	}

	if !r.shouldProvision(claimClass) {
		return reconcile.Result{}, fmt.Errorf("claim failed shouldProvision check")
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

	// TODO wrap in retry logic
	err = wait.PollImmediate(r.retryInterval, r.retryTimeout, func() (done bool, err error) {
		if err = r.client.Create(context.TODO(), ob); err != nil {
			if apierrs.IsAlreadyExists(err) {
				return true, fmt.Errorf("object bucket %s already exists: %v", ob.Name, err)
			} else {
				return true, fmt.Errorf("object bucket %s creation failed: %v", ob.Name, err)
			}
		}
		return true, nil
	})

	credSecret := newCredentailsSecret(obc, s3keys)
	err = wait.PollImmediate(r.retryInterval, r.retryTimeout, func() (done bool, err error) {
		if err = r.client.Create(context.TODO(), credSecret); err != nil {
			if apierrs.IsAlreadyExists(err) {
				return true, fmt.Errorf("secret %s/%s already exists: %v", credSecret.Namespace, credSecret.Name, err)
			} else {
				return true, fmt.Errorf("secret %s/%s creation failed: %v", err)
			}
		}
		return true, nil
	})

	bucketConfigMap := newBucketConfigMap(ob, obc)
	err = wait.PollImmediate(r.retryInterval, r.retryTimeout, func() (done bool, err error) {
		if err = r.client.Create(context.TODO(), bucketConfigMap); err != nil {
			if apierrs.IsAlreadyExists(err) {
				return true, fmt.Errorf("configMap %s/%s already exists: %v", bucketConfigMap.Namespace, bucketConfigMap.Name, err)
			} else {
				return true, fmt.Errorf("configMap %s/%s creation failed: %v", err)
			}
		}
		return true, nil
	})

	return reconcile.Result{}, nil
}

// A simplistic check on whether this obc is a concern for this provisioner.  Down the road, this will perform a broader
// set of checks.
func (r *objectBucketClaimReconciler) shouldProvision(class *storagev1.StorageClass) bool {
	return class.Provisioner == r.provisionerName
}

func (r *objectBucketClaimReconciler) classFromClaim(obc *v1alpha1.ObjectBucketClaim) (*storagev1.StorageClass, error) {
	className := obc.Spec.StorageClassName
	if className == "" {
		return nil, fmt.Errorf("got empty string storage class name")
	}
	sc := &storagev1.StorageClass{}
	err := r.client.Get(context.TODO(), client.ObjectKey{Name: className}, sc)
	if err != nil {
		return nil, fmt.Errorf("could not get storage class: %v", err)
	}
	return sc, nil
}

func newCredentailsSecret(obc *v1alpha1.ObjectBucketClaim, keys *auth.S3AccessKeys) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			GenerateName: obc.Name,
			Namespace:    obc.Namespace,
			Finalizers:   []string{finalizer},
		},
		StringData: map[string]string{
			bucketAccessKey: keys.AccessKey,
			bucketSecretKey: keys.SecretKey,
		},
	}
}

func newBucketConfigMap(ob *v1alpha1.ObjectBucket, obc *v1alpha1.ObjectBucketClaim) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: v1.ObjectMeta{
			Name:      obc.Name,
			Namespace: obc.Namespace,
		},
		Data: map[string]string{
			bucketName: obc.Spec.BucketName,
			bucketHost: ob.Spec.BucketHost,
			bucketPort: strconv.Itoa(ob.Spec.BucketPort),
		},
	}
}
