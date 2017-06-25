## Second Iteration of Prom2click

The first iteration of prom2click used a single table with an array of "key=value" entries for the metric labels and values, eg.


```sql
    CREATE DATABASE IF NOT EXISTS metrics;
    CREATE TABLE IF NOT EXISTS metrics.samples
    (
            date Date DEFAULT toDate(0),
            name String,
            tags Array(String),
            val Float64,
            ts DateTime,
            updated DateTime DEFAULT now()
    )
    ENGINE = GraphiteMergeTree(
            date, (name, tags, ts), 8192, 'graphite_rollup'
    );
```

The main problem with this schema is eg. needing to perform a regular expression 
match on all samples for read requests. Even with the graphite merge tree reducing
the sample counts this can become very expensive with even a modest number of 
metrics.

The next iteration could use a table for metric names,labels/values and
another table for metric values and timestamps. With this schema it is possible
to query only the metric names for matching metric ids and use these ids to 
query for samples. It is also possible to do this using a subquery. In addition
the system can check for queries that may match a large number of metrics and 
prevent them from overloading both the clickhouse server and the reader itself.

The main challenge is creating uniq ids for the mapping between labels/values and samples and also coordinating new metric creation/insertion.

The next challenge is handling data growth in the metrics table. Clickhouse does 
not support delete operations however does support dropping partitions.

Some possibilities:

* use another datastore for the id generation and mapping eg. mysql
* run a single master process to submit new metrics (static)
* use zookeeper to elect a master, submit all new metrics through the master
* use zookeeper to coordinate metric creation
* use a hash value of the tags as the id and asynchronously handle collisions

### Solution

* requirements
    * fully ha and scalable in terms of data storage, query processing
    * ability to efficiently search over very large set of metric metadata
    * ability to remove stale metric meta data
        * required due to regular expression searching over all metrics
        * autoscaling in some environments can create massive numbers of metrics
        * over time metrics metadata table queries will become too expensive
    * main classes of solutions considered
        * store metric metadata in mysql
            * plus
                * greatly simplifies metric creation, no id collisions
                * simplifies data management - eg. delete metrics with no samples
            * minus
                * traditional rdbms woes
                * significant additional requirement to deploy and maintain
        * create distributed system
            * ensure atomic generation of metric ids
            * ensure atomic metric insertion into db
            * plus
                * 'theoretically' uniq id generation
            * minus
                * requires coordination service, eg. zookeeper
                * significant complexity
                * distributed system woes - network splits, etc
                * many moving parts, much can go wrong
                * theory doesn't match reality - systems fail
        * create ids using hash of metric
            * plus
                * each writer can create metrics independently 
                * allows for duplicate entry creation
                * replacing merge tree can dedup over time 
                * by far the simplest approach
            * minus
                * collisions
                   * while very low still a non-zero probability
                   * asynchronous collision detection and resolution only possible
                   * introduce errors in a single metric
                        * errors introduced until all writers cache updated
                        * errors present until rotated out of sample history
                * full solution requires
                    * periodic collision detection and resolution
                        * upon new metric creation insert into two tables
                        * label_log includes hash, metric name and full tags 
                        * full_tags => strings.Join(tags, "0xff")
                        * select entries with same hash and different tags
                        * anything returned is a duplicate
                    * updates of writers cache
                        * add http endpoint on writers for cache updates
                        * record duplicates in a table writers can poll

### Solution
* hash based id maps
* table for samples, metrics, collisions
* writers cache id maps
* develop in several steps
    * basic functionality - reading/writing metrics to/from clickhouse
    * data management - expire old samples and metrics automatically
    * hash collision detection and resolution


### Lookup Cache
* cache only needed for efficient insertion, read requests serviced from db
* possibly do hashing in golang instead of in clickhouse
    * still need to check for hash collisions? how to know if metric exists?
    * possibly check each hash and maintain cache of known to exist metrics
    * cache only needs to be size of active metric set
* possibly use golang lib for this (eg. bigcache, freecache) for in process cache
* 10Million metrics at ~250 bytes each is only ~2.5GB +overhead
* fetch from db when a metric is received that does not exist in the cache
* if the metric isn't in cache or db then add it to the db and update cache
* only need active metric set - could be 10Million+ metrics in db, 1Million active
* add metrics on cache hit/miss - allows operators to tune cache size as needed


