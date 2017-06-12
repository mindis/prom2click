package main

import (
	"database/sql"
	"fmt"

	"github.com/prometheus/prometheus/storage/remote"
)

/*
	misc notes on adding remote reader functionality..

	need:
		-conf for quantile value for ts value aggregation
		-conf for max # samples to return per timeseries
		-conf for min time period for aggregation (eg. poll interval of prometheus)
		-algo to determine aggregation period for "group by t" from max # samples (above)
		-query builder that supports each type of matcher (eq, neq, regex, nregex)
		-sample/response builder that takes clickhouse results and returns samples


	matcher types:
		EQUAL
		NOT_EQUAL
		REGEX_MATCH
		REGEX_NO_MATCH

	also:
		looks like each match type can have multiple values (see below) eg.

			matchers:<name:"handler" value:"alertmanagers|alerts|config|consoles|drop_series|federate|flags|graph|heap|label_values|options|prometheus|query|query_range|rules|series|static|status|targets|version" >


	some sqlish ideas:

	// regular expression match - note: also possible to use or in arrayExists
	select count() AS CNT, (intDiv(toUInt32(ts), 3600) * 3600) * 1000 as t,
		name,
		tags,
		quantile(0.9)(val)
	from metrics.samples
	where arrayExists(
		x -> 1 == match(x, '\^__name__=go_memstats_alloc.\*'),
		tags
	) = 1
	GROUP BY t, name, tags
	ORDER BY t
	limit 500;


	// regular expression no match:
	select count() AS CNT, (intDiv(toUInt32(ts), 3600) * 3600) * 1000 as t,
		name,
		tags,
		quantile(0.9)(val)
	from metrics.samples
	where arrayExists(
		x -> 1 == match(x, '\^__name__=go_memstats_alloc.\*'),
		tags
	) = 0
	GROUP BY t, name, tags
	ORDER BY t
	limit 500;


	// match - build IN () statement for multiple values
	// 	-or- for single value x == value
	select count() AS CNT, (intDiv(toUInt32(ts), 3600) * 3600) * 1000 as t,
		name,
		tags,
		quantile(0.9)(val)
	from metrics.samples
	where arrayExists(
	    x -> x IN ('__name__=go_gc_duration_seconds'),
	    tags
	) = 1
	GROUP BY t, name, tags
	ORDER BY t
	limit 500;


	// no-match - same as match however where arrayExits = 0
	select count() AS CNT, (intDiv(toUInt32(ts), 3600) * 3600) * 1000 as t,
		name,
		tags,
		quantile(0.9)(val)
	from metrics.samples
	where arrayExists(
	    x -> x IN ('__name__=go_gc_duration_seconds'),
	    tags
	) = 0
	GROUP BY t, name, tags
	ORDER BY t
	limit 500;



	a few example remote read requests from prometheus:


	start_timestamp_ms:1497145508000
	end_timestamp_ms:1497231968000
	matchers:<name:"instance" value:"localhost:9090" >
	matchers:<name:"__name__" value:"go_memstats_buck_hash_sys_bytes" >
	matchers:<name:"job" value:"prometheus" >


	Reader: rx read request
	start_timestamp_ms:1497167948000
	end_timestamp_ms:1497254408000
	matchers:<name:"instance" value:"localhost:9090" >
	matchers:<name:"__name__" value:"http_request_duration_microseconds" >
	matchers:<type:NOT_EQUAL name:"handler" value:"alerts" >
	matchers:<type:REGEX_MATCH name:"job" value:"^prometheus$" >
	matchers:<type:REGEX_NO_MATCH name:"handler" value:"^alertmana.*" >



	start_timestamp_ms:1497163400000
	end_timestamp_ms:1497249860000
	matchers:<name:"instance" value:"localhost:9090" >
	matchers:<name:"handler" value:"alertmanagers|alerts|config|consoles|drop_series|federate|flags|graph|heap|label_values|options|prometheus|query|query_range|rules|series|static|status|targets|version" >
	matchers:<name:"__name__" value:"http_request_duration_microseconds_count" >
	matchers:<name:"job" value:"prometheus" >


	start_timestamp_ms:1497165574000
	end_timestamp_ms:1497252034000
	matchers:<name:"instance" value:"localhost:9090" >
	matchers:<name:"__name__" value:"go_gc_duration_seconds_sum" >
	matchers:<type:REGEX_MATCH name:"job" value:"^prometheus$" >


	Reader: rx read request
	start_timestamp_ms:1497167714000
	end_timestamp_ms:1497254174000
	matchers:<name:"instance"
	value:"localhost:9090" >
	matchers:<name:"__name__"
	value:"http_request_duration_microseconds" >
	matchers:<type:REGEX_MATCH name:"job" value:"^prometheus$" >
	matchers:<type:REGEX_NO_MATCH name:"handler" value:"alerts" >


	more misc sql..

	// example from clickhouse grafana datasource:
	SELECT (intDiv(toUInt32(ts), 20) * 20) * 1000 as t,
		median(val) FROM metrics.samples
	WHERE
		date >= toDate(1497209095) AND
		ts >= toDateTime(1497209095) AND
		name = 'go_memstats_frees_total'
	GROUP BY t, tags ORDER BY t

	// multiple regex example:
	select count() AS CNT, (intDiv(toUInt32(ts), 3600) * 3600) * 1000 as t,
		name,
		tags,
		quantile(0.9)(val)
	from metrics.samples
	where arrayExists(
		x -> 1 == match(x, '\^__name__=go_memstats_alloc.\*') or 1 == match(x, '\^__name__=http.\*'),
		tags
	) = 1
	GROUP BY t, name, tags
	ORDER BY t
	limit 500;

*/

type p2cReader struct {
	conf *config
	db   *sql.DB
}

func NewP2CReader(conf *config) (*p2cReader, error) {
	var err error
	r := new(p2cReader)
	r.conf = conf
	r.db, err = sql.Open("clickhouse", r.conf.ChDSN)
	if err != nil {
		fmt.Printf("Error connecting to clickhouse: %s\n", err.Error())
		return r, err
	}

	return r, nil
}

func (r *p2cReader) Read(reqs *remote.ReadRequest) (*remote.ReadResponse, error) {
	fmt.Println("Reader: rx read request")
	for _, req := range reqs.Queries {
		fmt.Println(req)
	}

	resp := remote.ReadResponse{
		Results: []*remote.QueryResult{
			{Timeseries: make([]*remote.TimeSeries, 0, 0)},
		},
	}

	fmt.Println("\n\n")
	return &resp, nil

	/*
		// todo: replace this with reader code..
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
	*/
}
