package main

import (
	"context"
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
	"github.com/sirupsen/logrus"
	errapi "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"
)

func printVersion() {
	logrus.Infof("Go Version: %s", goruntime.Version())
	logrus.Infof("Go OS/Arch: %s/%s", goruntime.GOOS, goruntime.GOARCH)
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
	flag.Parse()

	printVersion()

	hostname := os.Getenv("NODE_NAME")
	node := &Node{Name: hostname}

	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	options := client.Options{
		Scheme: scheme.Scheme,
	}

	clientset, err := client.New(config, options)
	if err != nil {
		panic(err.Error())
	}

	err = apis.AddToScheme(options.Scheme)
	if err != nil {
		panic(err.Error())
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
		logrus.Infof("Entering detection loop")
		select {
		case d := <-diskch:
			//updatedb
			logrus.Infof("Recevied a disk update from udev: %v", d)

			if d.Disk.GetWwn() == "" {
				logrus.Errorf("Recevied a disk without a WWN... Skipping: %v", d.Disk.GetDevName())
				continue
			}

			err := syncDisk(*node, d.Disk, clientset)
			if err != nil {
				logrus.Errorf("Error syncing disk: %v", err)
			}
		case <-stopch:
			os.Exit(0)
		}
	}
}

func buildDiskObject(disk devices.Disk, node Node) *v1alpha1.Disk {
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

	return k8sDisk
}

//syncDisk syncs a single disk with the Kubernetes api.
func syncDisk(node Node, disk devices.Disk, client Client) error {
	k8sDisk := buildDiskObject(disk, node)

	diskObj := k8sDisk.DeepCopy()
	diskObj.Status.PreparePhase = v1alpha1.StoragePreparePhaseDiscovered

	err := client.Create(context.TODO(), diskObj)
	if errapi.IsAlreadyExists(err) {
		existingDisk := &v1alpha1.Disk{}

		err = client.Get(context.TODO(), types.NamespacedName{Name: diskObj.GetName()}, existingDisk)
		if err != nil {
			return fmt.Errorf("Error getting exisiting disk %v", err)
		}

		if !diskObj.Equals(existingDisk) {
			logrus.Warnf("Exisiting disk differs from discovered disk. Updating.")
			diskObj.SetResourceVersion(existingDisk.GetResourceVersion())
			diskObj.Status = *existingDisk.Status.DeepCopy()
			diskObj.ObjectMeta = *existingDisk.ObjectMeta.DeepCopy()
			diskObj.UpdateLabels()
			err = client.Update(context.TODO(), diskObj)
			if err != nil {
				return fmt.Errorf("Error updating disk: %v : %v", err, diskObj)
			}
		}
	}

	return err
}
