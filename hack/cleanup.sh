# clean up k8s resources after failed obc run

ns="s3-provisioner"
name="myobc"

for res in cm secret obc; do
   if kubectl get $res -n $ns $name; then
      # remove finalizer and delete
      kubectl patch $res -n $ns $name --type merge -p '{"metadata":{"finalizers": []}}'
      kubectl delete $res -n $ns $name
   fi
done

obName="obc-$ns-$name"
if kubectl get ob $obName; then
   kubectl patch ob $obName --type merge -p '{"metadata":{"finalizers": []}}'
   kubectl delete ob $obName
fi

