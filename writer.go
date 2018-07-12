package main

import (
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/kshvakov/clickhouse"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

var insertSQL = `INSERT INTO %s.%s
	(date, name, tags, val, ts)
	VALUES	(?, ?, ?, ?, ?)`

var writerContent = []interface{}{"component", "writer"}

type p2cWriter struct {
	conf     *config
	requests chan *p2cRequest
	wg       sync.WaitGroup
	db       *sql.DB
	tx       prometheus.Counter
	ko       prometheus.Counter
	test     prometheus.Counter
	timings  prometheus.Histogram

	logger *zap.SugaredLogger
}

func NewP2CWriter(conf *config, reqs chan *p2cRequest, sugar *zap.SugaredLogger) (*p2cWriter, error) {
	var err error
	w := new(p2cWriter)
	w.conf = conf
	w.requests = reqs
	w.logger = sugar
	w.db, err = sql.Open("clickhouse", w.conf.ChDSN)
	if err != nil {
		w.logger.With(writerContent...).Errorf("connecting to clickhouse: %s", err.Error())
		return w, err
	}

	if err := w.db.Ping(); err != nil {
		if exception, ok := err.(*clickhouse.Exception); ok {
			w.logger.With(writerContent...).Errorf("[%d] %s \n%s\n", exception.Code, exception.Message, exception.StackTrace)
		} else {
			w.logger.With(writerContent...).Error(err.Error())
		}
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

	w.test = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "prometheus_remote_storage_sent_batch_duration_seconds_bucket_test",
			Help: "Test metric to ensure backfilled metrics are readable via prometheus.",
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
	prometheus.MustRegister(w.test)
	prometheus.MustRegister(w.timings)

	return w, nil
}

func (w *p2cWriter) Start() {

	go func() {
		w.wg.Add(1)
		w.logger.With(writerContent...).Info("Writer starting..")
		sql := fmt.Sprintf(insertSQL, w.conf.ChDB, w.conf.ChTable)
		ok := true
		for ok {
			w.test.Add(1)
			// get next batch of requests
			var reqs []*p2cRequest

			tstart := time.Now()
			for i := 0; i < w.conf.ChBatch; i++ {
				var req *p2cRequest
				// get requet and also check if channel is closed
				req, ok = <-w.requests
				if !ok {
					w.logger.With(writerContent...).Info("Writer stopping..")
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
				w.logger.With(writerContent...).Errorf("begin transaction: %s", err.Error())
				w.ko.Add(1.0)
				continue
			}

			// build statements
			smt, err := tx.Prepare(sql)
			for _, req := range reqs {
				if err != nil {
					w.logger.With(writerContent...).Errorf("prepare statement: %s", err.Error())
					w.ko.Add(1.0)
					continue
				}

				// ensure tags are inserted in the same order each time
				// possibly/probably impacts indexing?
				sort.Strings(req.tags)
				_, err = smt.Exec(req.ts, req.name, clickhouse.Array(req.tags),
					req.val, req.ts)

				if err != nil {
					w.logger.With(writerContent...).Errorf("statement exec: %s", err.Error())
					w.ko.Add(1.0)
				}
			}

			// commit and record metrics
			if err = tx.Commit(); err != nil {
				w.logger.With(writerContent...).Errorf("commit failed: %s", err.Error())
				w.ko.Add(1.0)
			} else {
				w.tx.Add(float64(nmetrics))
				w.timings.Observe(float64(time.Since(tstart)))
			}

		}
		w.logger.With(writerContent...).Info("Writer stopped..")
		w.wg.Done()
	}()
}

func (w *p2cWriter) Wait() {
	w.wg.Wait()
}
