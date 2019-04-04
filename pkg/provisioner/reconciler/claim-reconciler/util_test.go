package reconciler

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"regexp"
	"strconv"
	"testing"

	"k8s.io/apimachinery/pkg/util/rand"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
)

func TestStorageClassForClaim(t *testing.T) {

	const (
		storageClassName = "testStorageClass"
	)

	testObjectMeta := metav1.ObjectMeta{
		Name:      "testname",
		Namespace: "testnamespace",
		Finalizers: []string{
			finalizer,
		},
	}

	type args struct {
		obc    *v1alpha1.ObjectBucketClaim
		client *internalClient
	}

	tests := []struct {
		name    string
		args    args
		want    *storagev1.StorageClass
		wantErr bool
	}{
		{
			name: "nil OBC ptr",
			args: args{
				obc:    nil,
				client: buildFakeInternalClient(t),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "nil storage class name",
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: testObjectMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						StorageClassName: "",
					},
				},
				client: buildFakeInternalClient(t),
			},
			want:    nil,
			wantErr: true,
		}, {
			name: "non nil storage class name",
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: testObjectMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						StorageClassName: storageClassName,
					},
				},
				client: buildFakeInternalClient(t),
			},
			want: &storagev1.StorageClass{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name: storageClassName,
				},
			},
			wantErr: false,
		},
		{
			name: "storage class does not exist",
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: testObjectMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						StorageClassName: storageClassName,
					},
				},
				client: buildFakeInternalClient(t),
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			if tt.args.obc != nil {
				if err := tt.args.client.Client.Create(context.TODO(), tt.args.obc); err != nil {
					t.Errorf("error pre-creating OBC: %v", err)
				}
			}
			if tt.want != nil {
				if err := tt.args.client.Client.Create(context.TODO(), tt.want); err != nil {
					t.Errorf("error pre-creating StorageClass: %v", err)
				}
			}

			got, err := storageClassForClaim(tt.args.obc, tt.args.client)
			if (err != nil) != tt.wantErr {
				t.Errorf("StorageClassForClaim() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("StorageClassForClaim() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewCredentialsSecret(t *testing.T) {
	const (
		obcName        = "obc-testname"
		obcNamespace   = "obc-testnamespace"
		authKey        = "test-auth-key"
		authSecret     = "test-auth-secret"
		awsKeyField    = "ACCESS_KEY_ID"
		awsSecretField = "SECRET_KEY_ID"
	)

	testObjectMeta := metav1.ObjectMeta{
		Name:      obcName,
		Namespace: obcNamespace,
		Finalizers: []string{
			finalizer,
		},
	}

	type args struct {
		obc            *v1alpha1.ObjectBucketClaim
		authentication *v1alpha1.Authentication
	}

	tests := []struct {
		name    string
		args    args
		want    *corev1.Secret
		wantErr bool
	}{
		{
			name: "with nil ObjectBucketClaim ptr",
			args: args{
				authentication: &v1alpha1.Authentication{},
				obc:            nil,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "with nil Authentication ptr",
			args: args{
				obc:            &v1alpha1.ObjectBucketClaim{},
				authentication: nil,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "with an authentication type defined (access keys)",
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: testObjectMeta,
				},
				authentication: &v1alpha1.Authentication{
					AccessKeys: &v1alpha1.AccessKeys{
						AccessKeyID:     authKey,
						SecretAccessKey: authSecret,
					},
				},
			},
			want: &corev1.Secret{
				ObjectMeta: testObjectMeta,
				StringData: map[string]string{
					awsKeyField:    authKey,
					awsSecretField: authSecret,
				},
			},
			wantErr: false,
		},
		{
			name: "with empty access keys",
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: testObjectMeta,
				},
				authentication: &v1alpha1.Authentication{
					AccessKeys: &v1alpha1.AccessKeys{
						AccessKeyID:     "",
						SecretAccessKey: "",
					},
				},
			},
			want: &corev1.Secret{
				ObjectMeta: testObjectMeta,
				StringData: map[string]string{
					awsKeyField:    "",
					awsSecretField: "",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newCredentialsSecret(tt.args.obc, tt.args.authentication)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewCredentailsSecret() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewCredentailsSecret() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateUntilDefaultTimeout(t *testing.T) {

	fakeClient := buildFakeInternalClient(t)

	objMeta := metav1.ObjectMeta{
		Namespace: "testNamespace",
		Name:      "testName",
	}

	type args struct {
		obj        runtime.Object
		fakeClient *internalClient
	}

	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "nil runtime object",
			args: args{
				obj:        nil,
				fakeClient: fakeClient,
			},
			wantErr: true,
		},
		{
			name: "nil client",
			args: args{
				obj:        &corev1.Pod{}, // arbitrary runtime.Object
				fakeClient: nil,
			},
			wantErr: true,
		},
		{
			name: "create a k8s.io/core object",
			args: args{
				obj: &corev1.Secret{
					ObjectMeta: objMeta,
				},
				fakeClient: fakeClient,
			},
			wantErr: false,
		},
		{
			name: "create a v1alpha1 custom object",
			args: args{
				obj: &v1alpha1.ObjectBucket{
					ObjectMeta: objMeta,
				},
				fakeClient: fakeClient,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := createUntilDefaultTimeout(tt.args.obj, tt.args.fakeClient.Client, 1, 1); (err != nil) != tt.wantErr {
				t.Errorf("CreateUntilDefaultTimeout() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGenerateBucketName(t *testing.T) {
	type args struct {
		prefix string
	}
	tests := []struct {
		name    string
		args    args
		wanterr bool
	}{
		{
			name: "empty name",
			args: args{
				prefix: "",
			},
			wanterr: false,
		},
		{
			name: "below max name",
			args: args{
				prefix: "foobar",
			},
			wanterr: false,
		},
		{
			name: "over max name length name",
			args: args{
				prefix: rand.String(maxNameLen * 2),
			},
			wanterr: false,
		},
	}

	const pattern = `.*-[a-z0-9]{8}(-[a-z0-9]{4}){3}-[a-z0-9]{12}`

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateBucketName(tt.args.prefix)
			if len(got) > maxNameLen {
				t.Errorf("GenerateName() wanted len <= %d, got len %d", maxNameLen, len(got))
			}
			if match, err := regexp.MatchString(pattern, got); err != nil {
				t.Errorf("error matching pattern: %v", err)
			} else if !match {
				t.Errorf("GenerateName() want match: %v, got %v", pattern, got)
			}
		})
	}
}

func TestNewBucketConfigMap(t *testing.T) {

	const (
		host      = "http://www.test.com"
		name      = "bucket-name"
		port      = 11111
		ssl       = true
		region    = "region"
		subRegion = "sub-region"
	)

	objMeta := metav1.ObjectMeta{
		Name:       "test-obc",
		Namespace:  "test-obc-namespace",
		Finalizers: []string{finalizer},
	}

	type args struct {
		ep  *v1alpha1.Endpoint
		obc *v1alpha1.ObjectBucketClaim
	}
	tests := []struct {
		name    string
		args    args
		want    *corev1.ConfigMap
		wantErr bool
	}{
		{
			name: "endpoint with region and subregion",
			args: args{
				ep: &v1alpha1.Endpoint{
					BucketHost: host,
					BucketPort: port,
					BucketName: name,
					Region:     region,
					SubRegion:  subRegion,
					SSL:        ssl,
				},
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: objMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						BucketName: name,
						SSL:        ssl,
					},
				},
			},
			want: &corev1.ConfigMap{
				ObjectMeta: objMeta,
				Data: map[string]string{
					bucketName:      name,
					bucketHost:      host,
					bucketPort:      strconv.Itoa(port),
					bucketSSL:       strconv.FormatBool(ssl),
					bucketRegion:    region,
					bucketSubRegion: subRegion,
					bucketURL:       fmt.Sprintf("%s:%d/%s", host, port, path.Join(region, subRegion, name)),
				},
			},
			wantErr: false,
		},
		{
			name: "endpoint with only region",
			args: args{
				ep: &v1alpha1.Endpoint{
					BucketHost: host,
					BucketPort: port,
					BucketName: name,
					Region:     region,
					SubRegion:  "",
					SSL:        ssl,
				},
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: objMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						BucketName: name,
						SSL:        ssl,
					},
				},
			},
			want: &corev1.ConfigMap{
				ObjectMeta: objMeta,
				Data: map[string]string{
					bucketName:      name,
					bucketHost:      host,
					bucketPort:      strconv.Itoa(port),
					bucketSSL:       strconv.FormatBool(ssl),
					bucketRegion:    region,
					bucketSubRegion: "",
					bucketURL:       fmt.Sprintf("%s:%d/%s", host, port, path.Join(region, name)),
				},
			},
			wantErr: false,
		},
		{
			name: "with endpoint defined (non-SSL)",
			args: args{
				ep: &v1alpha1.Endpoint{
					BucketHost: host,
					BucketPort: port,
					BucketName: name,
					Region:     region,
					SubRegion:  subRegion,
					SSL:        !ssl,
				},
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: objMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						BucketName: name,
						SSL:        !ssl,
					},
				},
			},
			want: &corev1.ConfigMap{
				ObjectMeta: objMeta,
				Data: map[string]string{
					bucketName:      name,
					bucketHost:      host,
					bucketPort:      strconv.Itoa(port),
					bucketSSL:       strconv.FormatBool(!ssl),
					bucketRegion:    region,
					bucketSubRegion: subRegion,
					bucketURL:       fmt.Sprintf("%s:%d/%s", host, port, path.Join(region, subRegion, name)),
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			got, err := newBucketConfigMap(tt.args.ep, tt.args.obc)
			if (err != nil) == !tt.wantErr {
				t.Errorf("newBucketConfigMap() error = %v, wantErr %v", err, tt.wantErr)
			} else if !reflect.DeepEqual(got, tt.want) {
				gotjson, _ := json.MarshalIndent(got, "", "\t")
				wantjson, _ := json.MarshalIndent(tt.want, "", "\t")
				t.Errorf("newBucketConfigMap() = %v, want %v", string(gotjson), string(wantjson))
			}
		})
	}
}

