// +build linux,cgo

package devices

import (
	"context"
	"errors"
	"fmt"

	"github.com/jochenvg/go-udev"
)

var ErrUnableToDetermineLocation = errors.New("unable to determine slot")

var supportedDrivers = DriverQueryMap{}

type DriverQueryMap map[string]DriverQuery

func (m DriverQueryMap) Get(driverName string) (DriverQuery, bool) {
	dq, ok := m[driverName]
	return dq, ok
}

type DriverQuery interface {
	GetLocation(*udev.Device) (string, string, error)
}

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

	driverQuery, ok := supportedDrivers.Get(driver)
	if !ok {
		return nil
	}

	disk.Driver = driver

	backplane, slot, err := driverQuery.GetLocation(dev)
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
