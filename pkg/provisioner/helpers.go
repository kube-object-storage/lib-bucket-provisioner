package provisioner

import (
	"fmt"

	"github.com/google/uuid"
	"k8s.io/client-go/tools/cache"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/client/clientset/versioned"
)

func shouldProvision(obc *v1alpha1.ObjectBucketClaim) bool {
	logD.Info("validating claim for provisioning")
	if obc.Spec.ObjectBucketName != "" {
		log.Info("provisioning already completed", "ObjectBucket", obc.Spec.ObjectBucketName)
		return false
	}
	if obc.Spec.StorageClassName == "" {
		log.Info("OBC did not provide a storage class, cannot provision")
		return false
	}
	return true
}

func claimRefForKey(key string, c versioned.Interface) (types.UID, error) {
	claim, err := claimForKey(key, c)
	if err != nil {
		return "", err
	}
	return claim.UID, nil
}

func claimForKey(key string, c versioned.Interface) (obc *v1alpha1.ObjectBucketClaim, err error) {
	logD.Info("getting claim for key")

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return nil, err
	}

	obc, err = c.ObjectbucketV1alpha1().ObjectBucketClaims(ns).Get(name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, err
		}
		return nil, fmt.Errorf("error getting claim: %v", err)
	}
	return obc.DeepCopy(), nil
}

// Return true if this storage class is for a new bucket vs an existing bucket.
func isNewBucketByClass(sc *storagev1.StorageClass) bool {
	return len(sc.Parameters[v1alpha1.StorageClassBucket]) == 0
}

// Return true if this OB is for a new bucket vs an existing bucket.
func isNewBucketByOB(c kubernetes.Interface, ob *v1alpha1.ObjectBucket) bool {
	// temp: get bucket name from OB's storage class
	class, err := storageClassForOB(ob, c)
	if err != nil || class == nil {
		log.Info("ERROR: unable to get storageclass", "ob", ob)
		return false
	}
	return len(class.Parameters[v1alpha1.StorageClassBucket]) == 0
}

func configMapForClaimKey(key string, c kubernetes.Interface) (*corev1.ConfigMap, error) {
	logD.Info("getting configMap for key", "key", key)
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return nil, err
	}
	cm, err := c.CoreV1().ConfigMaps(ns).Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return cm, nil
}

func secretForClaimKey(key string, c kubernetes.Interface) (sec *corev1.Secret, err error) {
	logD.Info("getting secret for key", "key", key)
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return nil, err
	}
	sec, err = c.CoreV1().Secrets(ns).Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return
}

func setObjectBucketName(ob *v1alpha1.ObjectBucket, key string) {
	obName, err := obNameFromClaimKey(key)
	if err != nil {
		return
	}
	logD.Info("setting OB name", "name", ob.Name)
	ob.Name = obName
}

func obNameFromClaimKey(key string) (string, error) {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(objectBucketNameFormat, ns, name), nil
}

func composeBucketName(obc *v1alpha1.ObjectBucketClaim) (string, error) {
	logD.Info("determining bucket name")
	// XOR BucketName and GenerateBucketName
	if (obc.Spec.BucketName == "") == (obc.Spec.GeneratBucketName == "") {
		return "", fmt.Errorf("expected either bucketName or generateBucketName defined")
	}
	bucketName := obc.Spec.BucketName
	if bucketName == "" {
		logD.Info("bucket name is empty, generating")
		bucketName = generateBucketName(obc.Spec.GeneratBucketName)
	}
	logD.Info("bucket name generated", "name", bucketName)
	return bucketName, nil
}

const (
	maxNameLen     = 63
	uuidSuffixLen  = 36
	maxBaseNameLen = maxNameLen - uuidSuffixLen
)

func generateBucketName(prefix string) string {
	if len(prefix) > maxBaseNameLen {
		prefix = prefix[:maxBaseNameLen-1]
		logD.Info("truncating prefix", "new prefix", prefix)
	}
	return fmt.Sprintf("%s-%s", prefix, uuid.New())
}

func storageClassForClaim(c kubernetes.Interface, obc *v1alpha1.ObjectBucketClaim) (*storagev1.StorageClass, error) {
	logD.Info("getting storageClass for claim")
	if obc == nil {
		return nil, fmt.Errorf("got nil ObjectBucketClaim ptr")
	}
	if obc.Spec.StorageClassName == "" {
		return nil, fmt.Errorf("no StorageClass defined for ObjectBucketClaim \"%s/%s\"", obc.Namespace, obc.Name)
	}
	logD.Info("getting storage class", "name", obc.Spec.StorageClassName)
	class, err := c.StorageV1().StorageClasses().Get(obc.Spec.StorageClassName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error getting storage class %q: %v", obc.Spec.StorageClassName, err)
	}
	log.Info("successfully got class", "name")
	return class, nil
}

func storageClassForOB(ob *v1alpha1.ObjectBucket, c kubernetes.Interface) (*storagev1.StorageClass, error) {
	logD.Info("getting storageClass for objectbucket")
	if ob == nil {
		return nil, fmt.Errorf("got nil ObjectBucket ptr")
	}

	logD.Info("getting storage class", "name", ob.Spec.StorageClassName)
	class, err := c.StorageV1().StorageClasses().Get(ob.Spec.StorageClassName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error getting storageclass %q: %v", ob.Spec.StorageClassName, err)
	}
	log.Info("successfully got class", "name")

	return class, nil
}

func removeFinalizer(obj metav1.Object) {
	logD.Info("removing finalizer from object", "name", obj.GetName())
	finalizers := obj.GetFinalizers()
	for i, f := range finalizers {
		if f == finalizer {
			logD.Info("found finalizer, deleting and updating API")
			obj.SetFinalizers(append(finalizers[:i], finalizers[i+1:]...))
			logD.Info("finalizer deletion successful")
			break
		}
	}
}
