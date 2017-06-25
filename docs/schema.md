## Schema

### Replicated
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
        CREATE TABLE IF NOT EXISTS metrics.hashes
        (
                date Date DEFAULT toDate(0),
                hash FixedString(16),
                tags String,
                creates UInt32 DEFAULT 1,
                ts DateTime DEFAULT now()
        )
        ENGINE = ReplicatedSummingMergeTree(
                '/clickhouse/tables/{shard}/metrics.hashes',
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
                val Float64,
                ts DateTime,
        )
        ENGINE = ReplicatedMergeTree(
                '/clickhouse/tables/{shard}/metrics.samples',
                '{replica}', date, (hash, ts), 8192
        );

        /* plus distributed tables for each */
```


### Non-Replicated
```sql

        /* hash collision update event table */
        CREATE TABLE IF NOT EXISTS metrics.collisions 
        (
                date Date DEFAULT toDate(now()),
                tags String,
                ts DateTime DEFAULT now()
        )
        ENGINE = MergeTree(date, (tags, ts), 8192);

        /* hash collision check table*/
        CREATE TABLE IF NOT EXISTS metrics.hashes
        (
                date Date DEFAULT toDate(0),
                hash FixedString(16),
                tags String,
                creates UInt32 DEFAULT 1,
                ts DateTime DEFAULT now()
        )
        ENGINE = SummingMergeTree(date, (hash, tags), 8192, (creates));

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
        ENGINE = SummingMergeTree(
                date, (hash, metric, key, val), 8192, (creates)
        );

        /* samples table */
        CREATE TABLE IF NOT EXISTS metrics.samples
        (
                date Date DEFAULT toDate(now()),
                hash FixedString(16),
                val Float64,
                ts DateTime
        )
        ENGINE = MergeTree(date, (hash, ts), 8192);
```