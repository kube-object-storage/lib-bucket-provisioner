package util

import (
	"context"
	"fmt"
	"k8s.io/client-go/tools/reference"
	"path"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	internal "github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/reconciler/reconciler-internal"
)

const (
	DefaultRetryBaseInterval = time.Second * 10
	DefaultRetryTimeout      = time.Second * 360

	DebugInfoLvl = iota
	DebugLogLvl

	DomainPrefix = "objectbucket.io"

	BucketName      = "BUCKET_NAME"
	BucketHost      = "BUCKET_HOST"
	BucketPort      = "BUCKET_PORT"
	BucketRegion    = "BUCKET_REGION"
	BucketSubRegion = "BUCKET_SUBREGION"
	BucketURL       = "BUCKET_URL"
	BucketSSL       = "BUCKET_SSL"
	Finalizer       = DomainPrefix + "/finalizer"
)

// NewBucketConfigMap constructs a config map from a given endpoint and ObjectBucketClaim
// As a quality of life addition, it constructs a full URL for the bucket path.
// Success is constrained by a defined Bucket name and Bucket host.
func NewBucketConfigMap(ep *v1alpha1.Endpoint, obc *v1alpha1.ObjectBucketClaim) (*corev1.ConfigMap, error) {

	if ep == nil {
		return nil, fmt.Errorf("cannot construct ConfigMap, got nil Endpoint")
	}
	if obc == nil {
		return nil, fmt.Errorf("cannot construct ConfigMap, got nil OBC")
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:       obc.Name,
			Namespace:  obc.Namespace,
			Finalizers: []string{Finalizer},
		},
		Data: map[string]string{
			BucketName:      obc.Spec.BucketName,
			BucketHost:      ep.BucketHost,
			BucketPort:      strconv.Itoa(ep.BucketPort),
			BucketSSL:       strconv.FormatBool(ep.SSL),
			BucketRegion:    ep.Region,
			BucketSubRegion: ep.SubRegion,
			BucketURL:       fmt.Sprintf("%s:%d/%s", ep.BucketHost, ep.BucketPort, path.Join(ep.Region, ep.SubRegion, ep.BucketName)),
		},
	}, nil
}

// NewCredentailsSecret returns a secret with data appropriate to the supported authenticaion method.
// Even if the values for the Authentication keys are empty, we generate the secret.
func NewCredentialsSecret(obc *v1alpha1.ObjectBucketClaim, auth *v1alpha1.Authentication) (*corev1.Secret, error) {

	if obc == nil {
		return nil, fmt.Errorf("ObjectBucketClaim required to generate secret")
	}
	if auth == nil {
		return nil, fmt.Errorf("got nil authentication, nothing to do")
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:       obc.Name,
			Namespace:  obc.Namespace,
			Finalizers: []string{Finalizer},
		},
	}

	secret.StringData = auth.ToMap()
	return secret, nil
}

func CreateUntilDefaultTimeout(obj runtime.Object, c client.Client, interval, timeout time.Duration) error {

	if c == nil {
		return fmt.Errorf("error creating object, nil client")
	}
	return wait.PollImmediate(interval, timeout, func() (done bool, err error) {
		err = c.Create(context.Background(), obj)
		if err != nil {
			if errors.IsAlreadyExists(err) {
				// The object already exists don't spam the logs, instead let the request be requeued
				return true, err
			} else {
				// The error could be intermittent, log and try again
				klog.Error(err)
				return false, nil
			}
		}
		return true, nil
	})
}

const ObjectBucketNameFormat = "obc-%s-%s"

func SetObjectBucketName(ob *v1alpha1.ObjectBucket, key client.ObjectKey) {
	ob.Name = fmt.Sprintf(ObjectBucketNameFormat, key.Namespace, key.Name)
}

const (
	maxNameLen     = 63
	uuidSuffixLen  = 36
	maxBaseNameLen = maxNameLen - uuidSuffixLen
)

func ComposeBucketName(obc *v1alpha1.ObjectBucketClaim) (string, error) {
	// XOR BucketName and GenerateBucketName
	if (obc.Spec.BucketName == "") == (obc.Spec.GeneratBucketName == "") {
		return "", fmt.Errorf("expected either bucketName or generateBucketName defined")
	}
	bucketName := obc.Spec.BucketName
	if bucketName == "" {
		bucketName = generateBucketName(obc.Spec.GeneratBucketName)
	}
	return bucketName, nil
}

func generateBucketName(prefix string) string {
	if len(prefix) > maxBaseNameLen {
		prefix = prefix[:maxBaseNameLen-1]
	}
	return fmt.Sprintf("%s-%s", prefix, uuid.New())
}

func StorageClassForClaim(obc *v1alpha1.ObjectBucketClaim, c *internal.InternalClient) (*storagev1.StorageClass, error) {

	if obc == nil {
		return nil, fmt.Errorf("got nil ObjectBucketClaim ptr")
	}
	if obc.Spec.StorageClassName == "" {
		return nil, fmt.Errorf("no StorageClass defined for ObjectBucketClaim \"%s/%s\"", obc.Namespace, obc.Name)
	}

	class := &storagev1.StorageClass{}
	err := c.Client.Get(
		c.Ctx,
		types.NamespacedName{
			Namespace: "",
			Name:      obc.Spec.StorageClassName,
		},
		class)
	if err != nil {
		return nil, fmt.Errorf("error getting storage class %q: %v", obc.Spec.StorageClassName, err)
	}
	return class, nil
}

func HasFinalizer(obj metav1.Object) bool {
	for _, f := range obj.GetFinalizers() {
		if f == Finalizer {
			return true
		}
	}
	return false
}

func RemoveFinalizer(obj metav1.Object, c *internal.InternalClient) error {
	if runObj, ok := obj.(runtime.Object); ok {
		finalizers := obj.GetFinalizers()
		for i, f := range finalizers {
			if f == Finalizer {
				obj.SetFinalizers(append(finalizers[:i], finalizers[i+1:]...))
				break
			}
		}
		err := c.Client.Update(c.Ctx, runObj)
		if err != nil {
			return err
		}
	}
	return nil
}

func UpdateClaimWithBucket(obc *v1alpha1.ObjectBucketClaim, name string, c *internal.InternalClient) error {
	obc.Spec.ObjectBucketName = name
	err := c.Client.Update(c.Ctx, obc)
	if err != nil {
		return fmt.Errorf("error adding OB name to OBC: %v", err)
	}
	return nil
}

func ClaimRefForClaim(obc *v1alpha1.ObjectBucketClaim, c internal.InternalClient) (*corev1.ObjectReference, error) {
	ref, err := reference.GetReference(c.Scheme, obc)
	if err != nil {
		return nil, fmt.Errorf("error getting claimref: %v", err)
	}
	return ref, nil
}
