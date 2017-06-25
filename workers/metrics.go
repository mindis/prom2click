package workers

import (
	"database/sql"
	"fmt"

	"github.com/allegro/bigcache"
	"github.com/s4z/prom2click/common"
	"github.com/s4z/prom2click/config"
	"github.com/s4z/prom2click/db"
)

type MetricsWriter struct {
	db       *sql.DB
	cache    *bigcache.BigCache
	size     int
	index    int
	metrics  []*common.Metric
	stop     chan bool
	requests chan []*common.Metric
	swriter  *SamplesWriter
	stopped  bool
}

func NewMetricsWriter(cfg config.Provider,
	c *bigcache.BigCache, hs *SamplesWriter) (*MetricsWriter, error) {
	var (
		h   *MetricsWriter
		err error
	)
	h = new(MetricsWriter)
	h.cache = c
	h.db, err = db.NewDB(cfg)
	if err != nil {
		return nil, err
	}
	h.size = cfg.GetInt("clickhouse.batch")
	h.metrics = make([]*common.Metric, h.size, h.size)
	h.requests = make(chan []*common.Metric, h.size)
	h.stop = make(chan bool)
	h.swriter = hs
	return h, nil
}

func (h *MetricsWriter) Run() {
	fmt.Println("Metrics handler starting..")
	var (
		tx  *db.MetricTx
		err error
	)
	h.stopped = false
	tx = db.NewMetricTx(h.db, h.cache)
	for {
		select {
		case <-h.stop:
			fmt.Println("Metrics handler received stop")
			if !h.stopped {
				close(h.requests)
				h.stopped = true
			}
		case metrics := <-h.requests:
			var creates []*common.Metric

			for _, metric := range metrics {
				// does the metric exist in the db?
				hash, err := tx.FindHash(metric)
				// if error and not a No Rows error skip
				if err != nil && err != sql.ErrNoRows {
					fmt.Printf("Error searching for hash: %s\n", err)
					continue
				} else if hash != "" {
					// found a hash, metric exists so simply write points
					metric.Hash = hash
					h.swriter.Queue(metric)
				} else {
					// didn't find a hash, need to create the metric
					hash, err = tx.GetHash(metric)
					// write points, metric will eventually get created
					if err != nil {
						fmt.Printf("Error: failed to get metric hash: %s\n", err)
						continue
					}
					metric.Hash = hash
					h.swriter.Queue(metric)
					creates = append(creates, metric)
				}
			}
			if len(creates) > 0 {
				err = tx.CreateMetrics(creates)
				if err != nil {
					fmt.Printf("Error: Failed to create Metrics: %s\n", err.Error())
				}
				err = tx.CreateHashes(creates)
				if err != nil {
					fmt.Printf("Error: Failed to create Hashes: %s\n", err.Error())
				}
			}
		}
	}
}

func (h *MetricsWriter) batch() {
	// push the next (potentially partial) batch onto requests channel
	if h.index > 0 {
		c := make([]*common.Metric, h.index, h.index)
		copied := copy(c, h.metrics)
		if copied != h.index {
			err := "Error: failed to copy metrics: %d != %d\n"
			fmt.Printf(fmt.Sprintf(err, h.index, copied))
		} else {
			// push to channel
			h.requests <- c
		}
		h.index = 0
		for i := 0; i < h.size; i++ {
			h.metrics[i] = nil
		}
	}
}

func (h *MetricsWriter) Stop() {
	fmt.Println("Metrics handler stopping..")
	h.batch()
	h.stop <- true
}

func (h *MetricsWriter) Wait() {
	fmt.Println("Metrics handler waited..")
}

func (h *MetricsWriter) Queue(metric *common.Metric) {
	// push onto metrics [] until full
	// create new metrics and copy existing onto channel
	// for async processing
	// this method is not thread safe!
	if h.index < h.size {
		h.metrics[h.index] = metric
		h.index++
	} else {
		h.batch()
	}
}
