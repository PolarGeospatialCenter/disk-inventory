package devices

import (
	"fmt"
	"strconv"
)

type DiskAction int

const (
	DiskInsert DiskAction = iota
	DiskRemove
	DiskNoop
)

type DiskUpdate struct {
	Disk   Disk
	Action DiskAction
}

type DiskMonitor struct{}

type Disk struct {
	Backplane  string
	Slot       string
	Driver     string
	Properties map[string]string
	Attributes map[string]string
}

func (d *Disk) Name() string {
	return fmt.Sprintf("wwn-%s", d.GetWwn())
}

func (d *Disk) GetCapacityBytes() uint64 {
	sectorString, _ := d.Attributes["size"]
	sectors, err := strconv.ParseUint(sectorString, 10, 64)
	if err != nil {
		sectors = 0
	}

	bytes := sectors * 512

	return bytes
}

func (d *Disk) GetBackplane() string {
	return d.Backplane
}

func (d *Disk) GetSlot() string {
	return d.Slot
}

func (d *Disk) GetModel() string {
	return d.Properties["ID_MODEL"]
}

func (d *Disk) GetSerialNumber() string {
	return d.Properties["ID_SERIAL_SHORT"]
}

func (d *Disk) GetWwn() string {
	return d.Properties["ID_WWN_WITH_EXTENSION"]
}

func (d *Disk) GetDriver() string {
	return d.Driver
}

func (d *Disk) GetDevName() string {
	return d.Properties["DEVNAME"]
}
