package provisioner

import (
	"flag"
	"k8s.io/klog"

	"k8s.io/client-go/rest"
	"k8s.io/klog/klogr"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
)

// Controller is the first iteration of our internal provisioning
// controller.  The passed-in bucket provisioner, coded by the user of the
// library, is stored for later Provision and Delete calls.
type Manager struct {
	Name        string
	Provisioner api.Provisioner
}

func initLoggers() {
	log = klogr.New().WithName(api.Domain + "/provisioner-manager")
	logD = log.V(1)
}

func initFlags() {
	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	flag.CommandLine.VisitAll(func(f *flag.Flag) {
		kflag := klogFlags.Lookup(f.Name)
		if kflag != nil {
			val := f.Value.String()
			kflag.Value.Set(val)
		}
	})
	if !flag.Parsed() {
		flag.Parse()
	}
}

// NewProvisioner should be called by importers of this library to
// instantiate a new provisioning controller. This controller will
// respond to Add / Update / Delete events by calling the passed-in
// provisioner's Provisioner and Delete methods.
// The Provisioner will be restrict to operating only to the namespace given
func NewProvisioner(
	cfg *rest.Config,
	provisionerName string,
	provisioner api.Provisioner,
	namespace string,
) (*Controller, error) {

	initFlags()
	initLoggers()



}

// Run starts the claim and bucket controllers.
func (p *Controller) Run() (err error) {
	defer klog.Flush()
	log.Info("Starting manager", "provisioner", p.Name)

	return
}

func handleSignals(){

}
