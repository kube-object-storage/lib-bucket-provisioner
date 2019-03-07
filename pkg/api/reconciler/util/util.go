package util

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/api/provisioner"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
)

const (
	DefaultRetryBaseInterval = time.Second * 10
	DefaultRetryTimeout      = time.Second * 360
	DefaultRetryBackOff      = 1
	DefaultMaxAttempts       = 5
	Finalizer                = "objectbucket.io/finalizer"
	BucketName               = "S3_BUCKET_NAME"
	BucketHost               = "S3_BUCKET_HOST"
	BucketPort               = "S3_BUCKET_PORT"
	BucketAccessKey          = "S3_BUCKET_ACCESS_KEY_ID"
	BucketSecretKey          = "S3_BUCKET_SECRET_KEY"
	BucketURL                = "S3_BUCKET_URL"

	InfoLogLvl = iota // only here for completeness, it's no different than calling klog.Info()
	DebugLogLvl
)

func GetStorageClassByName(name string, c client.Client) (*storagev1.StorageClass, error) {
	sc := &storagev1.StorageClass{}
	err := c.Get(context.TODO(), client.ObjectKey{Name: name}, sc)
	if err != nil {
		return nil, fmt.Errorf("could not get storage class: %v", err)
	}
	return sc, nil
}

func NewCredentailsSecret(obc *v1alpha1.ObjectBucketClaim, keys *provisioner.S3AccessKeys) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: obc.Name,
			Namespace:    obc.Namespace,
			Finalizers:   []string{Finalizer},
		},
		StringData: map[string]string{
			BucketAccessKey: keys.AccessKey,
			BucketSecretKey: keys.SecretKey,
		},
	}
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
