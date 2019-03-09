package v1alpha1

import (
	"reflect"
	"testing"
)

const (
	authKey    = "test-key"
	authSecret = "test-secret"
)

func TestAccessKeys_toMap(t *testing.T) {
	tests := []struct {
		name string
		ak   *AccessKeys
		want map[string]string
	}{
		{
			name: "with defined key values",
			ak: &AccessKeys{
				AccessKeyId:     authKey,
				SecretAccessKey: authSecret,
			},
			want: map[string]string{
				AwsKeyField:    authKey,
				AwsSecretField: authSecret,
			},
		}, {
			name: "without defined key values",
			ak:   &AccessKeys{},
			want: map[string]string{
				AwsKeyField:    "",
				AwsSecretField: "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ak.toMap(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AccessKeys.toMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAuthSource_ToMap(t *testing.T) {
	tests := []struct {
		name string
		a    *AuthSource
		want map[string]string
	}{
		{
			name: "with no fields declared",
			a:    &AuthSource{},
			want: map[string]string{},
		}, {
			name: "with AccessKeys defined",
			a: &AuthSource{
				AccessKeys: &AccessKeys{
					AccessKeyId:     authKey,
					SecretAccessKey: authSecret,
				},
			},
			want: map[string]string{
				AwsKeyField:    authKey,
				AwsSecretField: authSecret,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.ToMap(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AuthSource.ToMap() = %v, want %v", got, tt.want)
			}
		})
	}
}
