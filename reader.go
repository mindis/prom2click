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
	var twhereSQL = "WHERE date >= toDate(%d) AND ts >= toDateTime(%d) AND ts <= toDateTime(%d) AND date <= toDate(%d)"
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
	whereSQL := fmt.Sprintf(twhereSQL, tstart, tstart, tend, tend)

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
	rcount := 0
	for _, q := range req.Queries {
		// remove me..
		fmt.Printf("\nquery: start: %d, end: %d\n\n", q.StartTimestampMs, q.EndTimestampMs)

		// get the select sql
		sqlStr, err = r.getSQL(q)
		fmt.Printf("query: running sql: %s\n\n", sqlStr)
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
			rcount++
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

	fmt.Printf("query: returning %d rows for %d queries\n", rcount, len(req.Queries))

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
