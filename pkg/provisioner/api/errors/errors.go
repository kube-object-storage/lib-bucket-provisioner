package errors

import (
	"fmt"
)

// BucketExistsErr SHOULD be returned by the Provision() method when bucket creation fails due a name collision in the
// object store
type BucketExistsErr struct {
	errString string
}

// Error implements the Error interface
func (e BucketExistsErr) Error() string {
	return fmt.Sprintf("%v", e.errString)
}

// NewBucketExistsError is a simple constructor for a BucketExistsErr
func NewBucketExistsError(msg string) *BucketExistsErr {
	return &BucketExistsErr{
		errString: msg,
	}
}

// IsBucketExists returns true if the error is of type BucketExistsErr
func IsBucketExists(e error) (is bool) {
	_, is = e.(BucketExistsErr)
	return is
}
