SELECT
    size.database,
    size.`table`,
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
