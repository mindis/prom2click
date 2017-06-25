package db

import (
	"database/sql"
	"fmt"

	"github.com/allegro/bigcache"
	"github.com/s4z/prom2click/common"
)

var (
	sqlFindHash = `
		SELECT hash FROM labels WHERE metric = ?
		ORDER BY date DESC LIMIT 1
	`
	sqlCreateHash = `
		INSERT INTO hashes (hash, tags)
			VALUES (?, ?)
	`
	sqlGetHash = `
		SELECT sipHash128(?)
	`
	sqlCreateMetric = `
		INSERT INTO labels (hash, metric, key, val)
			VALUES (?, ?, ?, ?)
	`
)

type MetricTx struct {
	db      *sql.DB
	cache   *bigcache.BigCache
	tx      *sql.Tx
	cMetric *sql.Stmt
	cHash   *sql.Stmt
}

func NewMetricTx(db *sql.DB, cache *bigcache.BigCache) *MetricTx {
	w := new(MetricTx)
	w.db = db
	w.cache = cache
	return w
}

func (t *MetricTx) FindHash(m *common.Metric) (string, error) {
	var (
		hash string
		err  error
	)
	row := t.db.QueryRow(sqlFindHash, m.Metric)
	err = row.Scan(&hash)
	// if the hash was found cache it
	if err == nil {
		err = t.cache.Set(m.Metric, []byte(hash))
	}
	return hash, err
}

func (t *MetricTx) GetHash(m *common.Metric) (string, error) {
	var (
		hash string
		err  error
	)
	row := t.db.QueryRow(sqlGetHash, m.Metric)
	if err = row.Scan(&hash); err != nil {
		return "", err
	}
	// cache the hash
	err = t.cache.Set(m.Metric, []byte(hash))
	return hash, err
}

func (t *MetricTx) CreateMetrics(metrics []*common.Metric) error {
	fmt.Printf("Creating %d metrics\n", len(metrics))
	var (
		tx      *sql.Tx
		cMetric *sql.Stmt
		err     error
	)
	tx, err = t.db.Begin()
	if err != nil {
		fmt.Printf("Error: NewMetricTx: Begin: %s\n", err)
		return err
	}

	cMetric, err = tx.Prepare(sqlCreateMetric)
	if err != nil {
		fmt.Printf("Error: NewMetricTx: CreateMetric: Prepare: %s\n", err)
		return err
	}

	for _, m := range metrics {
		for _, lbl := range m.TS.Labels {
			_, err := cMetric.Exec(m.Hash, m.Metric, lbl.Name, lbl.Value)
			if err != nil {
				fmt.Printf("Error: Create Metric Exec failed: %s\n", err.Error())
				continue
			}
		}
	}

	return tx.Commit()
}

func (t *MetricTx) CreateHashes(metrics []*common.Metric) error {
	fmt.Printf("Creating %d hashes\n", len(metrics))
	var (
		tx    *sql.Tx
		cHash *sql.Stmt
		err   error
	)
	tx, err = t.db.Begin()
	if err != nil {
		fmt.Printf("Error: NewMetricTx: Begin: %s\n", err)
		return err
	}

	cHash, err = tx.Prepare(sqlCreateHash)
	if err != nil {
		fmt.Printf("Error: NewMetricTx: CreateHash: Prepare: %s\n", err)
		return err
	}

	for _, m := range metrics {
		_, err := cHash.Exec(m.Hash, m.Metric)
		if err != nil {
			fmt.Printf("Error: Create Hash Exec failed: %s\n", err.Error())
			continue
		}
	}

	return tx.Commit()
}
