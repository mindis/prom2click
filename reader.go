package main

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/storage/remote"
)

type p2cReader struct {
	conf *config
	db   *sql.DB
}

// getTimePeriod return select and where SQL chunks relating to the time period -or- error
func (r *p2cReader) getTimePeriod(query *remote.Query) (string, string, error) {

	var tselSQL = "SELECT COUNT() AS CNT, (intDiv(toUInt32(ts), %d) * %d) * 1000 as t"
	var twhereSQL = "WHERE date >= toDate(%d) AND ts >= toDateTime(%d) AND ts <= toDateTime(%d)"
	var err error
	tstart := query.StartTimestampMs / 1000
	tend := query.EndTimestampMs / 1000

	// valid time period
	if tend < tstart {
		err = errors.New("Start time is after end time")
		return "", "", err
	}

	// need time period in seconds
	tperiod := tend - tstart

	// need to split time period into <nsamples> - also, don't divide by zero
	if r.conf.CHMaxSamples < 1 {
		err = fmt.Errorf(fmt.Sprintf("Invalid CHMaxSamples: %d", r.conf.CHMaxSamples))
		return "", "", err
	}
	taggr := tperiod / int64(r.conf.CHMaxSamples)
	if taggr < int64(r.conf.CHMinPeriod) {
		taggr = int64(r.conf.CHMinPeriod)
	}

	selectSQL := fmt.Sprintf(tselSQL, taggr, taggr)
	whereSQL := fmt.Sprintf(twhereSQL, tstart, tstart, tend)

	return selectSQL, whereSQL, nil
}

func (r *p2cReader) getSQL(query *remote.Query) (string, error) {
	// time related select sql, where sql chunks
	tselectSQL, twhereSQL, err := r.getTimePeriod(query)
	if err != nil {
		return "", err
	}

	// match sql chunk
	var mwhereSQL []string
	// build an sql statement chunk for each matcher in the query
	// yeah, this is a bit ugly..
	for _, m := range query.Matchers {
		// __name__ is handled specially - match it directly
		// as it is stored in the name column (it's also in tags as __name__)
		// note to self: add name to index.. otherwise this will be slow..
		if m.Name == model.MetricNameLabel {
			var whereAdd string
			switch m.Type {
			case remote.MatchType_EQUAL:
				whereAdd = fmt.Sprintf(` name='%s' `, strings.Replace(m.Value, `'`, `\'`, -1))
			case remote.MatchType_NOT_EQUAL:
				whereAdd = fmt.Sprintf(` name!='%s' `, strings.Replace(m.Value, `'`, `\'`, -1))
			case remote.MatchType_REGEX_MATCH:
				whereAdd = fmt.Sprintf(` match(name, %s) = 1 `, strings.Replace(m.Value, `/`, `\/`, -1))
			case remote.MatchType_REGEX_NO_MATCH:
				whereAdd = fmt.Sprintf(` match(name, %s) = 0 `, strings.Replace(m.Value, `/`, `\/`, -1))
			}
			mwhereSQL = append(mwhereSQL, whereAdd)
			continue
		}

		switch m.Type {
		case remote.MatchType_EQUAL:
			var insql bytes.Buffer
			asql := "arrayExists(x -> x IN (%s), tags) = 1"
			// value appears to be | sep'd for multiple matches
			for i, val := range strings.Split(m.Value, "|") {
				if len(val) < 1 {
					continue
				}
				if i == 0 {
					istr := fmt.Sprintf(`'%s=%s' `, m.Name, strings.Replace(val, `'`, `\'`, -1))
					insql.WriteString(istr)
				} else {
					istr := fmt.Sprintf(`,'%s=%s' `, m.Name, strings.Replace(val, `'`, `\'`, -1))
					insql.WriteString(istr)
				}
			}
			wstr := fmt.Sprintf(asql, insql.String())
			mwhereSQL = append(mwhereSQL, wstr)

		case remote.MatchType_NOT_EQUAL:
			var insql bytes.Buffer
			asql := "arrayExists(x -> x IN (%s), tags) = 0"
			// value appears to be | sep'd for multiple matches
			for i, val := range strings.Split(m.Value, "|") {
				if len(val) < 1 {
					continue
				}
				if i == 0 {
					istr := fmt.Sprintf(`'%s=%s' `, m.Name, strings.Replace(val, `'`, `\'`, -1))
					insql.WriteString(istr)
				} else {
					istr := fmt.Sprintf(`,'%s=%s' `, m.Name, strings.Replace(val, `'`, `\'`, -1))
					insql.WriteString(istr)
				}
			}
			wstr := fmt.Sprintf(asql, insql.String())
			mwhereSQL = append(mwhereSQL, wstr)

		case remote.MatchType_REGEX_MATCH:
			asql := `arrayExists(x -> 1 == match(x, '^%s=%s'),tags) = 1`
			// we can't have ^ in the regexp since keys are stored in arrays of key=value
			if strings.HasPrefix(m.Value, "^") {
				val := strings.Replace(m.Value, "^", "", 1)
				val = strings.Replace(val, `/`, `\/`, -1)
				mwhereSQL = append(mwhereSQL, fmt.Sprintf(asql, m.Name, val))
			} else {
				val := strings.Replace(m.Value, `/`, `\/`, -1)
				mwhereSQL = append(mwhereSQL, fmt.Sprintf(asql, m.Name, val))
			}

		case remote.MatchType_REGEX_NO_MATCH:
			asql := `arrayExists(x -> 1 == match(x, '^%s=%s'),tags) = 0`
			if strings.HasPrefix(m.Value, "^") {
				val := strings.Replace(m.Value, "^", "", 1)
				val = strings.Replace(val, `/`, `\/`, -1)
				mwhereSQL = append(mwhereSQL, fmt.Sprintf(asql, m.Name, val))
			} else {
				val := strings.Replace(m.Value, `/`, `\/`, -1)
				mwhereSQL = append(mwhereSQL, fmt.Sprintf(asql, m.Name, val))
			}
		}
	}

	// put select and where together with group by etc
	tempSQL := "%s, name, tags, quantile(%f)(val) as value FROM %s.%s %s AND %s GROUP BY t, name, tags ORDER BY t"
	sql := fmt.Sprintf(tempSQL, tselectSQL, r.conf.CHQuantile, r.conf.ChDB, r.conf.ChTable, twhereSQL,
		strings.Join(mwhereSQL, " AND "))
	return sql, nil
}