// should be implemented as an E2E test
// func TestNewObjectBucket(t *testing.T) {
//
// 	const (
// 		testns       = "test-namespace"
// 		testname     = "test-name"
// 		objName      = testname
// 		objNamespace = testns
// 		className    = "dummyClass"
// 		bucketName   = "testbucket"
// 	)
//
// 	selfLink := fmt.Sprintf("/apis/objectbucket.io/v1alpha1/namespaces/%s/objectbucketclaims/%s", testns, testname)
//
// 	deletePolicy := corev1.PersistentVolumeReclaimDelete
//
// 	class := &storagev1.StorageClass{
// 		ObjectMeta: metav1.ObjectMeta{
// 			Name: className,
// 		},
// 		Provisioner:   "dummyProvisioner",
// 		ReclaimPolicy: &deletePolicy,
// 	}
//
// 	type args struct {
// 		obc        *v1alpha1.ObjectBucketClaim
// 		connection *v1alpha1.Connection
// 		scheme     *runtime.Scheme
// 		ctx        context.Context
// 		client     client.Client
// 	}
// 	tests := []struct {
// 		name    string
// 		args    args
// 		want    *v1alpha1.ObjectBucket
// 		wantErr bool
// 	}{
// 		{
// 			name: "expected output",
// 			args: args{
// 				obc: &v1alpha1.ObjectBucketClaim{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      testname,
// 						Namespace: testns,
// 						SelfLink:  selfLink,
// 					},
// 					Spec: v1alpha1.ObjectBucketClaimSpec{
// 						StorageClassName: className,
// 						BucketName:       bucketName,
// 					},
// 				},
// 				connection: &v1alpha1.Connection{
// 					Endpoint:       nil,
// 					Authentication: nil,
// 				},
// 				scheme: runtime.NewScheme(),
// 				ctx:    context.Background(),
// 			},
// 			want: &v1alpha1.ObjectBucket{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: fmt.Sprintf(ObjectBucketFormat, objNamespace, objName),
// 				},
// 				Spec: v1alpha1.ObjectBucketSpec{
// 					StorageClassName: className,
// 					ReclaimPolicy:    &deletePolicy,
// 					ClaimRef: &corev1.ObjectReference{
// 						Kind:            "ObjectBucketClaim",
// 						Namespace:       testns,
// 						Name:            testname,
// 						UID:             "",
// 						APIVersion:      "objectbucket.io/v1alpha1",
// 					},
// 					Connection: nil,
// 				},
// 			},
// 			wantErr: false,
// 		},
// 	}
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
//
// 			tt.args.client = buildFakeInternalClient(t, tt.args.obc)
//
// 			err := tt.args.client.Create(tt.args.ctx, class)
// 			if err != nil {
// 				t.Error(err)
// 			}
//
// 			err = v1alpha1.AddToScheme(tt.args.scheme)
// 			if err != nil {
// 				t.Error(err)
// 			}
//
// 			got, err := NewObjectBucket(tt.args.obc, tt.args.connection, tt.args.client, tt.args.ctx, tt.args.scheme)
// 			if (err != nil) != tt.wantErr {
// 				t.Errorf("NewObjectBucket() error = %v, wantErr %v", err, tt.wantErr)
// 				return
// 			}
// 			if !reflect.DeepEqual(got, tt.want) {
// 				t.Errorf("NewObjectBucket() = %v, want %v", got, tt.want)
// 			}
// 		})
// 	}
// }
