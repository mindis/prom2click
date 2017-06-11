package main

import (
	"database/sql"
	"fmt"
	"time"

	"sync"

	"github.com/kshvakov/clickhouse"
	"github.com/prometheus/client_golang/prometheus"
)

/*
	Clickhouse SQL for expected tables - depending on metric volume and if you don't
	need replicas/ha then a MergeTree engine may be all you need. Most likley you'll
	want a ReplicatedMergeTree with at least one replica.

	For scaling use a Distributed table accross each MergeTree (or ReplicatedMergeTree).
	Prom2click doesn't understand shards and so it depends on the distributed table
	hashing to distribute writes accross the shards.

	create database if not exists metrics;

	-no replication - fine for testing:

	create table if not exists metrics.samples
	(
		date Date DEFAULT toDate(0),
		name String,
		tags Array(String),
		vals Array(String),
		value Float64,
		ts UInt32

	) ENGINE = MergeTree(date, (tags, ts), 8192);

	-or for replication - this is probably what you want:

	create table if not exists metrics.samples
	(
		date Date DEFAULT toDate(0),
		name String,
		tags Array(String),
		vals Array(String),
		val Float64,
		ts UInt32

	) ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/metrics.samples', '{replica}', date, (tags, ts), 8192);


	-and if you have more than one shard - # shards depends on your metric volumes:

	create table if not exists metrics.dist
	(
		date Date DEFAULT toDate(0),
		name String,
		tags Array(String),
		vals Array(String),
		val Float64,
		ts UInt32
	) ENGINE = Distributed(metrics, metrics, samples, intHash64(name));

*/

var insertSQL = `INSERT INTO %s.%s
	(date, name, tags, vals, val, ts)
	VALUES	(?, ?, ?, ?, ?, ?)`

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

				_, err = smt.Exec(req.date, req.name, clickhouse.Array(req.tags),
					clickhouse.Array(req.vals), req.val, req.ts)

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
