# note: replace {shard} and {replica} and run on each server
DROP TABLE IF EXISTS metrics.samples;
CREATE TABLE IF NOT EXISTS metrics.samples
 (
 	date Date DEFAULT toDate(0),
 	name String,
 	tags Array(String),
 	val Float64,
 	ts DateTime,
	updated DateTime DEFAULT now()
)
ENGINE = ReplicatedGraphiteMergeTree(
	'/clickhouse/tables/{shard}/metrics.samples',
	'{replica}', date, (name, tags, ts), 8192, 'graphite_rollup'
);

DROP TABLE IF EXISTS metrics.dist;
CREATE TABLE IF NOT EXISTS metrics.dist
 (
 	date Date DEFAULT toDate(0),
 	name String,
 	tags Array(String),
 	val Float64,
 	ts DateTime,
	updated DateTime DEFAULT now()
 ) ENGINE = Distributed(metrics, metrics, samples, sipHash64(name));
