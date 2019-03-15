package util

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"testing"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
			Finalizer,
		},
	}

	type args struct {
		obc    *v1alpha1.ObjectBucketClaim
		client client.Client
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
				client: BuildFakeClient(t),
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
				client: BuildFakeClient(t),
			},
			want:    nil,
			wantErr: false,
		}, {
			name: "non nil storage class name",
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: testObjectMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						StorageClassName: storageClassName,
					},
				},
				client: BuildFakeClient(t),
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
				client: BuildFakeClient(t),
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			if tt.args.obc != nil {
				if err := tt.args.client.Create(context.TODO(), tt.args.obc); err != nil {
					t.Errorf("error pre-creating OBC: %v", err)
				}
			}
			if tt.want != nil {
				if err := tt.args.client.Create(context.TODO(), tt.want); err != nil {
					t.Errorf("error pre-creating StorageClass: %v", err)
				}
			}

			got, err := StorageClassForClaim(tt.args.obc, tt.args.client, context.TODO())
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
		obcName      = "obc-testname"
		obcNamespace = "obc-testnamespace"
		authKey      = "test-auth-key"
		authSecret   = "test-auth-secret"
	)

	testObjectMeta := metav1.ObjectMeta{
		Name:      obcName,
		Namespace: obcNamespace,
		Finalizers: []string{
			Finalizer,
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
						AccessKeyId:     authKey,
						SecretAccessKey: authSecret,
					},
				},
			},
			want: &corev1.Secret{
				ObjectMeta: testObjectMeta,
				StringData: map[string]string{
					v1alpha1.AwsKeyField:    authKey,
					v1alpha1.AwsSecretField: authSecret,
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
						AccessKeyId:     "",
						SecretAccessKey: "",
					},
				},
			},
			want: &corev1.Secret{
				ObjectMeta: testObjectMeta,
				StringData: map[string]string{
					v1alpha1.AwsKeyField:    "",
					v1alpha1.AwsSecretField: "",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewCredentialsSecret(tt.args.obc, tt.args.authentication)
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

	fakeClient := BuildFakeClient(t)

	objMeta := metav1.ObjectMeta{
		Namespace: "testNamespace",
		Name:      "testName",
	}

	type args struct {
		obj        runtime.Object
		fakeClient client.Client
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
			if err := CreateUntilDefaultTimeout(tt.args.obj, tt.args.fakeClient); (err != nil) != tt.wantErr {
				t.Errorf("CreateUntilDefaultTimeout() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTranslateReclaimPolicy(t *testing.T) {
	type args struct {
		policy corev1.PersistentVolumeReclaimPolicy
	}
	tests := []struct {
		name    string
		args    args
		want    v1alpha1.ReclaimPolicy
		wantErr bool
	}{
		{
			name: "nil policy",
			args: args{
				policy: "",
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "unknown policy",
			args: args{
				policy: "foobar",
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "retain policy",
			args: args{
				policy: corev1.PersistentVolumeReclaimRetain,
			},
			want:    v1alpha1.ReclaimPolicyRetain,
			wantErr: false,
		},
		{
			name: "delete policy",
			args: args{
				policy: corev1.PersistentVolumeReclaimDelete,
			},
			want:    v1alpha1.ReclaimPolicyDelete,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TranslateReclaimPolicy(tt.args.policy)
			if (err != nil) != tt.wantErr {
				t.Errorf("TranslateReclaimPolicy() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("TranslateReclaimPolicy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateBucketName(t *testing.T) {

	expectPattern := fmt.Sprintf("-[bcdfghjklmnpqrstvwxz0-9]{%d}", suffixLen)

	type args struct {
		prefix string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "empty name",
			args: args{
				prefix: "",
			},
			want: "",
		},
		{
			name: "non-empty name",
			args: args{
				prefix: "foobar",
			},
			want: "(foobar)" + expectPattern,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			got := GenerateBucketName(tt.args.prefix)

			if matches, err := regexp.MatchString(tt.want, got); err != nil {
				t.Errorf("error matching pattern %s with %s", tt.want, got)
			} else if !matches {
				t.Errorf("GenerateBucketName() = %v, want %v", got, tt.want)
			}

			// pattern, err := regexp.Compile(alphaNum)
			// if err != nil {
			// 	t.Errorf("error generating regex pattern %q: %v", tt.want, err)
			// }
			// if ! pattern.MatchString(got) {
			// 	t.Errorf("GenerateBucketName() = %v, want %v", got, tt.want)
			// }
		})
	}
}

func TestNewBucketConfigMap(t *testing.T) {

	const (
		host      = "www.test.com"
		name      = "bucket-name"
		port      = 11111
		ssl       = true
		region    = "region"
		subRegion = "sub-region"
	)

	objMeta := metav1.ObjectMeta{
		Name:      "test-obc",
		Namespace: "test-obc-namespace",
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
			name: "nil OBC",
			args: args{
				ep:  &v1alpha1.Endpoint{},
				obc: nil,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "nil Endpoint",
			args: args{
				ep:  nil,
				obc: &v1alpha1.ObjectBucketClaim{},
			},
			want:    nil,
			wantErr: true,
		},
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
					BucketName:      name,
					BucketHost:      host,
					BucketPort:      strconv.Itoa(port),
					BucketSSL:       strconv.FormatBool(ssl),
					BucketRegion:    region,
					BucketSubRegion: subRegion,
					BucketURL:       fmt.Sprintf("https://%s:%d/%s/%s/%s", host, port, region, subRegion, name),
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
					BucketName:      name,
					BucketHost:      host,
					BucketPort:      strconv.Itoa(port),
					BucketSSL:       strconv.FormatBool(ssl),
					BucketRegion:    region,
					BucketSubRegion: "",
					BucketURL:       fmt.Sprintf("https://%s:%d/%s/%s", host, port, region, name),
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
					BucketName:      name,
					BucketHost:      host,
					BucketPort:      strconv.Itoa(port),
					BucketSSL:       strconv.FormatBool(!ssl),
					BucketRegion:    region,
					BucketSubRegion: subRegion,
					BucketURL:       fmt.Sprintf("http://%s:%d/%s/%s/%s", host, port, region, subRegion, name),
				},
			},
			wantErr: false,
		},
		{
			name: "with no bucket defined",
			args: args{
				ep: &v1alpha1.Endpoint{
					BucketHost: host,
					BucketPort: port,
					BucketName: "",
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
			want:    nil,
			wantErr: true,
		},
		{
			name: "with no host defined",
			args: args{
				ep: &v1alpha1.Endpoint{
					BucketHost: "",
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
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			got, err := NewBucketConfigMap(tt.args.ep, tt.args.obc)
			if (err != nil) == !tt.wantErr {
				t.Errorf("NewBucketConfigMap() error = %v, wantErr %v", err, tt.wantErr)
			} else if !reflect.DeepEqual(got, tt.want) {
				gotjson, _ := json.MarshalIndent(got, "", "\t")
				wantjson, _ := json.MarshalIndent(tt.want, "", "\t")
				t.Errorf("NewBucketConfigMap() = %v, want %v", string(gotjson), string(wantjson))
			}
		})
	}
}

func TestNewObjectBucket(t *testing.T) {

	const objName, objNamespace = "test-name", "test-namespace"

	type args struct {
		obc        *v1alpha1.ObjectBucketClaim
		connection *v1alpha1.Connection
	}
	tests := []struct {
		name    string
		args    args
		want    *v1alpha1.ObjectBucket
		wantErr bool
	}{
		{
			name: "catch nil inputs",
			args: args{
				obc:        nil,
				connection: nil,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "expected output",
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-name",
						Namespace: "test-namespace",
					},
					Spec: v1alpha1.ObjectBucketClaimSpec{},
				},
				connection: &v1alpha1.Connection{
					Endpoint:       nil,
					Authentication: nil,
				},
			},
			want: &v1alpha1.ObjectBucket{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(obNamePattern, objNamespace, objName),
				},
				Spec: v1alpha1.ObjectBucketSpec{
					Connection: &v1alpha1.Connection{},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewObjectBucket(tt.args.obc, tt.args.connection)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewObjectBucket() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewObjectBucket() = %v, want %v", got, tt.want)
			}
		})
	}
}

func BuildFakeClient(t *testing.T, initObjs ...runtime.Object) (fakeClient client.Client) {

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Errorf("error adding core/v1 scheme: %v", err)
	}
	if err := storagev1.AddToScheme(scheme); err != nil {
		t.Errorf("error adding storage/v1 scheme: %v", err)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Errorf("error adding storage/v1 scheme: %v", err)
	}
	fakeClient = fake.NewFakeClientWithScheme(scheme, initObjs...)

	return fakeClient
}
