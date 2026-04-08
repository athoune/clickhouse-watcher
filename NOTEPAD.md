# My best SQL queries

## Server Settings

```sql
SELECT
    name,
    value,
    changed,
    type
FROM system.server_settings
ORDER BY name ASC
```

## Disks

```sql
SELECT *
FROM system.disks
```

```sql
SELECT
    database,
    `table`,
    disk_name,
    sum(bytes_on_disk)
FROM system.parts
GROUP BY
    database,
    `table`,
    disk_name
ORDER BY
    database ASC,
    `table` ASC
```

```sql
SELECT
    database,
    `table`,
    disk_name,
    round((100 * sum(bytes_on_disk)) / median(disks.total_space), 5) AS percent,
    sum(bytes_on_disk) AS bytes
FROM system.parts AS parts
INNER JOIN system.disks AS disks ON parts.disk_name = disks.name
GROUP BY
    database,
    `table`,
    disk_name
ORDER BY
    disk_name ASC,
    database ASC,
    `table` ASC,
    bytes DESC
```

## TTL

```sql
SELECT
    database,
    name,
    trimRight(extractGroups(create_table_query, 'TTL (.*?)( +COMMENT|SETTINGS.*)?;?$')[1]) AS ttl
FROM system.tables
WHERE (create_table_query LIKE '%TTL %') AND (NOT (create_table_query LIKE '%SYSTEM TTL%'))
```

```sql
SELECT
    database,
    name,
    tables.bytes
    tables.percent,
    trimRight(extractGroups(create_table_query, 'TTL (.*?)( +COMMENT|SETTINGS.*)?;?$')[1]) AS ttl
FROM system.tables AS tables LEFT OUTER JOIN (
    SELECT
        database,
        "table",
        disk_name,
        round((100 * sum(bytes_on_disk)) / median(disks.total_space), 5) AS percent,
        sum(bytes_on_disk) AS bytes
    FROM system.parts AS parts
    INNER JOIN system.disks AS disks ON parts.disk_name = disks.name
    GROUP BY
        database,
        "table",
        disk_name
    ORDER BY
        disk_name ASC,
        database ASC,
        `table` ASC,
        bytes DESC
) AS parts ON tables.database = parts.database AND tables.name = parts."table"
WHERE (create_table_query LIKE '%TTL %') AND (NOT (create_table_query LIKE '%SYSTEM TTL%'))



```

```sql
SELECT
    concat(size.database,'.', size.`table`) AS table,
    size.disk_name,
    size.percent,
    size.bytes,
    ttl.ttl,
    partition.partition_key
FROM
(
    SELECT
        database,
        `table`,
        disk_name AS disk,
        round((100 * sum(bytes_on_disk)) / median(disks.total_space), 5) AS percent,
        sum(bytes_on_disk) AS bytes
    FROM system.parts AS parts
    INNER JOIN system.disks AS disks ON parts.disk_name = disks.name
    GROUP BY
        database,
        `table`,
        disk_name
    ORDER BY
        disk_name ASC,
        database ASC,
        `table` ASC,
        bytes DESC
) AS size
LEFT JOIN
(
    SELECT
        database,
        name,
        trimRight(extractGroups(create_table_query, 'TTL (.*?)( +COMMENT|SETTINGS.*)?;?$')[1]) AS ttl
    FROM system.tables
    WHERE (create_table_query LIKE '%TTL %') AND (NOT (create_table_query LIKE '%SYSTEM TTL%'))
) AS ttl ON (size.database = ttl.database) AND (size.`table` = ttl.name)
LEFT JOIN
(
    SELECT
        database,
        name,
        partition_key
    FROM system.tables
) AS partition ON (size.database = partition.database) AND (size.`table` = partition.name)
```

```sql
SELECT *
FROM system.parts AS parts
INNER JOIN
(
    SELECT
        name,
        engine,
        loading_dependencies_table,
        has_own_data
    FROM system.tables
    ARRAY JOIN loading_dependencies_table
    WHERE (database = 'plausible') AND (engine != 'MergeTree')
) AS tables ON (tables.database = parts.database) AND (parts.`table` = tables.name)
```

## Partition

```sql
SELECT
    concat(database,'.', name) AS table,
    partition,
    ttl
FROM (
    SELECT
        database,
        name,
        regexpExtract(create_table_query, 'PARTITION BY (.*?) (PRIMARY KEY)?', 1) AS partition
    FROM system.tables
    WHERE create_table_query LIKE '%PARTITION BY%'
) AS partition
LEFT JOIN (
    SELECT
        database,
        name,
        trimRight(extractGroups(create_table_query, 'TTL (.*?)( +COMMENT|SETTINGS.*)?;?$')[1]) AS ttl
        FROM system.tables
    WHERE (create_table_query LIKE '%TTL %') AND (NOT (create_table_query LIKE '%SYSTEM TTL%'))
) AS ttl
ON partition.name = ttl.name AND partition.database = ttl.database
ORDER BY table
```

## Truncate

```sql
SELECT
    database,
    "table",
    size,
    rows,
    first,
    la
    duration,
    age,
    like(comment, '%It is safe to truncate or drop this table at any time.') AS truncatable
FROM system.tables AS t
INNER JOIN
(
    SELECT
        "table",
        database,
        formatReadableSize(sum(bytes)) AS size,
        sum(bytes) AS bytes_raw,
        sum(rows) AS rows,
        min(min_date) AS first,
        max(max_date) AS last,
        last - first AS duration,
        date(now()) - last AS age
    FROM system.parts
    WHERE active
    GROUP BY
        database,
        "table"
    ORDER BY bytes_raw DESC
    LIMIT 50
) AS p ON (t."table" = p."table") AND (t.database = p.database)
ORDER BY p.bytes_raw DESC
```

```sql
SELECT
    concat(database, '.', "table") AS db,
    formatReadableSize(sum(bytes)) AS size,
    sum(bytes) AS sort_by_size,
    min(min_date) AS first,
    max(max_date) AS last,
    last - first AS duration,
    date(now()) - last AS age
FROM system.parts
WHERE active
GROUP BY
    "table",
    database
ORDER BY sort_by_size DESC
```
