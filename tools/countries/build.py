#!/usr/bin/env python3
"""Builds internal/countries/countries.json: a short fact sheet for every country.

Sources (downloaded when run, nothing is fetched by the app at runtime):
  - world-countries 5.1.0 (mledoze/countries, ODbL 1.0): names, capital, region,
    languages, currencies, calling code, borders, area, top-level domain
  - World Bank, SP.POP.TOTL, most recent value (CC BY 4.0): population
  - Wikidata P1622 (CC0): which side of the road traffic drives on
  - IANA tz database zone.tab (public domain): time zones per country

Run:  python3 -I tools/countries/build.py [path/to/zone.tab]
"""
import json
import sys
import urllib.parse
import urllib.request

UA = {"User-Agent": "pocket-travel-countries-build/1.0 (https://github.com/sajben94/pocket-homelab-travel)"}
OUT = "internal/countries/countries.json"


def get(url, headers=None):
    req = urllib.request.Request(url, headers={**UA, **(headers or {})})
    with urllib.request.urlopen(req, timeout=60) as r:
        return json.load(r)


def main():
    zone_tab = sys.argv[1] if len(sys.argv) > 1 else "/usr/share/zoneinfo/zone.tab"
    wc = get("https://cdn.jsdelivr.net/npm/world-countries@5.1.0/countries.json")
    pop = get("https://api.worldbank.org/v2/country/all/indicator/SP.POP.TOTL?mrv=1&format=json&per_page=400")[1]
    q = 'SELECT ?iso ?side WHERE { ?c wdt:P297 ?iso; wdt:P1622 ?s. ?s rdfs:label ?side FILTER(lang(?side)="en") }'
    drive = get("https://query.wikidata.org/sparql?" + urllib.parse.urlencode({"query": q}), {"Accept": "application/sparql-results+json"})

    population = {p["country"]["id"]: (p["value"], p["date"]) for p in pop if p.get("value") and len(p["country"]["id"]) == 2}
    sides = {}
    for b in drive["results"]["bindings"]:
        side = b["side"]["value"].replace(" ", "-")
        if side in ("left", "right"):  # a few have both over history; first wins
            sides.setdefault(b["iso"]["value"], side)
    zones = {}
    with open(zone_tab) as f:
        for line in f:
            if line.startswith("#") or not line.strip():
                continue
            parts = line.rstrip("\n").split("\t")
            zones.setdefault(parts[0], []).append(parts[2])
    cca3to2 = {c["cca3"]: c["cca2"] for c in wc}

    out = []
    for c in sorted(wc, key=lambda c: c["cca2"]):
        code = c["cca2"]
        idd = c.get("idd") or {}
        suffixes = idd.get("suffixes") or []
        calling = idd.get("root", "") + (suffixes[0] if len(suffixes) == 1 else "")
        p = population.get(code)
        out.append({
            "code": code,
            "name": c["name"]["common"],
            "official": c["name"]["official"],
            "capital": c.get("capital") or [],
            "region": c.get("region", ""),
            "subregion": c.get("subregion", ""),
            "population": p[0] if p else 0,
            "population_year": int(p[1]) if p else 0,
            "area_km2": c.get("area") or 0,
            "languages": list((c.get("languages") or {}).values()),
            "currencies": [{"code": k, "name": v.get("name", ""), "symbol": v.get("symbol", "")} for k, v in (c.get("currencies") or {}).items()],
            "calling_code": calling,
            "tld": (c.get("tld") or [""])[0],
            "drives": sides.get(code, ""),
            "timezones": zones.get(code, []),
            "borders": [cca3to2[b] for b in c.get("borders") or [] if b in cca3to2],
            "landlocked": bool(c.get("landlocked")),
            "latlng": c.get("latlng") or [],
            "demonym": ((c.get("demonyms") or {}).get("eng") or {}).get("m", ""),
            "independent": bool(c.get("independent")),
        })
    with open(OUT, "w") as f:
        json.dump({"sources": "world-countries 5.1.0 (ODbL), World Bank SP.POP.TOTL (CC BY 4.0), Wikidata P1622 (CC0), IANA tz zone.tab", "countries": out}, f, ensure_ascii=False, separators=(",", ":"))
    print(f"{len(out)} countries → {OUT}")


if __name__ == "__main__":
    main()
