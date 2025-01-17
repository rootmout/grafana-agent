package client

import (
	"fmt"
	"sync"

	"github.com/go-kit/log"
	"github.com/grafana/agent/pkg/flow/logging/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"

	"github.com/grafana/agent/component/common/loki"
	"github.com/grafana/loki/pkg/ingester/wal"
	"github.com/grafana/loki/pkg/util"
)

// clientWriteTo implements a wal.WriteTo that re-builds entries with the stored series, and the received entries. After,
// sends each to the provided Client channel.
type clientWriteTo struct {
	series        map[chunks.HeadSeriesRef]model.LabelSet
	seriesSegment map[chunks.HeadSeriesRef]int
	seriesLock    sync.RWMutex

	logger   log.Logger
	toClient chan<- loki.Entry
}

// newClientWriteTo creates a new clientWriteTo
func newClientWriteTo(toClient chan<- loki.Entry, logger log.Logger) *clientWriteTo {
	return &clientWriteTo{
		series:        make(map[chunks.HeadSeriesRef]model.LabelSet),
		seriesSegment: make(map[chunks.HeadSeriesRef]int),
		toClient:      toClient,
		logger:        logger,
	}
}

func (c *clientWriteTo) StoreSeries(series []record.RefSeries, segment int) {
	c.seriesLock.Lock()
	defer c.seriesLock.Unlock()
	for _, seriesRec := range series {
		c.seriesSegment[seriesRec.Ref] = segment
		labels := util.MapToModelLabelSet(seriesRec.Labels.Map())
		c.series[seriesRec.Ref] = labels
	}
}

// SeriesReset will delete all cache entries that were last seen in segments numbered equal or lower than segmentNum
func (c *clientWriteTo) SeriesReset(segmentNum int) {
	c.seriesLock.Lock()
	defer c.seriesLock.Unlock()
	for k, v := range c.seriesSegment {
		if v <= segmentNum {
			level.Debug(c.logger).Log("msg", fmt.Sprintf("reclaiming series under segment %d", segmentNum))
			delete(c.seriesSegment, k)
			delete(c.series, k)
		}
	}
}

func (c *clientWriteTo) AppendEntries(entries wal.RefEntries, _ int) error {
	var entry loki.Entry
	c.seriesLock.RLock()
	l, ok := c.series[entries.Ref]
	c.seriesLock.RUnlock()
	if ok {
		entry.Labels = l
		for _, e := range entries.Entries {
			entry.Entry = e
			c.toClient <- entry
		}
	} else {
		// TODO(thepalbi): Add metric here
		level.Debug(c.logger).Log("msg", "series for entry not found")
	}
	return nil
}
