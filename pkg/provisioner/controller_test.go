/*
Copyright 2019 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package provisioner

import (
	"context"
	"testing"

	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	externalFake "github.com/kube-object-storage/lib-bucket-provisioner/pkg/client/clientset/versioned/fake"
)

func TestHandleProvisionClaim(t *testing.T) {
	tests := []struct {
		name       string
		parameters map[string]string
		wantSecret bool
		wantErr    bool
	}{
		{
			name:       "creates secret by default",
			parameters: nil,
			wantSecret: true,
		},
		{
			name:       "skips secret when storage class opts out",
			parameters: map[string]string{v1alpha1.StorageClassSkipCredentialsSecret: "true"},
			wantSecret: false,
		},
		{
			name:       "invalid opt-out value",
			parameters: map[string]string{v1alpha1.StorageClassSkipCredentialsSecret: "maybe"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			extClient := externalFake.NewSimpleClientset()
			c := &obcController{
				clientset:    client,
				libClientset: extClient,
				provisioner:  &fakeProvisioner{},
			}
			class := &storagev1.StorageClass{
				ObjectMeta:  metav1.ObjectMeta{Name: className},
				Provisioner: provisionerName,
				Parameters:  tt.parameters,
			}
			obc, err := extClient.ObjectbucketV1alpha1().ObjectBucketClaims(testNamespace).Create(context.TODO(), &v1alpha1.ObjectBucketClaim{
				ObjectMeta: objMeta,
				Spec: v1alpha1.ObjectBucketClaimSpec{
					StorageClassName: className,
					BucketName:       "test-bucket",
				},
			}, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("error pre-creating OBC: %v", err)
			}

			err = c.handleProvisionClaim(testNamespace+"/"+testName, obc, class)
			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr %v, error = %v", tt.wantErr, err)
			}
			if tt.wantErr {
				return
			}

			_, err = client.CoreV1().Secrets(testNamespace).Get(context.TODO(), testName, metav1.GetOptions{})
			if tt.wantSecret && err != nil {
				t.Errorf("want secret, got error = %v", err)
			}
			if !tt.wantSecret && !errors.IsNotFound(err) {
				t.Errorf("want secret not found, got error = %v", err)
			}
			if _, err = client.CoreV1().ConfigMaps(testNamespace).Get(context.TODO(), testName, metav1.GetOptions{}); err != nil {
				t.Errorf("want configmap, got error = %v", err)
			}
		})
	}
}
