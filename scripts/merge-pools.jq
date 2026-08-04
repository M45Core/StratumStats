def canonical_id($id): $metadata[0].aliases[$id] // $id;

def endpoint_key: "\(.host):\(.port):\(.tls)";
def unique_endpoints:
  group_by(endpoint_key) | map(.[0]);

($primary[0].pools | map({
  id: canonical_id(.name),
  name: .display_name,
  website: (.website // ""),
  category: ((.pool_type // "unknown") | ascii_downcase),
  sources: ["stratum-race"],
  endpoints: [{host: .host, port: .port, tls: false, sources: ["stratum-race"]}]
})) as $primary_pools |

($census[0].pools | map(
  . as $p |
  {
    id: canonical_id(.pool_id),
    name: .name,
    website: (.website // ""),
    category: (.type // "unknown"),
    sources: ["PoolCensus"],
    endpoints: [.endpoints[]? | . + {sources: ["PoolCensus"]}]
  }
)) as $census_pools |

($metadata[0].pools | map({
  id: .id,
  name: .name,
  website: (.website // ""),
  category: (.category // "unknown"),
  sources: ["pool-research"],
  endpoints: (.endpoints // [])
})) as $metadata_pools |

($metadata[0].pools | map({key: .id, value: .}) | from_entries) as $metadata_by_id |

{
  schema_version: 2,
  research_as_of: $metadata[0].as_of,
  generated_from: [
    {name: "stratum-race", path: "../stratum-race/config/pools.json"},
    {name: "PoolCensus", path: "../PoolCensus/collector/pools.json"},
    {name: "pool-research", path: "pool-metadata.json"}
  ],
  pools: (
    ($primary_pools + $census_pools + $metadata_pools)
    | map(select(.id as $id | (($metadata[0].exclude_ids // []) | index($id) | not)))
    | sort_by(.id)
    | group_by(.id)
    | map(
        . as $entries |
        ($entries | map(.endpoints[]) | unique_endpoints) as $endpoints |
        ($entries | map(.sources[]) | unique) as $sources |
        ($entries | map(select(.website != "")) | first) as $with_site |
        ({
          id: .[0].id,
          name: (if ($entries | map(.name) | unique | length) > 1 then $entries[0].name else .[0].name end),
          website: ($with_site.website // ""),
          category: .[0].category,
          sources: $sources,
          endpoints: $endpoints
        }) as $base |
        ($metadata_by_id[$base.id] // {}) as $details |
        ($base + $details) |
        .id = $base.id |
        .sources = (($sources + ["pool-research"]) | unique) |
        .endpoints = (($details.endpoints // $endpoints) | unique_endpoints)
      )
  )
}
