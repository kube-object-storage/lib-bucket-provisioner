package util

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
)

const (
	DefaultRetryBaseInterval = time.Second * 10
	DefaultRetryTimeout      = time.Second * 360
	DefaultRetryBackOff      = 1
	DefaultMaxAttempts       = 5

	BucketName      = "BUCKET_NAME"
	BucketHost      = "BUCKET_HOST"
	BucketPort      = "BUCKET_PORT"
	BucketAccessKey = "ACCESS_KEY_ID"
	BucketSecretKey = "SECRET_ACCESS_KEY"
	BucketURL       = "BUCKET_URL"

	InfoLogLvl = iota // only here for completeness, it's no different than calling klog.Info()
	DebugLogLvl

	DomainPrefix = "objectbucket.io"
	Finalizer    = DomainPrefix + "/finalizer"
	// OBC Annotations
)

func StorageClassForClaim(obc *v1alpha1.ObjectBucketClaim, client client.Client, ctx context.Context) (*storagev1.StorageClass, error) {
	if obc.Spec.StorageClassName == "" {
		return nil, nil
	}

	class := &storagev1.StorageClass{}
	err := client.Get(
		context.Background(),
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

func NewBucketConfigMap(ob *v1alpha1.ObjectBucket, obc *v1alpha1.ObjectBucketClaim) *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      obc.Name,
			Namespace: obc.Namespace,
		},
		Data: map[string]string{
			BucketName: obc.Spec.BucketName,
			BucketHost: ob.Spec.BucketHost,
			BucketPort: strconv.Itoa(ob.Spec.BucketPort),
		},
	}
}

func CreateUntilDefaultTimeout(obj runtime.Object, c client.Client) error {
	return wait.PollImmediate(DefaultRetryBaseInterval, DefaultRetryTimeout, func() (done bool, err error) {
		err = c.Create(context.Background(), obj)
		if err != nil && !errors.IsAlreadyExists(err) {
			return false, err
		}
		return true, nil
	})
}

func TranslateReclaimPolicy(rp v1.PersistentVolumeReclaimPolicy) (v1alpha1.ReclaimPolicy, error) {
	switch v1alpha1.ReclaimPolicy(rp) {
	case v1alpha1.ReclaimPolicyDelete:
		return v1alpha1.ReclaimPolicyDelete, nil
	case v1alpha1.ReclaimPolicyRetain:
		return v1alpha1.ReclaimPolicyRetain, nil
	}
	return "", fmt.Errorf("unrecognized reclaim policy %q", rp)
}

const suffixLen = 5

func GenerateBucketName(prefix string) string {
	suf := rand.String(suffixLen)
	if prefix == "" {
		return suf
	}
	return fmt.Sprintf("%s-%s", prefix, suf)
}
