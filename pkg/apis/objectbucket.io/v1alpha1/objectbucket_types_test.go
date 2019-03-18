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

func TestAuthentication_ToMap(t *testing.T) {
	type fields struct {
		AccessKeys *AccessKeys
	}
	tests := []struct {
		name   string
		fields fields
		want   map[string]string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Authentication{
				AccessKeys: tt.fields.AccessKeys,
			}
			if got := a.ToMap(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Authentication.ToMap() = %v, want %v", got, tt.want)
			}
		})
	}
}
