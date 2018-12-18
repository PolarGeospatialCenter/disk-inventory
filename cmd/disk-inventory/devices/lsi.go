// +build linux,cgo

package devices

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/jochenvg/go-udev"
)

func init() {
	supportedDrivers["megaraid_sas"] = &MegaRaidSAS{}
	supportedDrivers["mpt3sas"] = &MPTSAS{}
}

type MegaRaidSAS struct{}

func (d *MegaRaidSAS) GetLocation(dev *udev.Device) (string, string, error) {
	scsiHost := dev.Parent()
	matches := strings.Split(path.Base(scsiHost.Syspath()), ":")
	if len(matches) != 4 {
		// Unable to determine scsi_host index
		return "", "", ErrUnableToDetermineLocation
	}

	return "SCSI", matches[2], nil
}

type MPTSAS struct{}

func (d *MPTSAS) getClosestSasExpander(dev *udev.Device) *udev.Device {
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

func (d *MPTSAS) getSasDevice(dev *udev.Device) (*udev.Device, error) {
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

func (d *MPTSAS) getSasTargetBayId(dev *udev.Device) (string, error) {
	sasDevice, err := d.getSasDevice(dev)
	if err != nil {
		return "", err
	}
	return sasDevice.SysattrValue("bay_identifier"), nil
}

func (d *MPTSAS) getSasTargetEnclosureId(dev *udev.Device) (string, error) {
	sasDevice, err := d.getSasDevice(dev)
	if err != nil {
		return "", err
	}
	return sasDevice.SysattrValue("enclosure_identifier"), nil
}

func (d *MPTSAS) GetLocation(dev *udev.Device) (string, string, error) {
	var backPlane string
	var slot string
	var err error

	enclosureId, err := d.getSasTargetEnclosureId(dev)
	if err != nil {
		return backPlane, slot, ErrUnableToDetermineLocation
	}

	if expander := d.getClosestSasExpander(dev); expander != nil {
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

	slot, err = d.getSasTargetBayId(dev)
	if err != nil {
		return backPlane, slot, ErrUnableToDetermineLocation
	}

	return backPlane, slot, nil
}
