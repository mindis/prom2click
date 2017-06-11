# prom2click

Prom2click is a Prometheus remote storage adapter for Clickhouse. This project is in the early stages, beta testers are welcome :)

It's in a working state and writing metrics into Clickhouse in configurable batch sizes with a very small number of metrics (eg. Prometheus scraping itself).


```console
prom2click  --help
Usage of ./prom2click:
  -ch.batch int
        Clickhouse write batch size (n metrics). (default 8192)
  -ch.buffer int
        Maximum internal channel buffer size (n requests). (default 8192)
  -ch.db string
        The clickhouse database to write to. (default "metrics")
  -ch.table string
        The clickhouse table to write to. (default "samples")
  -dsn string
        The clickhouse server DSN to write to eg. tcp://host1:9000?username=user&password=qwerty&database=clicks&read_timeout=10&write_timeout=20&alt_hosts=host2:9000,host3:9000 (see https://github.com/kshvakov/clickhouse). (default "tcp://127.0.0.1:9000?username=&password=&database=metrics&read_timeout=10&write_timeout=10&alt_hosts=")
  -log.format value
        Set the log target and format. Example: "logger:syslog?appname=bob&local=7" or "logger:stdout?json=true" (default "logger:stderr")
  -log.level value
        Only log messages with the given severity or above. Valid levels: [debug, info, warn, error, fatal]
  -version
        Version
  -web.address string
        Address to listen on for web endpoints. (default ":9201")
  -web.metrics string
        Address to listen on for metric requests. (default "/metrics")
  -web.timeout duration
        The timeout to use for HTTP requests and server shutdown. Defaults to 30s. (default 30s)
  -web.write string
        Address to listen on for remote write requests. (default "/write")
```

## Getting started

* [Install clickhouse](https://clickhouse.yandex/)
    * If you run ubuntu they have debs, otherwise.. well.. containers? (I'm running it in lxc with net=none on some Redhat based systems)

    * Goto [Tabix](http://ui.tabix.io/) for a quick and easy Clickhouse UI

    * Create clickhouse schema
        ```sql
        CREATE DATABASE IF NOT EXISTS metrics;
        CREATE TABLE IF NOT EXISTS metrics.samples
            (
                date Date DEFAULT toDate(0),
                name String,
                tags Array(String),
                vals Array(String),
                val Float64,
                ts DateTime

            ) ENGINE = MergeTree(date, (tags, ts), 8192);
        ```
    * For a more resiliant setup you could setup shards, replicas and a distributed table
        * setup a Zookeeper cluster (or zetcd)
        * eg. for each clickhouse shard run two+ clickhouse servers and setup a ReplicatedMergeTree on each with the same zk path and uniq replicas (eg. replica => the servers fqdn)
        * next create a distributed table that looks at the ReplicatedMergeTrees
        * either define the {shard} and {replica} macros in your clickhouse server config or replace accordingly when you run the queries on each host
        * see: [Distributed](https://clickhouse.yandex/docs/en/table_engines/distributed.html) and [Replicated](https://clickhouse.yandex/docs/en/table_engines/replication.html)
    	```sql

    	CREATE TABLE IF NOT EXISTS metrics.samples
    	(
    		date Date DEFAULT toDate(0),
    		name String,
    		tags Array(String),
    		vals Array(String),
    		val Float64,
    		ts DateTime

    	) ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/metrics.samples', '  {replica}', date, (tags, ts), 8192);

    	CREATE TABLE IF NOT EXISTS metrics.dist
    	(
    		date Date DEFAULT toDate(0),
    		name String,
    		tags Array(String),
    		vals Array(String),
    		val Float64,
    		ts DateTime
    	) ENGINE = Distributed(metrics, metrics, samples, intHash64(name));
        ```

* Install/Configure [Grafana](https://grafana.com/)
* Install the [Clickhouse Grafana Datasource](https://github.com/Vertamedia/clickhouse-grafana) Plugin
     ```console
    sudo grafana-cli plugins  install vertamedia-clickhouse-datasource
     ```
* Install [Prometheus](https://prometheus.io/) and configure a remote write url
    ```yaml
    # Remote write configuration (for Graphite, OpenTSDB, or InfluxDB).
    remote_write:
        - url: "http://localhost:9201/write"

    ```
* Build prom2click and run it
    * Install go and glide

    ```console
    $ make get-deps
    $ make build
    $ ./bin/prom2click
    ```

* Create a dashboard
    * Example template query 
    ```sql
    SELECT DISTINCT(name) FROM metrics.samples
    ```
    * Example query
    ```sql
    SELECT $timeSeries as t, median(val) FROM $table WHERE $timeFilter
        AND name = $metric GROUP BY t ORDER BY t
    ```
    * Basic graph

    ![Alt text](./img/screen1.png "Dashboard Screen" )

### Testing

``make test``

note: there are no tests (yet)..


### Notes

Some things not yet sorted - this list will likely get longer for a bit:

* Get list of metrics, tags and tag values into Grafana more efficiently (instead of SELECT DISTINCT..)
* Load testing - how many metrics/second can a reasonably sized cluster accept (eg. 4 servers, 2 shards, 1 replica per shard)
* How to use templated variables within templates 
* How to select multiple series
* Will the graphite merge engine work with my schema
* Are 10 second samples for > 1 year feasible