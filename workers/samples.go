package workers

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/allegro/bigcache"
	"github.com/s4z/prom2click/common"
	"github.com/s4z/prom2click/config"
	"github.com/s4z/prom2click/db"
)

var (
	sqlSamples = `
		INSERT INTO samples (hash, val, ts)
			VALUES (?, ?, ?)
	`
)

type SamplesWriter struct {
	db       *sql.DB
	cache    *bigcache.BigCache
	size     int
	index    int
	metrics  []*common.Metric
	requests chan []*common.Metric
	stop     chan bool
}

func NewSamplesWriter(cfg config.Provider, c *bigcache.BigCache) (*SamplesWriter, error) {
	var (
		h   *SamplesWriter
		err error
	)
	h = new(SamplesWriter)
	h.cache = c
	h.db, err = db.NewDB(cfg)
	if err != nil {
		return nil, err
	}
	h.size = cfg.GetInt("clickhouse.batch")
	h.metrics = make([]*common.Metric, h.size, h.size)
	h.requests = make(chan []*common.Metric, h.size)
	h.stop = make(chan bool)
	return h, nil
}

func (h *SamplesWriter) Run() {
	fmt.Println("Samples handler starting..")
	stopped := false
	for {
		select {
		case <-h.stop:
			fmt.Println("Samples handler received stop")
			if !stopped {
				close(h.requests)
				stopped = true
			}
		case metrics := <-h.requests:
			fmt.Printf("Samples handler received: %d metrics\n", len(metrics))

			var (
				tx       *sql.Tx
				iSamples *sql.Stmt
				err      error
			)

			tx, err = h.db.Begin()
			if err != nil {
				fmt.Printf("Error: Samples: Begin: %s\n", err)
				break
			}

			iSamples, err = tx.Prepare(sqlSamples)
			if err != nil {
				fmt.Printf("Error: Samples: Prepare: %s\n", err)
				break
			}

			for _, m := range metrics {
				for _, s := range m.TS.Samples {
					t := time.Unix(s.TimestampMs/1000, 0)
					_, err := iSamples.Exec(m.Hash, s.Value, t)
					if err != nil {
						fmt.Printf("Error: Sample Insert: %s\n", err.Error())
						continue
					}
				}
			}

			if err = tx.Commit(); err != nil {
				fmt.Printf("Error: Samples: Commit: %s\n", err)
			}

		}
	}
}

func (h *SamplesWriter) batch() {
	// push (potentially partial) batch onto requests channel
	if h.index > 0 {
		// copy metrics so we can reset it
		c := make([]*common.Metric, h.index, h.index)
		copied := copy(c, h.metrics[0:h.index])
		if copied != h.index {
			err := "Error: failed to copy metrics: %d != %d, len: %d\n"
			fmt.Printf(fmt.Sprintf(err, h.index, copied, len(c)))
		} else {
			// push to channel
			h.requests <- c
		}
		// reset metrics for next batch
		h.index = 0
		for i := 0; i < h.size; i++ {
			h.metrics[i] = nil
		}
	}
}

func (h *SamplesWriter) Stop() {
	fmt.Println("Samples handler stopping..")
	h.batch()
	h.stop <- true
}

func (h *SamplesWriter) Wait() {
	fmt.Println("Samples handler waited..")
}

func (h *SamplesWriter) Queue(metric *common.Metric) {
	// push onto metrics until full
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
