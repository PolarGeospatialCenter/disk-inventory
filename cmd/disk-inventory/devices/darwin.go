// +build darwin

package devices

import (
	"context"
)

func (m *DiskMonitor) Start(ctx context.Context) chan DiskUpdate {
	updateCh := make(chan DiskUpdate)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			}
		}
	}()
	return updateCh
}

func EnumerateDisks() ([]*Disk, error) {
	var disks []*Disk
	return disks, nil
}
