package main

import (
	"io/ioutil"
	"net/http"
	"time"

	"fmt"

	"github.com/golang/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/storage/remote"
	"gopkg.in/tylerb/graceful.v1"
)

type p2cRequest struct {
	name string
	tags []string
	vals []string
	val  float64
	ts   time.Time
}

type p2cServer struct {
	requests chan *p2cRequest
	mux      *http.ServeMux
	conf     *config
	writer   *p2cWriter
	rx       prometheus.Counter
}

func NewP2CServer(conf *config) (*p2cServer, error) {
	var err error
	c := new(p2cServer)
	c.requests = make(chan *p2cRequest, conf.ChanSize)
	c.mux = http.NewServeMux()
	c.conf = conf

	c.writer, err = NewP2CWriter(conf, c.requests)
	if err != nil {
		fmt.Printf("Error creating clickhouse writer: %s\n", err.Error())
		return c, err
	}

	c.rx = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "received_samples_total",
			Help: "Total number of received samples.",
		},
	)
	prometheus.MustRegister(c.rx)

	c.mux.HandleFunc(c.conf.HTTPWritePath, func(w http.ResponseWriter, r *http.Request) {
		compressed, err := ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		reqBuf, err := snappy.Decode(nil, compressed)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var req remote.WriteRequest
		if err := proto.Unmarshal(reqBuf, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		c.process(req)
	})

	c.mux.Handle(c.conf.HTTPMetricsPath, prometheus.InstrumentHandler(
		c.conf.HTTPMetricsPath, prometheus.UninstrumentedHandler(),
	))

	return c, nil
}

func (c *p2cServer) process(req remote.WriteRequest) {
	for _, series := range req.Timeseries {
		c.rx.Add(float64(len(series.Samples)))
		var (
			name string
			tags []string
			vals []string
		)

		for _, label := range series.Labels {
			if model.LabelName(label.Name) == model.MetricNameLabel {
				name = label.Value
			}
			tags = append(tags, label.Name)
			vals = append(vals, label.Value)
		}

		for _, sample := range series.Samples {
			p2c := new(p2cRequest)
			p2c.name = name
			p2c.ts = time.Unix(sample.TimestampMs/1000, 0)
			p2c.val = sample.Value
			p2c.tags = tags
			p2c.vals = vals
			c.requests <- p2c
		}

	}
}

func (c *p2cServer) Start() error {
	fmt.Println("Clickhouse http server starting...")
	c.writer.Start()
	return graceful.RunWithErr(c.conf.HTTPAddr, c.conf.HTTPTimeout, c.mux)
}

func (c *p2cServer) Shutdown() {
	close(c.requests)
	c.writer.Wait()

	wchan := make(chan struct{})
	go func() {
		c.writer.Wait()
		close(wchan)
	}()

	select {
	case <-wchan:
	fmt.Println("Writer shutdown cleanly..")
	// All done!
	case <-time.After(10 * time.Second):
		fmt.Println("Writer shutdown timed out, samples will be lost..")
	}

}
