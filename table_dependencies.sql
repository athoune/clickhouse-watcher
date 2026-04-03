SELECT *
FROM system.parts AS parts
INNER JOIN
(
    SELECT
    database,
        name,
        engine,
        loading_dependencies_table,
        has_own_data
    FROM system.tables
    ARRAY JOIN loading_dependencies_table
    WHERE (database = 'plausible') AND (engine != 'MergeTree')
) AS tables ON (tables.database = parts.database) AND (parts.`table` = tables.name)