### Hash Collisions
* see [https://eprint.iacr.org/2014/722.pdf](https://eprint.iacr.org/2014/722.pdf)
* also [Birthday Attacks](https://en.wikipedia.org/wiki/Birthday_attack)
* my understanding is with a 128 bit hash collisions are very very unlikely
* minimize collision chances with sipHash128 that returns a FixedString(16)
* Clickhouse is optmized to work with FixedString(16) efficiently
* use a dedicated table (full_labels) to detect collisions
* use a dedicated table (collisions) to record collision events
* write new hash values into full_labels and record event in collisions
* writers poll collisions for updates
* possibly add /cache endpoint to update cache values
* new hash values written with dates 1 day before existing entry
* re-hash uses eg. tags + 0xff repeated <attempt#> times
* re-hashing from multiple writers simultaneously results in duplicates
* check for hash collision before inserting new hashes
* duplicate creates deduped by replacing merge tree
* hash value selection uses oldest entry
* use a single process for collision detection/handling
* also possible to rewrite collided hash and all samples, removing corrupted period


### Simultaneous Insertion
* coordination of new metric creation and insertion complex, allow duplicates
* use a Replicated Summing Merge Tree table to dedup
* ignore duplicates, use eg. "LIMIT 1 SORT ASC ts" on timestamp field
* all writers
    * ensure tags include __name__ on insertion
    * sort labels in same manner 
    * insert hash and full tags using sipHash128() on strings.Join(tags, ",")


### Cache Consistency 
* each writer starts a go routine to poll the collisions table 
* updates cache with new hash values as discovered
* writes to the previous hash value will appear to be for other metric 
* hash collisions are very unlikely
* detection and cache update times kept reasonably low (eg. 30mins)
* impact of hash collision reduced to detection and cache update interval


### Disk Space / Data Management
* samples table is quite simple - drop partitions older than n days
* metrics mapping table is more complex
    * run a single process - cannot have multiple instances running
    * process identifies all long lived metrics in a given partition in labels
    * moves records forward in time to next oldest partition
    * confirms all metrics in a partition to be dropped are either
        * migrated to the next oldest partition
        * or have no samples in the sample table
    * drops partitions with only stale metrics

### Record Growth Estimates
* eg. top end 5000 metrics / server x 100 autoscaled servers / day x 365 days
* results in 500,000 metrics/day and 182,500,000 metrics/year
* results in 912,500,000 metric records (assuming 5 tags per metric)
* this reduces to 182,500,000 metric records assuming 1000 metrics / server
* further reduces to 36,500,000 assuming 1000 metrics/server and 20 autoscales/day
* depending on many factors estimated 50Million to 1Billion+ metric rows / year
* cluster sizing needs to account for <1s searches over this data set for graphing
* eg. tags region=?, env=?, srv=?, app=?, feature=?


## Schema

```sql

        /* hash collision update event table */
        CREATE TABLE IF NOT EXISTS metrics.collisions 
        (
                date Date DEFAULT toDate(now()),
                tags String,
                ts DateTime DEFAULT now()
        )
        ENGINE = ReplicatedMergeTree(
                '/clickhouse/tables/{shard}/metrics.collisions',
                '{replica}', date, (tags, ts), 8192
        );

        /* hash collision check table*/
        CREATE TABLE IF NOT EXISTS metrics.full_labels
        (
                date Date DEFAULT toDate(0),
                hash FixedString(16),
                tags String,
                creates UInt32 DEFAULT 1,
                ts DateTime DEFAULT now()
        )
        ENGINE = ReplicatedSummingMergeTree(
                '/clickhouse/tables/{shard}/metrics.checks',
                '{replica}', date, (hash, tags), 8192, (creates)
        );

        /* labels table */
        CREATE TABLE IF NOT EXISTS metrics.labels
        (
                date Date DEFAULT toDate(now()),
                hash FixedString(16),
                metric String,
                key String,
                val String,
                creates UInt32 DEFAULT 1
        )
        ENGINE = ReplicatedSummingMergeTree(
                '/clickhouse/tables/{shard}/metrics.labels',
                '{replica}', date, (hash, metric, key, val), 8192, (creates)
        );

        /* samples table */
        CREATE TABLE IF NOT EXISTS metrics.samples
        (
                date Date DEFAULT toDate(now()),
                hash FixedString(16),
                val String,
                ts DateTime,
        )
        ENGINE = ReplicatedMergeTree(
                '/clickhouse/tables/{shard}/metrics.samples',
                '{replica}', date, (hash, ts), 8192
        );

        /* plus distributed tables for each */
```
