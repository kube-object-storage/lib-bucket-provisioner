package auth

type S3AccessKeys struct {
	AccessKey, SecretKey string
}

func (k *S3AccessKeys) AreEmpty() bool {
	return k.AccessKey == "" && k.SecretKey == ""
}
