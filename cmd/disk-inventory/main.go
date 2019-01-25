package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	goruntime "runtime"
	"strconv"
	"time"

	"github.com/PolarGeospatialCenter/disk-inventory/cmd/disk-inventory/devices"
	"github.com/PolarGeospatialCenter/disk-inventory/cmd/disk-inventory/disks"
	"github.com/PolarGeospatialCenter/local-storage-operator/pkg/apis"
	"github.com/PolarGeospatialCenter/local-storage-operator/pkg/apis/localstorage/v1alpha1"
	errapi "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"
)

var log = logf.Log.WithName("cmd")

func printVersion() {
	log.Info(fmt.Sprintf("Go Version: %s", goruntime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", goruntime.GOOS, goruntime.GOARCH))
}

type Node struct {
	Name string
}

type Client interface {
	Create(ctx context.Context, obj runtime.Object) error
	Update(ctx context.Context, obj runtime.Object) error
	Get(ctx context.Context, key client.ObjectKey, obj runtime.Object) error
}

func (n *Node) GetName() string {
	return n.Name
}

func main() {

	updateInterval := flag.String("interval", "1m", "Interval to scan for devices")
	enableDisks := flag.Bool("enableDisks", false, "Sets whether disks will be automaticly enabled.")
	flag.Parse()

	// The logger instantiated here can be changed to any logger
	// implementing the logr.Logger interface.
	logf.SetLogger(logf.ZapLogger(false))

	printVersion()

	hostname := os.Getenv("NODE_NAME")
	node := &Node{Name: hostname}

	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	options := client.Options{
		Scheme: scheme.Scheme,
	}

	clientset, err := client.New(config, options)
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	err = apis.AddToScheme(options.Scheme)
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	interval, _ := time.ParseDuration(*updateInterval)

	m := disks.Monitor{
		Node:     node,
		Interval: interval,
	}
	diskch := m.Start(ctx)
	stopch := signals.SetupSignalHandler()

	for {
		log.Info("Entering detection loop")
		select {
		case d := <-diskch:

			if d.Disk.GetWwn() == "" {
				log.Error(errors.New(""), fmt.Sprintf("recevied a disk without a WWN... Skipping: %v", d.Disk.GetDevName()))
				continue
			}

			err := syncDisk(*node, d.Disk, clientset, *enableDisks)
			if err != nil {
				log.Error(err, "error syncing disk")
			}
		case <-stopch:
			os.Exit(0)
		}
	}
}

func buildDiskObject(disk devices.Disk, node Node, enableDisk bool) *v1alpha1.Disk {
	k8sDisk := &v1alpha1.Disk{}
	k8sDisk.Init(disk.Name())
	k8sDisk.Spec.Info.UpdateInfo(
		disk.GetWwn(),
		disk.GetModel(),
		disk.GetSerialNumber(),
		strconv.FormatUint(disk.GetCapacityBytes(), 10))
	k8sDisk.Spec.Location.UpdateLocation(node.GetName(), disk.GetBackplane(), disk.GetSlot(), disk.GetDriver())
	k8sDisk.Spec.Info.UdevAttributes = disk.Attributes
	k8sDisk.Spec.Info.UdevProperties = disk.Properties

	k8sDisk.ObjectMeta.GetResourceVersion()

	k8sDisk.UpdateLabels()

	if enableDisk {
		k8sDisk.Spec.Enabled = true
	}

	return k8sDisk
}

//syncDisk syncs a single disk with the Kubernetes api.
func syncDisk(node Node, disk devices.Disk, client Client, enableDisks bool) error {
	k8sDisk := buildDiskObject(disk, node, enableDisks)

	diskObj := k8sDisk.DeepCopy()
	diskObj.Status.PreparePhase = v1alpha1.StoragePreparePhaseDiscovered

	err := client.Create(context.TODO(), diskObj)
	if errapi.IsAlreadyExists(err) {
		existingDisk := &v1alpha1.Disk{}

		err = client.Get(context.TODO(), types.NamespacedName{Name: diskObj.GetName()}, existingDisk)
		if err != nil {
			return fmt.Errorf("error getting exisiting disk %v", err)
		}

		if !diskObj.Equals(existingDisk) {
			log.Info("Exisiting disk differs from discovered disk. Updating.")
			diskObj.SetResourceVersion(existingDisk.GetResourceVersion())
			diskObj.Status = *existingDisk.Status.DeepCopy()
			diskObj.ObjectMeta = *existingDisk.ObjectMeta.DeepCopy()
			diskObj.Spec.Enabled = existingDisk.Spec.Enabled
			diskObj.UpdateLabels()
			err = client.Update(context.TODO(), diskObj)
			if err != nil {
				return fmt.Errorf("error updating disk: %v : %v", err, diskObj)
			}
		}
	}

	log.Info(fmt.Sprintf("Synced disk with kubernetes : %v", diskObj))
	return err
}
