// +build linux,cgo

package devices

import (
	"path"
	"regexp"

	"github.com/jochenvg/go-udev"
)

func init() {
	supportedDrivers["ahci"] = &Ahci{}
}

type Ahci struct{}

func (d *Ahci) GetLocation(dev *udev.Device) (string, string, error) {
	scsiHost := dev.ParentWithSubsystemDevtype("scsi", "scsi_host")
	re := regexp.MustCompile("host([0-9]+)")
	matches := re.FindStringSubmatch(path.Base(scsiHost.Syspath()))
	if len(matches) != 2 {
		// Unable to determine scsi_host index
		return "", "", ErrUnableToDetermineLocation
	}

	return "SATA", matches[1], nil
}
