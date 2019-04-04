package reconciler

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

// shouldProvision is a simplistic check on whether this obc is a concern for this provisioner.
// Down the road, this will perform a broader set of checks.
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

func claimForKey(key client.ObjectKey, ic *internalClient) (obc *v1alpha1.ObjectBucketClaim, err error) {
	logD.Info("getting claim for key")
	if err = ic.Client.Get(ic.Ctx, key, obc); err != nil {
		if errors.IsNotFound(err) {
			return nil, err
		}
		return nil, fmt.Errorf("error getting claim: %v", err)
	}
	return obc.DeepCopy(), nil
}

func configMapForClaimKey(key client.ObjectKey, ic *internalClient) (cm *corev1.ConfigMap, err error) {
	logD.Info("getting configMap for key", "key", key)
	err = ic.Client.Get(ic.Ctx, key, cm)
	return
}

func secretForClaimKey(key client.ObjectKey, ic *internalClient) (sec *corev1.Secret, err error) {
	logD.Info("getting secret for key", "key", key)
	err = ic.Client.Get(ic.Ctx, key, sec)
	return
}

func setObjectBucketName(ob *v1alpha1.ObjectBucket, key client.ObjectKey) {
	logD.Info("setting OB name", "name", ob.Name)
	ob.Name = fmt.Sprintf(ObjectBucketNameFormat, key.Namespace, key.Name)
}

func updateClaim(obc *v1alpha1.ObjectBucketClaim, c *internalClient) error {
	logD.Info("updating claim", "name", fmt.Sprintf("%s/%s", obc.Namespace, obc.Name))
	err := c.Client.Update(c.Ctx, obc)
	if err != nil {
		if errors.IsNotFound(err) {
			return err
		} else {
			return fmt.Errorf("error updating OBC: %v", err)
		}
	}
	logD.Info("claim update successful")
	return nil
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

func storageClassForClaim(obc *v1alpha1.ObjectBucketClaim, ic *internalClient) (*storagev1.StorageClass, error) {
	logD.Info("getting storageClass for claim")
	if obc == nil {
		return nil, fmt.Errorf("got nil ObjectBucketClaim ptr")
	}
	if obc.Spec.StorageClassName == "" {
		return nil, fmt.Errorf("no StorageClass defined for ObjectBucketClaim \"%s/%s\"", obc.Namespace, obc.Name)
	}
	logD.Info("OBC defined class", "name", obc.Spec.StorageClassName)

	class := &storagev1.StorageClass{}
	logD.Info("getting storage class", "name", obc.Spec.StorageClassName)
	err := ic.Client.Get(
		ic.Ctx,
		types.NamespacedName{
			Namespace: "",
			Name:      obc.Spec.StorageClassName,
		},
		class)
	if err != nil {
		return nil, fmt.Errorf("error getting storage class %q: %v", obc.Spec.StorageClassName, err)
	}
	log.Info("successfully got class", "name")
	return class, nil
}

func createUntilDefaultTimeout(obj runtime.Object, c client.Client, interval, timeout time.Duration) error {
	logD.Info("creating object until timeout", "interval", interval, "timeout", timeout)
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
				klog.Error("")
				return false, nil
			}
		}
		return true, nil
	})
}

func hasFinalizer(obj v1.Object) bool {
	logD.Info("checking for finalizer", "value", Finalizer, "object", obj.GetName())
	for _, f := range obj.GetFinalizers() {
		if f == Finalizer {
			logD.Info("found finalizer in obj")
			return true
		}
	}
	logD.Info("finalizer not found")
	return false
}

// RemoveFinalizer deletes the provisioner libraries's finalizer from the Object.  Finalizers added by
// other sources are left intact.
// obj MUST be a point so that changes made to obj finalizers are reflected in runObj
func removeFinalizer(obj v1.Object, ic *internalClient) error {
	logD.Info("removing finalizer from object", "name", obj.GetName())
	runObj, ok := obj.(runtime.Object)
	if !ok {
		return fmt.Errorf("could not case obj to runtime.Object interface")
	}

	finalizers := obj.GetFinalizers()
	for i, f := range finalizers {
		if f == Finalizer {
			logD.Info("found finalizer, deleting and updating API")
			obj.SetFinalizers(append(finalizers[:i], finalizers[i+1:]...))
			err := ic.Client.Update(ic.Ctx, runObj)
			if err != nil {
				return err
			}
			logD.Info("finalizer deletion successful")
			break
		}
	}
	return nil
}
