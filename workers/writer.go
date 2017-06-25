package workers

import (
	"io/ioutil"
	"net/http"
	"sync"

	"github.com/allegro/bigcache"
	"github.com/golang/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/storage/remote"
	"github.com/s4z/prom2click/common"
	"github.com/s4z/prom2click/config"
)

type WriteWorker struct {
	cache    *bigcache.BigCache
	wgroup   sync.WaitGroup
	hsamples *SamplesWriter
	hmetrics *MetricsWriter
}

func NewWriteWorker(cache *bigcache.BigCache,
	cfg config.Provider) (*WriteWorker, error) {
	var (
		w   *WriteWorker
		err error
	)
	w = new(WriteWorker)
	w.cache = cache

	w.hsamples, err = NewSamplesWriter(cfg, w.cache)
	if err != nil {
		return nil, err
	}

	w.hmetrics, err = NewMetricsWriter(cfg, w.cache, w.hsamples)
	if err != nil {
		return nil, err
	}

	return w, nil
}

func (w *WriteWorker) Queue(req *http.Request) error {
	// http request should be a snappy compressed protobuf message
	var (
		hash, dat, buf []byte
		err            error
	)
	// http request post data is a ~write request
	dat, err = ioutil.ReadAll(req.Body)
	if err != nil {
		return err
	}
	// decompress
	buf, err = snappy.Decode(nil, dat)
	if err != nil {
		return err
	}
	// decode
	wreq := new(remote.WriteRequest)
	if err := proto.Unmarshal(buf, wreq); err != nil {
		return err
	}

	// each request can have multiple time series
	for _, ts := range wreq.Timeseries {
		// for each timeseries create a metric
		m := new(common.Metric)
		m.Metric = common.FingerPrint(ts)
		m.TS = ts
		// get the corresponding hash from cache
		hash, err = w.cache.Get(m.Metric)
		if err != nil {
			// either metric not yet cached or not yet created in db
			// send to metrics worker to handle either case
			w.hmetrics.Queue(m)
		} else {
			m.Hash = string(hash)
			// metric found in cache - send to samples worker
			w.hsamples.Queue(m)
		}
	}

	return nil
}

func (w *WriteWorker) Run() {
	go w.hsamples.Run()
	go w.hmetrics.Run()
}

func (w *WriteWorker) Stop() {
	// first stop the metrics worker
	w.hmetrics.Stop()
	// wait for it to process its queue
	// todo: timeout on it
	w.hmetrics.Wait()
	// last stop the samples writer
	w.hsamples.Stop()
}

func (w *WriteWorker) Wait() {
	// wait for the samples writer to flush its queue
	// todo: timeout on samples writer wait
	w.hsamples.Wait()
	// todo: timeout on waiting for our worker to complete
	w.wgroup.Wait()
}
