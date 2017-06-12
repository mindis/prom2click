package main

import (
	"database/sql"
	"fmt"
	"time"

	"sync"

	"github.com/kshvakov/clickhouse"
	"github.com/prometheus/client_golang/prometheus"
)

var insertSQL = `INSERT INTO %s.%s
	(date, name, tags, val, ts)
	VALUES	(?, ?, ?, ?, ?)`

type p2cWriter struct {
	conf     *config
	requests chan *p2cRequest
	wg       sync.WaitGroup
	db       *sql.DB
	tx       prometheus.Counter
	ko       prometheus.Counter
	timings  prometheus.Histogram
}

func NewP2CWriter(conf *config, reqs chan *p2cRequest) (*p2cWriter, error) {
	var err error
	w := new(p2cWriter)
	w.conf = conf
	w.requests = reqs
	w.db, err = sql.Open("clickhouse", w.conf.ChDSN)
	if err != nil {
		fmt.Printf("Error connecting to clickhouse: %s\n", err.Error())
		return w, err
	}

	w.tx = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "sent_samples_total",
			Help: "Total number of processed samples sent to remote storage.",
		},
	)

	w.ko = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "failed_samples_total",
			Help: "Total number of processed samples which failed on send to remote storage.",
		},
	)

	w.timings = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "sent_batch_duration_seconds",
			Help:    "Duration of sample batch send calls to the remote storage.",
			Buckets: prometheus.DefBuckets,
		},
	)
	prometheus.MustRegister(w.tx)
	prometheus.MustRegister(w.ko)
	prometheus.MustRegister(w.timings)

	return w, nil
}

func (w *p2cWriter) Start() {

	go func() {
		w.wg.Add(1)
		fmt.Println("Clickhouse writer starting..")
		sql := fmt.Sprintf(insertSQL, w.conf.ChDB, w.conf.ChTable)
		ok := true
		for ok {
			// get next batch of requests
			var reqs []*p2cRequest

			tstart := time.Now()
			for i := 0; i < w.conf.ChBatch; i++ {
				var req *p2cRequest
				// get requet and also check if channel is closed
				req, ok = <-w.requests
				if !ok {
					fmt.Println("clickhouse writer stopping..")
					break
				}
				reqs = append(reqs, req)
			}

			// ensure we have something to send..
			nmetrics := len(reqs)
			if nmetrics < 1 {
				continue
			}

			// post them to db all at once
			tx, err := w.db.Begin()
			if err != nil {
				fmt.Printf("Error: begin transaction: %s\n", err.Error())
				w.ko.Add(1.0)
				continue
			}

			// build statements
			smt, err := tx.Prepare(sql)
			for _, req := range reqs {
				if err != nil {
					fmt.Printf("Error: prepare statement: %s\n", err.Error())
					w.ko.Add(1.0)
					continue
				}

				_, err = smt.Exec(req.ts, req.name, clickhouse.Array(req.tags),
					req.val, req.ts)

				if err != nil {
					fmt.Printf("Error: statement exec: %s\n", err.Error())
					w.ko.Add(1.0)
				}
			}

			// commit and record metrics
			if err = tx.Commit(); err != nil {
				fmt.Printf("Error: commit failed: %s\n", err.Error())
				w.ko.Add(1.0)
			} else {
				w.tx.Add(float64(nmetrics))
				w.timings.Observe(float64(time.Since(tstart)))
			}

		}
		fmt.Println("clickhouse writer stopped..")
		w.wg.Done()
	}()
}

func (w *p2cWriter) Wait() {
	w.wg.Wait()
}