/*
	misc notes on adding remote reader functionality..

	issues:
		-prometheus passes match on specific field not an overall match eg. key=^something.* not ^key=something.*
		-simplest solution that may work is to remove any ^ chars from ^something.* and always put ^key=something.*
		-ie. remove hat symbol from prometheus remote read request with regexp match/not match and put in front of key

	need:
		+ -conf for quantile value for ts value aggregation
		+ -conf for max # samples to return per timeseries
		+ -conf for min time period for aggregation (eg. poll interval of prometheus)
		+ -algo to determine aggregation period for "group by t" from max # samples (above)
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
	//                           key regexp
	sql = "x -> 1 == match(x, '\^%s=%s'),"


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

func (r *p2cReader) Read(req *remote.ReadRequest) (*remote.ReadResponse, error) {
	var err error
	var sqlStr string
	var rows *sql.Rows

	resp := remote.ReadResponse{
		Results: []*remote.QueryResult{
			{Timeseries: make([]*remote.TimeSeries, 0, 0)},
		},
	}
	// need to map tags to timeseries to record samples
	var tsres = make(map[string]*remote.TimeSeries)

	// for debugging/figuring out query format/etc
	for _, q := range req.Queries {
		// remove me..
		//fmt.Println(q)

		// get the select sql
		sqlStr, err = r.getSQL(q)
		if err != nil {
			fmt.Printf("Error: reader: getSQL: %s\n", err.Error())
			return &resp, err
		}

		// get the select sql
		if err != nil {
			fmt.Printf("Error: reader: getSQL: %s\n", err.Error())
			return &resp, err
		}

		// todo: metrics on number of errors, rows, selects, timings, etc
		rows, err = r.db.Query(sqlStr)
		if err != nil {
			fmt.Printf("Error: query failed: %s", sqlStr)
			fmt.Printf("Error: query error: %s\n", err)
			return &resp, err
		}

		// build map of timeseries from sql result
		for rows.Next() {
			var (
				cnt   int
				t     int64
				name  string
				tags  []string
				value float64
			)
			if err = rows.Scan(&cnt, &t, &name, &tags, &value); err != nil {
				fmt.Printf("Error: scan: %s\n", err.Error())
			}
			// remove this..
			//fmt.Printf(fmt.Sprintf("%d,%d,%s,%s,%f\n", cnt, t, name, strings.Join(tags, ":"), value))

			// borrowed from influx remote storage adapter - array sep
			key := strings.Join(tags, "\xff")
			ts, ok := tsres[key]
			if !ok {
				ts = &remote.TimeSeries{
					Labels: makeLabels(tags),
				}
				tsres[key] = ts
			}
			ts.Samples = append(ts.Samples, &remote.Sample{
				Value:       float64(value),
				TimestampMs: t,
			})
		}
	}

	// now add results to response
	for _, ts := range tsres {
		resp.Results[0].Timeseries = append(resp.Results[0].Timeseries, ts)
	}

	//todo: return metrics :P
	return &resp, nil

}

func makeLabels(tags []string) []*remote.LabelPair {
	lpairs := make([]*remote.LabelPair, 0, len(tags))
	// (currently) writer includes __name__ in tags so no need to add it here
	// may change this to save space later..
	for _, tag := range tags {
		vals := strings.SplitN(tag, "=", 2)
		if len(vals) != 2 {
			fmt.Printf("Error unpacking tag key/val: %s\n", tag)
			continue
		}
		if vals[1] == "" {
			continue
		}
		lpairs = append(lpairs, &remote.LabelPair{
			Name:  vals[0],
			Value: vals[1],
		})
	}
	return lpairs
}
