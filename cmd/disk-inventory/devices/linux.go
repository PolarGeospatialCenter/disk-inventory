// +build linux,cgo

package devices

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/jochenvg/go-udev"
)

var supportedDrivers = map[string]struct{}{"megaraid_sas": struct{}{}, "ahci": struct{}{}, "mpt3sas": struct{}{}}

func (m *DiskMonitor) Start(ctx context.Context) chan DiskUpdate {
	updateCh := make(chan DiskUpdate)
	go func() {
		u := udev.Udev{}
		um := u.NewMonitorFromNetlink("udev")
		um.FilterAddMatchSubsystemDevtype("block", "disk")
		ch, _ := um.DeviceChan(ctx)
		for {
			select {
			case d := <-ch:
				action := DiskNoop
				switch d.Action() {
				case "add":
					action = DiskInsert
				case "remove":
					action = DiskRemove
				}

				if disk := newDiskFromDevice(d); disk != nil {
					updateCh <- DiskUpdate{Disk: *disk, Action: action}
				}

			case <-ctx.Done():
				return
			}
		}
	}()
	return updateCh
}

func findScsiDriver(dev *udev.Device) string {
	scsiHost := dev.ParentWithSubsystemDevtype("scsi", "scsi_host")
	if scsiHost == nil {
		return ""
	}
	p := scsiHost.Parent()
	for p != nil {
		if p.Driver() != "" {
			return p.Driver()
		}
		p = p.Parent()
	}
	return ""
}

func findSlot(dev *udev.Device) string {
	return ""
}

func findBackplane(dev *udev.Device) string {
	return ""
}

func EnumerateDisks() ([]*Disk, error) {
	var disks []*Disk
	u := udev.Udev{}
	e := u.NewEnumerate()
	e.AddMatchSubsystem("block")
	e.AddMatchProperty("DEVTYPE", "disk")
	e.AddMatchIsInitialized()
	devs, err := e.Devices()

	if err != nil {
		return nil, fmt.Errorf("Error getting Udev Devices: %v", err)
	}

	for _, d := range devs {
		disk := newDiskFromDevice(d)

		if disk != nil {
			disks = append(disks, disk)
		}
	}

	return disks, nil
}

func newDiskFromDevice(dev *udev.Device) *Disk {
	disk := &Disk{}

	driver := findScsiDriver(dev)

	if _, ok := supportedDrivers[driver]; !ok {
		return nil
	}

	disk.Driver = driver

	backplane, slot, err := findLocation(dev)
	if err == nil {
		disk.Backplane = backplane
		disk.Slot = slot
	}

	disk.Properties = dev.Properties()

	sysAttrMap := dev.Sysattrs()
	disk.Attributes = make(map[string]string, len(sysAttrMap))
	for k, _ := range sysAttrMap {
		disk.Attributes[k] = dev.SysattrValue(k)
	}
	return disk
}

var ErrUnableToDetermineLocation = errors.New("unable to determine slot")

func findLocation(dev *udev.Device) (string, string, error) {

	switch findScsiDriver(dev) {
	case "ahci":
		return getSataLocation(dev)
	case "megaraid_sas":
		return getMegaRaidLocation(dev)
	default:
		return getSasExpanderLocation(dev)
	}
}

//For Dell R620
func getMegaRaidLocation(dev *udev.Device) (string, string, error) {
	scsiHost := dev.Parent()
	matches := strings.Split(path.Base(scsiHost.Syspath()), ":")
	if len(matches) != 4 {
		// Unable to determine scsi_host index
		return "", "", ErrUnableToDetermineLocation
	}

	return "SCSI", matches[2], nil
}

func getSataLocation(dev *udev.Device) (string, string, error) {
	scsiHost := dev.ParentWithSubsystemDevtype("scsi", "scsi_host")
	re := regexp.MustCompile("host([0-9]+)")
	matches := re.FindStringSubmatch(path.Base(scsiHost.Syspath()))
	if len(matches) != 2 {
		// Unable to determine scsi_host index
		return "", "", ErrUnableToDetermineLocation
	}

	return "SATA", matches[1], nil
}

func getClosestSasExpander(dev *udev.Device) *udev.Device {
	p := dev.Parent()
	for p != nil {
		expString := path.Base(p.Syspath())
		expanderPath := path.Join(p.Syspath(), "sas_expander", expString)
		_, err := os.Stat(expanderPath)
		if err == nil {
			// found closest expander
			u := udev.Udev{}
			return u.NewDeviceFromSyspath(expanderPath)
		}
		p = p.Parent()
	}
	return nil
}

func getSasDevice(dev *udev.Device) (*udev.Device, error) {
	scsiTarget := dev.ParentWithSubsystemDevtype("scsi", "scsi_target")
	if scsiTarget != nil {
		endDevice := scsiTarget.Parent()
		edString := path.Base(endDevice.Syspath())
		sasDevicePath := path.Join(endDevice.Syspath(), "sas_device", edString)
		u := udev.Udev{}
		sasDevice := u.NewDeviceFromSyspath(sasDevicePath)
		if sasDevice == nil {
			return nil, fmt.Errorf("unable to create device from syspath for sas device: %s", sasDevicePath)
		}
		return sasDevice, nil
	}
	return nil, fmt.Errorf("unable to find end device")
}

func getSasTargetBayId(dev *udev.Device) (string, error) {
	sasDevice, err := getSasDevice(dev)
	if err != nil {
		return "", err
	}
	return sasDevice.SysattrValue("bay_identifier"), nil
}

func getSasTargetEnclosureId(dev *udev.Device) (string, error) {
	sasDevice, err := getSasDevice(dev)
	if err != nil {
		return "", err
	}
	return sasDevice.SysattrValue("enclosure_identifier"), nil
}

func getSasExpanderLocation(dev *udev.Device) (string, string, error) {
	var backPlane string
	var slot string
	var err error

	enclosureId, err := getSasTargetEnclosureId(dev)
	if err != nil {
		return backPlane, slot, ErrUnableToDetermineLocation
	}

	if expander := getClosestSasExpander(dev); expander != nil {
		productID := strings.TrimSpace(expander.SysattrValue("product_id"))
		switch productID {
		case "SAS2X36":
			backPlane = "Front"
		case "SAS2X28":
			backPlane = "Rear"
		case "":
			backPlane = enclosureId
		default:
			backPlane = fmt.Sprintf("%s-%s", productID, enclosureId)
		}
	}

	slot, err = getSasTargetBayId(dev)
	if err != nil {
		return backPlane, slot, ErrUnableToDetermineLocation
	}

	return backPlane, slot, nil
}
