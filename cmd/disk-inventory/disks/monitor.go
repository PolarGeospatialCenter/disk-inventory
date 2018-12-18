package disks

import (
	"context"
	"time"

	"github.com/PolarGeospatialCenter/disk-inventory/cmd/disk-inventory/devices"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("monitor_disks")

type NodeDataGetter interface {
	GetName() string
}

type Monitor struct {
	Node     NodeDataGetter
	Interval time.Duration
	scanCh   chan struct{}
}

func (m *Monitor) Scan() {
	if m.scanCh == nil {
		m.scanCh = make(chan struct{}, 1)
	}
	m.scanCh <- struct{}{}
}

func (m *Monitor) Start(ctx context.Context) chan devices.DiskUpdate {
	update := make(chan devices.DiskUpdate)
	timer := time.NewTicker(m.Interval)
	m.Scan()
	go func(update chan devices.DiskUpdate) {
		diskMonitor := devices.DiskMonitor{}
		ch := diskMonitor.Start(ctx)
		for {
			select {
			case diskUpdate := <-ch:
				update <- diskUpdate

			case <-m.scanCh:
				disks, err := devices.EnumerateDisks()
				if err != nil {
					log.Error(err, "error enumerating disks")
				}
				for _, d := range disks {
					u := devices.DiskUpdate{Disk: *d, Action: devices.DiskNoop}
					update <- u
				}

			case <-timer.C:
				disks, err := devices.EnumerateDisks()
				if err != nil {
					log.Error(err, "error enumerating disks")
				}
				for _, d := range disks {
					u := devices.DiskUpdate{Disk: *d, Action: devices.DiskNoop}
					update <- u
				}
			case <-ctx.Done():
				break
			}
		}
	}(update)
	return update
}
