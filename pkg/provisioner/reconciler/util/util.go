package util

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/reference"

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

func NewObjectBucket(obc *v1alpha1.ObjectBucketClaim, connection *v1alpha1.Connection, client client.Client, ctx context.Context, scheme *runtime.Scheme) (*v1alpha1.ObjectBucket, error) {

	claimRef, err := reference.GetReference(scheme, obc)
	if err != nil {
		return nil, fmt.Errorf("could not get object ref: %v", err)
	}

	class := &storagev1.StorageClass{}
	err = client.Get(ctx, types.NamespacedName{Name: obc.Spec.StorageClassName}, class)
	if err != nil {
		return nil, fmt.Errorf("could not get storage class: %v", err)
	}

	return &v1alpha1.ObjectBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(ObjectBucketFormat, obc.Namespace, obc.Name),
		},
		Spec: v1alpha1.ObjectBucketSpec{
			StorageClassName: obc.Spec.StorageClassName,
			ReclaimPolicy:    class.ReclaimPolicy,
			ClaimRef:         claimRef,
			Connection:       connection,
		},
		// TODO probably need to prepopulate some prebinding, etc
		Status: v1alpha1.ObjectBucketStatus{
			Phase: v1alpha1.ObjectBucketClaimStatusPhaseBound,
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

func ValidEndpoint(ep *v1alpha1.Endpoint) error {
	if ep == nil {
		return fmt.Errorf("v1alpha1.Endpoint and v1alpha1.ObjectbucketClaim cannot be nil")
	}
	if ep.BucketHost == "" {
		return fmt.Errorf("bucketHost cannot be empty")
	}
	if ep.BucketName == "" {
		return fmt.Errorf("bucketName cannot be empty")
	}
	if !(strings.HasPrefix("https://", ep.BucketHost) ||
		!strings.HasPrefix("http://", ep.BucketHost) ||
		!strings.HasPrefix("s3://", ep.BucketHost)) {
		return fmt.Errorf("bucketHost must contain URL scheme")
	}
	return nil
}

const ObjectBucketFormat = "obc-%s-%s"

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

func StorageClassForClaim(obc *v1alpha1.ObjectBucketClaim, client client.Client, ctx context.Context) (*storagev1.StorageClass, error) {

	if obc == nil {
		return nil, fmt.Errorf("got nil ObjectBucketClaim ptr")
	}
	if obc.Spec.StorageClassName == "" {
		return nil, fmt.Errorf("no StorageClass defined for ObjectBucketClaim \"%s/%s\"", obc.Namespace, obc.Name)
	}

	class := &storagev1.StorageClass{}
	err := client.Get(
		ctx,
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

func HasFinalizer(meta metav1.ObjectMeta) bool {
	for _, f := range meta.GetFinalizers() {
		if f == Finalizer {
			return true
		}
	}
	return false
}

func RemoveFinalizer(meta *metav1.ObjectMeta) {
	for i, f := range meta.GetFinalizers() {
		if f == Finalizer {
			meta.Finalizers = append(meta.Finalizers[:i], meta.Finalizers[i+1:]...)
			break
		}
	}
}
